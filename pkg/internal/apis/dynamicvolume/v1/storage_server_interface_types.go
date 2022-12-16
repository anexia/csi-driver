package v1

import (
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
	corev1 "go.anx.io/go-anxcloud/pkg/apis/core/v1"
)

type StorageServerInterface struct {
	gs.GenericService
	gs.HasState

	Identifier string `json:"identifier,omitempty" anxcloud:"identifier"`
	Name       string `json:"name,omitempty"`

	IPAddress IPAddress       `json:"ip_address"`
	Location  corev1.Location `json:"location,omitempty"`
}

type IPAddress struct {
	Identifier string `json:"identifier,omitempty"`
	Name       string `json:"name,omitempty"`
}
