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
	"google.golang.org/protobuf/types/known/timestamppb"
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
	snapshotData                snapshotDataManager
	dataCopyLocks               operationLocks
}

// New creates a fresh instance of the Controller component, ready to register to a GRPC server.
func New() (csi.ControllerServer, error) {
	engine, err := api.NewAPI(api.WithClientOptions(client.TokenFromEnv(false)))
	if err != nil {
		return nil, fmt.Errorf("error creating API client with token from env: %w", err)
	}

	return &controller{
		engine:       engine,
		snapshotData: newDirectorySnapshotDataManager(engine),
	}, nil
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
		snapshotID     string
		snapshotVolume *dynamicvolumev1.Volume
	)
	if source := req.GetVolumeContentSource().GetSnapshot(); source != nil {
		snapshotID = source.GetSnapshotId()
		handle, decodeErr := decodeSnapshotHandle(snapshotID)
		if decodeErr != nil {
			return nil, status.Errorf(codes.NotFound, "snapshot not found: %s", decodeErr)
		}

		snapshotVolume = &dynamicvolumev1.Volume{Identifier: handle.BackingVolumeID}
		if getErr := cs.engine.Get(ctx, snapshotVolume); getErr != nil {
			return nil, engineErrorToGRPC(getErr)
		}
		if snapshotVolume.Size > size {
			return nil, status.Errorf(codes.OutOfRange, "requested volume size %d is smaller than snapshot size %d", size, snapshotVolume.Size)
		}

		lockKey := "restore:" + req.GetName()
		if !cs.dataCopyLocks.TryAcquire(lockKey) {
			return nil, status.Error(codes.Aborted, "another restore for this volume is already in progress")
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

	if snapshotVolume != nil {
		restoreErr := cs.snapshotDataManager().Restore(ctx, snapshotID, snapshotVolume, volume, newlyCreated)
		if restoreErr != nil {
			if newlyCreated {
				cs.cleanupFailedVolume(ctx, volume.Identifier, "snapshot restore")
			}
			if ctx.Err() != nil {
				return nil, status.FromContextError(ctx.Err()).Err()
			}
			if errors.Is(restoreErr, errIncompatibleContentSource) {
				return nil, status.Errorf(codes.AlreadyExists, "existing volume is incompatible with requested snapshot: %s", restoreErr)
			}

			return nil, status.Errorf(codes.Internal, "restore volume from snapshot: %s", restoreErr)
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

func (cs *controller) snapshotDataManager() snapshotDataManager {
	if cs.snapshotData == nil {
		cs.snapshotData = newDirectorySnapshotDataManager(cs.engine)
	}

	return cs.snapshotData
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
						Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
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

func (cs *controller) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "snapshot name must be provided")
	}
	if req.GetSourceVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "source volume id must be provided")
	}
	lockKey := "snapshot:" + req.GetName()
	if !cs.dataCopyLocks.TryAcquire(lockKey) {
		return nil, status.Error(codes.Aborted, "another operation for this snapshot is already in progress")
	}
	defer cs.dataCopyLocks.Release(lockKey)

	source := &dynamicvolumev1.Volume{Identifier: req.GetSourceVolumeId()}
	if err := cs.engine.Get(ctx, source); err != nil {
		return nil, engineErrorToGRPC(err)
	}
	if source.StorageServerInterfaces == nil || len(*source.StorageServerInterfaces) == 0 {
		return nil, status.Error(codes.Internal, "source volume has no storage server interface")
	}
	if source.Size <= 0 {
		return nil, status.Error(codes.Internal, "source volume has no usable capacity")
	}

	storageServerID := (*source.StorageServerInterfaces)[0].Identifier
	adsClass := source.ADSClass
	if value := req.GetParameters()["csi.anx.io/storage-server-identifier"]; value != "" {
		storageServerID = value
	}
	if value := req.GetParameters()["csi.anx.io/ads-class"]; value != "" {
		adsClass = value
	}

	snapshotRequest := &csi.CreateVolumeRequest{
		Name: snapshotBackingVolumeName(req.GetName()),
		Parameters: map[string]string{
			"csi.anx.io/storage-server-identifier": storageServerID,
			"csi.anx.io/ads-class":                 adsClass,
		},
	}
	snapshotVolume, newlyCreated, err := createAnexiaDynamicVolumeFromRequestWithStatus(ctx, cs.engine, snapshotRequest, source.Size)
	if err != nil {
		return nil, engineErrorToGRPC(err)
	}

	handle, err := encodeSnapshotHandle(snapshotHandle{
		BackingVolumeID: snapshotVolume.Identifier,
		SourceVolumeID:  source.Identifier,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%s", err)
	}

	createdAt, copyErr := cs.snapshotDataManager().Create(ctx, req.GetName(), source, snapshotVolume, newlyCreated)
	if copyErr != nil {
		if newlyCreated {
			cs.cleanupFailedVolume(ctx, snapshotVolume.Identifier, "snapshot copy")
		}
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		if errors.Is(copyErr, errIncompatibleContentSource) {
			return nil, status.Errorf(codes.AlreadyExists, "snapshot with the same name is incompatible: %s", copyErr)
		}

		return nil, status.Errorf(codes.Internal, "copy snapshot data: %s", copyErr)
	}

	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     handle,
			SourceVolumeId: source.Identifier,
			SizeBytes:      source.Size,
			CreationTime:   timestamppb.New(createdAt),
			ReadyToUse:     true,
		},
	}, nil
}

func (cs *controller) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	if req.GetSnapshotId() == "" {
		return nil, status.Error(codes.InvalidArgument, "snapshot id must be provided")
	}

	handle, valid := snapshotHandleForDelete(req.GetSnapshotId())
	if !valid {
		// DeleteSnapshot is idempotent. An unknown handle is treated as an
		// already-removed snapshot rather than as a volume identifier.
		return &csi.DeleteSnapshotResponse{}, nil
	}
	if err := cs.deleteDynamicVolume(ctx, handle.BackingVolumeID); err != nil {
		return nil, err
	}

	return &csi.DeleteSnapshotResponse{}, nil
}

func snapshotHandleForDelete(value string) (snapshotHandle, bool) {
	handle, err := decodeSnapshotHandle(value)
	return handle, err == nil
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
