package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"

	dynamicvolumev1 "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
)

// For a discussion regarding those limits, see also SO-14229.
const (
	oneMebibyteInBytes int64 = 1 << (2 * 10)             // = 1 MiB
	oneGibibyteInBytes int64 = oneMebibyteInBytes * 1024 // = 1 GiB

	defaultVolumeSize int64 = 10 * oneGibibyteInBytes   // Default size for volumes without a capacity range specified = 10 GiB
	maxVolumeSize     int64 = 1024 * oneGibibyteInBytes // Maximum volume size (= 1TiB)
)

type controller struct {
	csi.UnimplementedControllerServer

	engine api.API
}

// New creates a fresh instance of the Controller component, ready to register to a GRPC server.
func New() (csi.ControllerServer, error) {
	engine, err := api.NewAPI(api.WithClientOptions(client.TokenFromEnv(false)))
	if err != nil {
		return nil, fmt.Errorf("error creating API client with token from env: %w", err)
	}

	return &controller{engine: engine}, nil
}

func (cs *controller) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(2).Info("Creating new volume")
	if err := checkCreateVolumeRequest(req); err != nil {
		klog.V(2).ErrorS(err, "Volume request validation failed", "request", req)
		return nil, status.Errorf(codes.InvalidArgument, "request check failed: %s", err)
	}

	klog.V(2).Info("Querying storage server interface from Anexia Engine")
	storageServer, err := getDynamicStorageServer(ctx, cs.engine, req)
	if err != nil {
		klog.V(2).ErrorS(err, "Failed to query storage server interface")
		return nil, engineErrorToGRPC(err)
	}

	volume, err := createAnexiaDynamicVolumeFromRequest(ctx, cs.engine, req)
	if err != nil {
		// The deadline exceeded error is returned by the AwaitCompletion helper.
		// Instead of writing a very long error message that just ends with "context
		// deadline exceeded", we return a proper error message that helps to understand
		// what's going on.
		//
		// Especially in the output of "kubectl describe pvc <pvc-name>", this improves the UX a lot.
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, status.Error(codes.Unavailable, "Volume is not ready yet")
		}

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

	klog.V(4).Info("Volume successfully created", "id", volume.Identifier)
	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      volume.Identifier,
			CapacityBytes: volume.Size,
			VolumeContext: map[string]string{
				"mountURL": mount,
			},
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
	if err := cs.engine.Destroy(ctx, &dynamicvolumev1.Volume{Identifier: req.VolumeId}); api.IgnoreNotFound(err) != nil {
		klog.V(2).ErrorS(err, "Volume deletion failed")
		return nil, engineErrorToGRPC(err)
	}

	klog.V(2).Info("Volume successfully deleted")
	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controller) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	// intentional noop to allow non-breaking activation in the future
	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (cs *controller) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	// intentional noop to allow non-breaking activation in the future
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *controller) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
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
