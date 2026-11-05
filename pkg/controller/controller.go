// Package controller implements the CSI controller service backed by Anexia Dynamic Volumes.
package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	dynamicvolumev1 "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
)

// For a discussion regarding those limits, see also SO-14229.
const (
	oneMebibyteInBytes int64 = 1 << (2 * 10)             // = 1 MiB
	oneGibibyteInBytes int64 = oneMebibyteInBytes * 1024 // = 1 GiB

	defaultVolumeSize                int64         = 10 * oneGibibyteInBytes        // Default size for volumes without a capacity range specified = 10 GiB
	maxVolumeSize                    int64         = 10 * 1024 * oneGibibyteInBytes // Maximum volume size (= 10TiB)
	defaultVolumeDeleteRetryInterval time.Duration = 5 * time.Second
	defaultVolumeDeleteMaxAttempts                 = 13
	failedOperationCleanupTimeout    time.Duration = 2 * time.Minute
)

type controller struct {
	csi.UnimplementedControllerServer

	engine                      api.API
	volumeExpansionPollInterval time.Duration
	volumeDeleteRetryInterval   time.Duration
	volumeDeleteMaxAttempts     int
	volumeData                  volumeDataManager
	dataCopyLocks               operationLocks
	kube                        kubernetes.Interface
}

// New creates a fresh instance of the Controller component, ready to register to a GRPC server.
// With sourceInUseCheck enabled. cloning of volumes attached to a node is rejected; this needs
// in-cluster access to Kubernetes API.
func New(sourceInUseCheck bool) (csi.ControllerServer, error) {
	engine, err := api.NewAPI(api.WithClientOptions(client.TokenFromEnv(false)))
	if err != nil {
		return nil, fmt.Errorf("error creating API client with token from env: %w", err)
	}

	cs := &controller{
		engine:     engine,
		volumeData: newDirectoryDataManager(engine),
	}

	if sourceInUseCheck {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("error loading in-cluster config for source-in-use check: %w", err)
		}
		if cs.kube, err = kubernetes.NewForConfig(cfg); err != nil {
			return nil, fmt.Errorf("error creating kubernetes client: %w", err)
		}
	}
	return cs, nil
}

func (cs *controller) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(2).Info("Creating new volume")
	if err := checkCreateVolumeRequest(req); err != nil {
		klog.V(2).ErrorS(err, "Volume request validation failed", "request", req)
		return nil, status.Errorf(codes.InvalidArgument, "request check failed: %s", err)
	}

	size, err := sizeFromCapacityRange(req.GetCapacityRange())
	if err != nil {
		klog.V(2).ErrorS(err, "Volume capacity range cannot be satisfied", "request", req)
		return nil, status.Errorf(codes.OutOfRange, "%s", err)
	}

	var (
		sourceVolume *dynamicvolumev1.Volume
	)

	if source := req.GetVolumeContentSource().GetVolume(); source != nil {
		sourceVolume = &dynamicvolumev1.Volume{Identifier: source.GetVolumeId()}
		if getErr := cs.engine.Get(ctx, sourceVolume); getErr != nil {
			return nil, engineErrorToGRPC(getErr)
		}
		if sourceVolume.Size > size {
			return nil, status.Errorf(codes.OutOfRange, "requested volume size %d is smaller than source volume size %d", size, sourceVolume.Size)
		}
		inUse, inUseErr := cs.sourceInUse(ctx, source.GetVolumeId())
		if inUseErr != nil {
			return nil, status.Errorf(codes.Internal, "check if source volume %s is in use", inUseErr)
		}
		if inUse {
			return nil, status.Error(codes.FailedPrecondition, "source volume is already in use, stop the workload using it before cloning")
		}
		lockKey := "clone:" + req.GetName()
		if !cs.dataCopyLocks.TryAcquire(lockKey) {
			return nil, status.Error(codes.Aborted, "another clone of this volume is already in progress")
		}
		defer cs.dataCopyLocks.Release(lockKey)
	}

	klog.V(2).Info("Querying storage server interface from Anexia Engine")
	storageServer, err := getDynamicStorageServer(ctx, cs.engine, req)
	if err != nil {
		klog.V(2).ErrorS(err, "Failed to query storage server interface")
		return nil, engineErrorToGRPC(err)
	}

	volume, newlyCreated, err := createAnexiaDynamicVolumeFromRequestWithStatus(ctx, cs.engine, req, size)
	if err != nil {
		klog.V(2).ErrorS(err, "Volume creation in Anexia Engine failed")
		return nil, engineErrorToGRPC(err)
	}

	mount, err := createMountURL(volume, storageServer)
	if err != nil {
		// ADV v2 switched to an asynchronous model under the hood. Therefore it's very
		// likely that although the engine says that the volume is ready, it's not ready
		// "ready", if I understood that correctly.
		//
		// Since ADV v2 is still in development, this might change. However, instead of
		// not doing any error checking whatsoever for the values (which already caused
		// support tickets in the past), we're now failing gracefully.
		//
		// The codes.Unavailable code is meant for transient errors. Therefore this
		// method is called again repeatedly until we can finally build that URL.
		klog.V(2).ErrorS(err, "Volume likely not ready yet, construction of mount URL not possible")
		return nil, status.Errorf(codes.Unavailable, "Volume not ready yet, construction of mount URL was not possible")
	}

	if sourceVolume != nil {
		cloneErr := cs.volumeDataManager().Clone(ctx, sourceVolume, volume, newlyCreated)
		if cloneErr != nil {
			if newlyCreated {
				cs.cleanupFailedVolume(ctx, volume.Identifier, "volume clone")
			}
			if ctx.Err() != nil {
				return nil, status.FromContextError(ctx.Err()).Err()
			}
			if errors.Is(cloneErr, errIncompatibleContentSource) {
				return nil, status.Errorf(codes.AlreadyExists, "existing volume is incompatible with requested source volume: %s", cloneErr)
			}
			return nil, status.Errorf(codes.Internal, "clone volume: %s", cloneErr)
		}
	}

	klog.V(4).InfoS("Volume successfully created", "id", volume.Identifier)
	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      volume.Identifier,
			CapacityBytes: volume.Size,
			VolumeContext: map[string]string{
				"mountURL": mount,
			},
			ContentSource: req.GetVolumeContentSource(),
		},
	}

	return resp, nil
}

