package v1

import (
	"context"
	"net/url"
)

// FilterAPIRequestBody flattens the referenced storage server interfaces and prefixes
// into the comma-separated identifier lists the Engine API expects.
func (v *Volume) FilterAPIRequestBody(ctx context.Context) (any, error) {
	return requestBody(ctx, func() any {
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

// EndpointURL returns the Engine API endpoint for volumes.
func (v *Volume) EndpointURL(ctx context.Context) (*url.URL, error) {
	return endpointURL(ctx, v, "/api/dynamic_volume/v1/volumes.json")
}

// GetIdentifier returns the identifier of the volume.
func (v *Volume) GetIdentifier(_ context.Context) (string, error) {
	return v.Identifier, nil
}
