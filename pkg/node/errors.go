package node

import "errors"

var (
	// ErrVolumeIDNotProvided is returned if no volume id was provided
	ErrVolumeIDNotProvided = errors.New("volume id was not provided")
	// ErrTargetPathNotProvided is returned if no target path was provided
	ErrTargetPathNotProvided = errors.New("target path was not provided")
	// ErrVolumeCapabilityNotProvided is returned if no volume capability was provided
	ErrVolumeCapabilityNotProvided = errors.New("volume capability not provided")
	// ErrMountURLNotPresentInPublishContext is returned if no mountURL is present in the PublishContext
	ErrMountURLNotPresentInPublishContext = errors.New("mountURL not present in PublishContext")
)