func (cs *controller) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.V(2).InfoS("Deleting volume", "id", req.GetVolumeId())
	if err := checkDeleteVolumeRequest(req); err != nil {
		klog.V(4).ErrorS(err, "Volume request invalid", "request", req)
		return nil, status.Errorf(codes.InvalidArgument, "request check failed: %s", err)
	}

	klog.V(4).InfoS("Deleting ADV volume in Anexia Engine")
	if err := cs.deleteDynamicVolume(ctx, req.GetVolumeId()); err != nil {
		return nil, err
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controller) deleteDynamicVolume(ctx context.Context, identifier string) error {
	volume := &dynamicvolumev1.Volume{Identifier: identifier}
	maxAttempts := cs.deleteMaxAttempts()
	for attempt := 1; ; attempt++ {
		err := cs.engine.Destroy(ctx, volume)
		if api.IgnoreNotFound(err) == nil {
			klog.V(2).Info("Volume successfully deleted")
			return nil
		}

		var httpError api.HTTPError
		if !errors.As(err, &httpError) || httpError.StatusCode() != http.StatusUnprocessableEntity || attempt >= maxAttempts {
			klog.V(2).ErrorS(err, "Volume deletion failed", "attempt", attempt)
			return engineErrorToGRPC(err)
		}

		klog.V(2).InfoS("Volume deletion temporarily blocked, retrying", "id", identifier, "attempt", attempt)
		timer := time.NewTimer(cs.deleteRetryInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return status.FromContextError(ctx.Err()).Err()
		case <-timer.C:
		}
	}
}

func (cs *controller) cleanupFailedVolume(ctx context.Context, identifier, operation string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failedOperationCleanupTimeout)
	defer cancel()

	if err := cs.deleteDynamicVolume(cleanupCtx, identifier); err != nil {
		klog.ErrorS(err, "Failed to remove volume after operation failed", "id", identifier, "operation", operation)
	}
}

func (cs *controller) volumeDataManager() volumeDataManager {
	if cs.volumeData == nil {
		cs.volumeData = newDirectoryDataManager(cs.engine)
	}

	return cs.volumeData
}

func (cs *controller) deleteRetryInterval() time.Duration {
	if cs.volumeDeleteRetryInterval > 0 {
		return cs.volumeDeleteRetryInterval
	}

	return defaultVolumeDeleteRetryInterval
}

func (cs *controller) deleteMaxAttempts() int {
	if cs.volumeDeleteMaxAttempts > 0 {
		return cs.volumeDeleteMaxAttempts
	}

	return defaultVolumeDeleteMaxAttempts
}

func (*controller) ControllerPublishVolume(_ context.Context, _ *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	// intentional noop to allow non-breaking activation in the future
	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (*controller) ControllerUnpublishVolume(_ context.Context, _ *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	// intentional noop to allow non-breaking activation in the future
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (*controller) ControllerGetCapabilities(_ context.Context, _ *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
					},
				},
			},

			// Support for volume expansion API: https://kubernetes-csi.github.io/docs/volume-expansion.html
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
					},
				},
			},
		},
	}, nil
}

type operationLocks struct {
	active sync.Map
}

func (locks *operationLocks) TryAcquire(key string) bool {
	_, loaded := locks.active.LoadOrStore(key, struct{}{})
	return !loaded
}

func (locks *operationLocks) Release(key string) {
	locks.active.Delete(key)
}

func (cs *controller) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	if err := checkValidateVolumeCapabilitiesRequest(req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "request check failed: %s", err)
	}

	if err := cs.engine.Get(ctx, &dynamicvolumev1.Volume{Identifier: req.GetVolumeId()}); err != nil {
		return nil, engineErrorToGRPC(err)
	}

	if err := checkVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "requested volume capabilities not supported: %s", err)
	}

	resp := &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.GetVolumeCapabilities(),
		},
	}

	return resp, nil
}
