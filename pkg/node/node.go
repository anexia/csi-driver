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
	l := ns.logger.V(2).WithValues("id", req.VolumeId, "path", req.GetTargetPath())
	l.Info("Trying to mount volume")

	if err := checkNodePublishVolumeRequest(req); err != nil {
		l.Error(err, "NodePublishVolumeRequest invalid")
		return nil, status.Errorf(codes.InvalidArgument, "invalid NodePublishVolumeRequest: %s", err)
	}

	opts := req.GetVolumeCapability().GetMount().GetMountFlags()
	if req.GetReadonly() {
		l.Info("Volume will be mounted as read-only")
		opts = append(opts, "ro")
	}

	l.Info("Validating target path")
	// adapted from https://github.com/kubernetes-csi/csi-driver-nfs/blob/f084312ad0a3c05b720466db7f8721db2aec6a66/pkg/nfs/nodeserver.go#L108
	notMount, err := ns.mounter.IsLikelyNotMountPoint(req.GetTargetPath())
	if err != nil {
		if os.IsNotExist(err) {
			l.Info("Creating new directory at target path", "target_path", req.GetTargetPath())
			if err := os.Mkdir(req.GetTargetPath(), os.FileMode(os.ModeDir)); err != nil {
				l.Error(err, "Directory at target path failed")
				return nil, status.Errorf(codes.Internal, "error creating target directory: %q", err)
			}

			notMount = true
		} else {
			return nil, status.Errorf(codes.Internal, "error checking if target path is mount: %q", err)
		}
	}

	if !notMount {
		klog.V(2).Infof("NodePublishVolume: Mount already present at target path %q.", req.TargetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	l.Info("Mounting volume to target path")
	mountURL := req.GetVolumeContext()["mountURL"]
	if err := ns.mounter.Mount(mountURL, req.GetTargetPath(), "nfs", opts); err != nil {
		l.Error(err, "Mounting volume failed")
		return nil, status.Errorf(codes.Internal, "error mounting volume: %s", err)
	}

	l.Info("Volume mounted successfully")
	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns node) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	l := ns.logger.V(2).WithValues("id", req.VolumeId, "path", req.GetTargetPath())
	l.Info("Trying to unmount volume")

	if err := checkNodeUnpublishVolumeRequest(req); err != nil {
		l.Error(err, "NodeUnpublishVolumeRequest invalid")
		return nil, status.Errorf(codes.InvalidArgument, "invalid NodeUnpublishVolumeRequest: %s", err)
	}

	l.Info("Cleaning up mount path")
	if err := mount.CleanupMountPoint(req.GetTargetPath(), ns.mounter, true); err != nil {
		l.Error(err, "Cleaning up mount path failed")
		return nil, status.Errorf(codes.Internal, "error cleaning up mount point: %s", err)
	}

	l.Info("Volume successfully unmounted")
	return &csi.NodeUnpublishVolumeResponse{}, nil
}
