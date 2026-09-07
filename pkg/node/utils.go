package node

import (
	"fmt"
	"math"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/sys/unix"
)

func checkNodePublishVolumeRequest(req *csi.NodePublishVolumeRequest) error {
	if req.GetVolumeId() == "" {
		return ErrVolumeIDNotProvided
	}

	if req.GetTargetPath() == "" {
		return ErrTargetPathNotProvided
	}

	if req.GetVolumeCapability() == nil {
		return ErrVolumeCapabilityNotProvided
	}

	if _, ok := req.GetVolumeContext()["mountURL"]; !ok {
		return ErrMountURLNotPresentInPublishContext
	}

	return nil
}

func checkNodeUnpublishVolumeRequest(req *csi.NodeUnpublishVolumeRequest) error {
	if req.GetVolumeId() == "" {
		return ErrVolumeIDNotProvided
	}

	if req.GetTargetPath() == "" {
		return ErrTargetPathNotProvided
	}

	return nil
}

func checkNodeGetVolumeStatsRequest(req *csi.NodeGetVolumeStatsRequest) error {
	if req.GetVolumeId() == "" {
		return ErrVolumeIDNotProvided
	}

	if req.GetVolumePath() == "" {
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
func volumeStats(statfs unix.Statfs_t) (*csi.VolumeUsage, *csi.VolumeUsage, error) {
	// Bsize is an int64 on linux/amd64 and linux/arm64, but an int32 on 32-bit platforms
	blockSize := int64(statfs.Bsize)
	if blockSize < 0 {
		return nil, nil, fmt.Errorf("invalid negative filesystem block size: %d", blockSize)
	}

	// A filesystem reporting more free blocks or inodes than it has in total would otherwise
	// produce a negative used count, so the subtractions are clamped at zero
	usedBlocks := uint64(0)
	if statfs.Blocks > statfs.Bfree {
		usedBlocks = statfs.Blocks - statfs.Bfree
	}

	totalBytes, err := scaledStatfsValue(statfs.Blocks, blockSize)
	if err != nil {
		return nil, nil, fmt.Errorf("calculate total bytes: %w", err)
	}

	availableBytes, err := scaledStatfsValue(statfs.Bavail, blockSize)
	if err != nil {
		return nil, nil, fmt.Errorf("calculate available bytes: %w", err)
	}

	usedBytes, err := scaledStatfsValue(usedBlocks, blockSize)
	if err != nil {
		return nil, nil, fmt.Errorf("calculate used bytes: %w", err)
	}

	bytes := &csi.VolumeUsage{
		Unit:      csi.VolumeUsage_BYTES,
		Total:     totalBytes,
		Available: availableBytes,
		Used:      usedBytes,
	}

	usedInodes := uint64(0)
	if statfs.Files > statfs.Ffree {
		usedInodes = statfs.Files - statfs.Ffree
	}

	totalInodes, err := scaledStatfsValue(statfs.Files, 1)
	if err != nil {
		return nil, nil, fmt.Errorf("calculate total inodes: %w", err)
	}

	availableInodes, err := scaledStatfsValue(statfs.Ffree, 1)
	if err != nil {
		return nil, nil, fmt.Errorf("calculate available inodes: %w", err)
	}

	usedInodeCount, err := scaledStatfsValue(usedInodes, 1)
	if err != nil {
		return nil, nil, fmt.Errorf("calculate used inodes: %w", err)
	}

	inodes := &csi.VolumeUsage{
		Unit:      csi.VolumeUsage_INODES,
		Total:     totalInodes,
		Available: availableInodes,
		Used:      usedInodeCount,
	}

	return bytes, inodes, nil
}

// scaledStatfsValue safely converts a filesystem counter to int64 and applies its multiplier.
func scaledStatfsValue(value uint64, multiplier int64) (int64, error) {
	if multiplier < 0 {
		return 0, fmt.Errorf("multiplier must not be negative: %d", multiplier)
	}

	if multiplier == 0 {
		return 0, nil
	}

	if value > math.MaxInt64 {
		return 0, fmt.Errorf("value %d exceeds int64", value)
	}

	// The upper-bound check above makes this narrowing conversion safe.
	converted := int64(value)
	if converted > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("value %d multiplied by %d exceeds int64", value, multiplier)
	}

	return converted * multiplier, nil
}
