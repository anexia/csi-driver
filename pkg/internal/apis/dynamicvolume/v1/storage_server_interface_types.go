package v1

import (
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"
)

// StorageServerInterface is the network interface of a storage server serving dynamic volumes.
type StorageServerInterface struct {
	gs.GenericService
	gs.HasState

	Identifier string `json:"identifier,omitempty" anxcloud:"identifier"`
	Name       string `json:"name,omitempty"`

	IPAddress IPAddress       `json:"ip_address"`
	Location  corev1.Location `json:"location"`
}

// IPAddress is the IP address assigned to a storage server interface.
type IPAddress struct {
	Identifier string `json:"identifier,omitempty"`
	Name       string `json:"name,omitempty"`
}
