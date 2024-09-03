package controller

import "errors"

var (
	// ErrVolumeIDNotProvided is returned if no volume id was provided
	ErrVolumeIDNotProvided = errors.New("volume id was not provided")
	// ErrNameNotProvided is returned if no name was provided
	ErrNameNotProvided = errors.New("name was not provided")
	// ErrCapacityRangeNotProvided is returned if no capacity range was provided
	ErrCapacityRangeNotProvided = errors.New("capacity range was not provided")

	// ErrVolumeCapabilitiesNotProvided is returned if volumes capabilities haven't been set
	ErrVolumeCapabilitiesNotProvided = errors.New("volume capabilities not set")
	// ErrVolumeCapabilitiesNotSupported is returned if set volume capabilities are not supported
	ErrVolumeCapabilitiesNotSupported = errors.New("volume capabilities not supported")
	// ErrVolumeCapabilityBlockNotSupported is returned if volume capabilities have been set to unsupported block mode
	ErrVolumeCapabilityBlockNotSupported = errors.New("block volumes are not supported")

	// ErrVolumeWithSameNameButDifferentSizeAlreadyExists is returned if a volume with the same name but different size already exists
	ErrVolumeWithSameNameButDifferentSizeAlreadyExists = errors.New("volume with the same name, but different size already exists")

	// ErrQueryingIPAddressesFailed is returned whenever we actually receive a
	// storage server interface from the Engine, but that has no IP addresses. This is
	// almost always due to missing IPAM permissions.
	ErrQueryingIPAddressesFailed = errors.New("engine returned no IP addresses for storage server interface, likely due to missing permissions on the token")
)
