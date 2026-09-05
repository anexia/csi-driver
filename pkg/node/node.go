package node

import (
	"context"
	"os"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
)

type node struct {
	csi.UnimplementedNodeServer

	nodeID  string
	mounter mount.Interface

	// statfs is overridden in tests, it defaults to statfsUsage
	statfs func(path string) (unix.Statfs_t, error)
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
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
					},
				},
			},
		},
	}, nil
}

// statfsFunc returns the configured statfs implementation, defaulting to the real syscall.
func (ns node) statfsFunc() func(path string) (unix.Statfs_t, error) {
	if ns.statfs != nil {
		return ns.statfs
	}

	return statfsUsage
}

func (ns node) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	klog.V(4).InfoS("Collecting volume stats", "id", req.GetVolumeId(), "path", req.GetVolumePath())

	if err := checkNodeGetVolumeStatsRequest(req); err != nil {
		klog.V(4).ErrorS(err, "NodeGetVolumeStatsRequest invalid")
		return nil, status.Errorf(codes.InvalidArgument, "invalid NodeGetVolumeStatsRequest: %s", err)
	}

	// An existing directory without a mount is not the volume, it is the node filesystem
	// showing through the empty mount point, so its usage must not be reported as the volume's.
	notMount, err := ns.mounter.IsLikelyNotMountPoint(req.GetVolumePath())
	if err != nil {
		if os.IsNotExist(err) {
			klog.V(4).ErrorS(err, "Volume path does not exist", "path", req.GetVolumePath())
			return nil, status.Errorf(codes.NotFound, "volume path %q does not exist", req.GetVolumePath())
		}

		klog.V(2).ErrorS(err, "Not possible to validate whether the volume path is a mount", "path", req.GetVolumePath())
		return nil, status.Errorf(codes.Internal, "error checking if volume path is mount: %s", err)
	}

	if notMount {
		klog.V(4).InfoS("No volume mounted at volume path", "path", req.GetVolumePath())
		return nil, status.Errorf(codes.NotFound, "no volume mounted at volume path %q", req.GetVolumePath())
	}

	statfs, err := ns.statfsFunc()(req.GetVolumePath())
	if err != nil {
		if os.IsNotExist(err) {
			klog.V(4).ErrorS(err, "Volume path vanished while collecting stats", "path", req.GetVolumePath())
			return nil, status.Errorf(codes.NotFound, "volume path %q does not exist", req.GetVolumePath())
		}

		klog.V(2).ErrorS(err, "Collecting volume stats failed", "path", req.GetVolumePath())
		return nil, status.Errorf(codes.Internal, "error collecting volume stats: %s", err)
	}

	bytes, inodes := volumeStats(statfs)

	klog.V(4).InfoS("Volume stats collected successfully", "id", req.GetVolumeId())
	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{bytes, inodes},
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
