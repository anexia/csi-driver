package controller

import (
	"context"
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	dynamicvolumev1 "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
)

// default size for volumes without a capacity range specified = 10 GiB
const defaultVolumeSize int64 = 10737418240

type controller struct {
	csi.UnimplementedControllerServer

	logger logr.Logger
	engine api.API
}

// New creates a fresh instance of the Controller component, ready to register to a GRPC server.
func New(logger logr.Logger) (csi.ControllerServer, error) {
	engine, err := api.NewAPI(api.WithClientOptions(client.TokenFromEnv(false)))
	if err != nil {
		return nil, fmt.Errorf("error creating API client with token from env: %w", err)
	}

	return &controller{
		logger: logger,
		engine: engine,
	}, nil
}

func (cs *controller) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	l := cs.logger.V(2).WithValues("name", req.GetName())
	l.Info("Creating new volume")
	if err := checkCreateVolumeRequest(req); err != nil {
		l.Error(err, "Volume request validation failed")
		return nil, status.Errorf(codes.InvalidArgument, "request check failed: %s", err)
	}

	l.Info("Querying storage server interface from Anexia Engine")
	storageServer, err := getDynamicStorageServer(ctx, cs.engine, req)
	if err != nil {
		l.Error(err, "Failed to query storage server interface")
		return nil, engineErrorToGRPC(err)
	}

	l.Info("Creating new volume")
	volume, err := createAnexiaDynamicVolumeFromRequest(ctx, cs.engine, req)
	if err != nil {
		l.Error(err, "Volume creation failed")
		return nil, engineErrorToGRPC(err)
	}

	l.Info("Volume successfully created", "id", volume.Identifier)
	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      volume.Identifier,
			CapacityBytes: volume.Size,
			VolumeContext: map[string]string{
				"mountURL": createMountURL(volume, storageServer),
			},
		},
	}

	return resp, nil
}

func (cs *controller) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	l := cs.logger.V(2).WithValues("id", req.GetVolumeId())
	l.Info("Deleting volume")

	if err := checkDeleteVolumeRequest(req); err != nil {
		l.Error(err, "Volume request invalid")
		return nil, status.Errorf(codes.InvalidArgument, "request check failed: %s", err)
	}

	l.Info("Deleting volume in Anexia Engine")
	if err := cs.engine.Destroy(ctx, &dynamicvolumev1.Volume{Identifier: req.VolumeId}); api.IgnoreNotFound(err) != nil {
		l.Error(err, "Volume deletion failed")
		return nil, engineErrorToGRPC(err)
	}

	l.Info("Volume successfully deleted")
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
