package node

import (
	"context"
	"os"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"k8s.io/mount-utils"
)

type node struct {
	csi.UnimplementedNodeServer

	logger  logr.Logger
	nodeID  string
	mounter mount.Interface
}

// New creates a fresh instance of the Node component, ready to register to a GRPC server.
func New(logger logr.Logger, nodeID string) (csi.NodeServer, error) {
	if nodeID == "" {
		logger.Info("nodeid is empty")
	}

	return &node{
		logger:  logger,
		nodeID:  nodeID,
		mounter: mount.New(""),
	}, nil
}

func (ns node) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{},
	}, nil
}

func (ns node) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId: ns.nodeID,
	}, nil
}

func (ns node) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if err := checkNodePublishVolumeRequest(req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid NodePublishVolumeRequest: %s", err)
	}

	opts := req.GetVolumeCapability().GetMount().GetMountFlags()

	if req.GetReadonly() {
		opts = append(opts, "ro")
	}

	notMount, err := ns.mounter.IsLikelyNotMountPoint(req.GetTargetPath())
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(req.GetTargetPath(), os.FileMode(0)); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}

			notMount = true
		} else {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	if !notMount {
		return &csi.NodePublishVolumeResponse{}, nil
	}

	mountURL := req.GetVolumeContext()["mountURL"]

	if err := ns.mounter.Mount(mountURL, req.GetTargetPath(), "nfs", opts); err != nil {
		return nil, status.Errorf(codes.Internal, "error mounting volume: %s", err)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns node) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if err := checkNodeUnpublishVolumeRequest(req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid NodeUnpublishVolumeRequest: %s", err)
	}

	if err := mount.CleanupMountPoint(req.GetTargetPath(), ns.mounter, true); err != nil {
		return nil, status.Errorf(codes.Internal, "error cleaning up mount point: %s", err)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}
