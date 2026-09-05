package node

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
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
