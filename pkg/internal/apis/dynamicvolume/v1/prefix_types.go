package v1

import "go.anx.io/go-anxcloud/pkg/apis/common/gs"

type Prefix struct {
	gs.GenericService
	gs.HasState

	Identifier string `json:"identifier,omitempty" anxcloud:"identifier"`
	Prefix     string `json:"prefix,omitempty"`
}
