package node

import (
	"context"
	"os"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
)

type node struct {
	csi.UnimplementedNodeServer

	nodeID  string
	mounter mount.Interface
}

// New creates a fresh instance of the Node component, ready to register to a GRPC server.
func New(nodeID string) (csi.NodeServer, error) {
	if nodeID == "" {
		klog.V(0).InfoS("The nodeID of this server is empty. This can lead to unexpected behaviour.")
	}

	return &node{
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
	klog.V(2).InfoS("Trying to mount volume", "id", req.VolumeId, "path", req.GetTargetPath())

	if err := checkNodePublishVolumeRequest(req); err != nil {
		klog.ErrorS(err, "NodePublishVolumeRequest invalid")
		return nil, status.Errorf(codes.InvalidArgument, "invalid NodePublishVolumeRequest: %s", err)
	}

	opts := req.GetVolumeCapability().GetMount().GetMountFlags()
	if req.GetReadonly() {
		klog.V(2).InfoS("Volume will be mounted as read-only", "id", req.VolumeId)
		opts = append(opts, "ro")
	}

	klog.V(3).InfoS("Validating target path")
	// adapted from https://github.com/kubernetes-csi/csi-driver-nfs/blob/f084312ad0a3c05b720466db7f8721db2aec6a66/pkg/nfs/nodeserver.go#L108
	notMount, err := ns.mounter.IsLikelyNotMountPoint(req.GetTargetPath())
	if err != nil {
		if os.IsNotExist(err) {
			klog.V(3).InfoS("Creating new directory at target path", "target_path", req.GetTargetPath())
			if err := os.Mkdir(req.GetTargetPath(), os.FileMode(os.ModeDir)); err != nil {
				klog.V(2).ErrorS(err, "Creating a directory at path failed, cannot mount PVC", "target_path", req.GetTargetPath())
				return nil, status.Errorf(codes.Internal, "error creating target directory: %q", err)
			}

			notMount = true
		} else {
			klog.V(2).ErrorS(err, "Not possible to validate whether the target path is a mount", "target_path", req.GetTargetPath())
			return nil, status.Errorf(codes.Internal, "error checking if target path is mount: %q", err)
		}
	}

	if !notMount {
		klog.V(2).Infof("NodePublishVolume: Mount already present at target path %q.", req.TargetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	klog.V(2).InfoS("Mounting volume to target path", "id", req.VolumeId)
	mountURL := req.GetVolumeContext()["mountURL"]
	if err := ns.mounter.Mount(mountURL, req.GetTargetPath(), "nfs", opts); err != nil {
		klog.V(2).ErrorS(err, "Mounting volume failed", "target_path", req.GetTargetPath())
		return nil, status.Errorf(codes.Internal, "error mounting volume: %s", err)
	}

	klog.V(4).InfoS("Volume mounted successfully", "id", req.VolumeId)
	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns node) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	klog.V(4).InfoS(
		"Trying to unmount volume",
		"id", req.VolumeId,
		"path", req.GetTargetPath(),
	)

	if err := checkNodeUnpublishVolumeRequest(req); err != nil {
		klog.V(4).ErrorS(err, "NodeUnpublishVolumeRequest invalid", "request", req)
		return nil, status.Errorf(codes.InvalidArgument, "invalid NodeUnpublishVolumeRequest: %s", err)
	}

	klog.V(4).Info("Cleaning up mount path")
	if err := mount.CleanupMountPoint(req.GetTargetPath(), ns.mounter, true); err != nil {
		klog.V(4).ErrorS(err, "Cleaning up mount path failed")
		return nil, status.Errorf(codes.Internal, "error cleaning up mount point: %s", err)
	}

	klog.V(4).Info("Volume successfully unmounted")
	return &csi.NodeUnpublishVolumeResponse{}, nil
}
