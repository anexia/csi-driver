package v1

import (
	"context"
	"net/url"
)

func (v *Volume) FilterAPIRequestBody(ctx context.Context) (interface{}, error) {
	return requestBody(ctx, func() interface{} {
		return &struct {
			commonRequestBody
			Volume
			StorageServerInterfaces *string `json:"storage_server_interfaces,omitempty"`
			Prefixes                *string `json:"prefixes,omitempty"`
		}{
			Volume: *v,

			StorageServerInterfaces: joinPointerString(mapPointerSlice(func(s StorageServerInterface) string {
				return s.Identifier
			}, v.StorageServerInterfaces), ","),
			Prefixes: joinPointerString(mapPointerSlice(func(p Prefix) string {
				return p.Identifier
			}, v.Prefixes), ","),
		}
	})
}

func (v *Volume) EndpointURL(ctx context.Context) (*url.URL, error) {
	return endpointURL(ctx, v, "/api/dynamic_volume/v1/volumes.json")
}

func (v *Volume) GetIdentifier(ctx context.Context) (string, error) {
	return v.Identifier, nil
}
