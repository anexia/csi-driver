package node

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
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
