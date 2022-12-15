package controller

import "errors"

var (
	ErrVolumeIdNotProvided      = errors.New("volume id was not provided")
	ErrNameNotProvided          = errors.New("name was not provided")
	ErrCapacityRangeNotProvided = errors.New("capacity range was not provided")

	ErrVolumeCapabilitiesNotProvided     = errors.New("volume capabilities not set")
	ErrVolumeCapabilitiesNotSupported    = errors.New("volume capabilities not supported")
	ErrVolumeCapabilityBlockNotSupported = errors.New("block volumes are not supported")

	ErrVolumeWithSameNameButDifferentSizeAlreadyExists = errors.New("volume with the same name, but different size already exists")
)
