package node

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/sys/unix"
)

func checkNodePublishVolumeRequest(req *csi.NodePublishVolumeRequest) error {
	if req.VolumeId == "" {
		return ErrVolumeIDNotProvided
	}

	if req.TargetPath == "" {
		return ErrTargetPathNotProvided
	}

	if req.VolumeCapability == nil {
		return ErrVolumeCapabilityNotProvided
	}

	if _, ok := req.GetVolumeContext()["mountURL"]; !ok {
		return ErrMountURLNotPresentInPublishContext
	}

	return nil
}

func checkNodeUnpublishVolumeRequest(req *csi.NodeUnpublishVolumeRequest) error {
	if req.VolumeId == "" {
		return ErrVolumeIDNotProvided
	}

	if req.TargetPath == "" {
		return ErrTargetPathNotProvided
	}

	return nil
}

func checkNodeGetVolumeStatsRequest(req *csi.NodeGetVolumeStatsRequest) error {
	if req.VolumeId == "" {
		return ErrVolumeIDNotProvided
	}

	if req.VolumePath == "" {
		return ErrVolumePathNotProvided
	}

	return nil
}

// statfsUsage returns the raw filesystem statistics for the given path.
func statfsUsage(path string) (unix.Statfs_t, error) {
	var buf unix.Statfs_t
	err := unix.Statfs(path, &buf)
	return buf, err
}

// volumeStats converts raw filesystem statistics into the byte and inode usage reported to
// kubelet.
//
// Used counts the occupied blocks (Blocks - Bfree) while Available only counts the free blocks
// usable by unprivileged users (Bavail). On a filesystem that reserves blocks for root the two
// therefore do not add up to Total, the difference being the unused part of that reserve. This is
// the same accounting kubelet applies to other volume types.
func volumeStats(statfs unix.Statfs_t) (bytes *csi.VolumeUsage, inodes *csi.VolumeUsage) {
	// Bsize is an int64 on linux/amd64 and linux/arm64, but an int32 on 32-bit platforms
	blockSize := int64(statfs.Bsize)

	// A filesystem reporting more free blocks or inodes than it has in total would otherwise
	// produce a negative used count, so the subtractions are clamped at zero
	bytes = &csi.VolumeUsage{
		Unit:      csi.VolumeUsage_BYTES,
		Total:     int64(statfs.Blocks) * blockSize,
		Available: int64(statfs.Bavail) * blockSize,
		Used:      max(int64(statfs.Blocks)-int64(statfs.Bfree), 0) * blockSize,
	}

	inodes = &csi.VolumeUsage{
		Unit:      csi.VolumeUsage_INODES,
		Total:     int64(statfs.Files),
		Available: int64(statfs.Ffree),
		Used:      max(int64(statfs.Files)-int64(statfs.Ffree), 0),
	}

	return bytes, inodes
}
