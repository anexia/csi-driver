package node

import (
	"context"
	"os"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"k8s.io/klog/v2"
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

	// adapted from https://github.com/kubernetes-csi/csi-driver-nfs/blob/f084312ad0a3c05b720466db7f8721db2aec6a66/pkg/nfs/nodeserver.go#L108
	notMount, err := ns.mounter.IsLikelyNotMountPoint(req.GetTargetPath())
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.Mkdir(req.GetTargetPath(), os.FileMode(os.ModeDir)); err != nil {
				return nil, status.Errorf(codes.Internal, "error creating target directory: %q", err)
			}

			notMount = true
		} else {
			return nil, status.Errorf(codes.Internal, "error checking if target path is mount: %q", err)
		}
	}

	if !notMount {
		klog.V(4).Infof("NodePublishVolume: Mount already present at target path %q.", req.TargetPath)
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
