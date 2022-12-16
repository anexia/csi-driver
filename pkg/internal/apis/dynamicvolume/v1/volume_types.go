package v1

import (
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
)

type Volume struct {
	gs.GenericService
	gs.HasState

	Identifier string `json:"identifier,omitempty" anxcloud:"identifier"`
	Name       string `json:"name,omitempty"`

	StorageServerInterfaces *[]StorageServerInterface `json:"storage_server_interfaces,omitempty"`
	Prefixes                *[]Prefix                 `json:"prefixes,omitempty"`

	ADSClass string `json:"ads_class,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Path     string `json:"path,omitempty"`
	Error    string `json:"error,omitempty"`
}
