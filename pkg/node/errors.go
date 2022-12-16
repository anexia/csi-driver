package node

import "errors"

var (
	ErrVolumeIdNotProvided                = errors.New("volume id was not provided")
	ErrTargetPathNotProvided              = errors.New("target path was not provided")
	ErrVolumeCapabilityNotProvided        = errors.New("volume capability not provided")
	ErrMountURLNotPresentInPublishContext = errors.New("mountURL not present in PublishContext")
)
