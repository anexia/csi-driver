package v1

import (
	"context"
	"net/url"
)

// EndpointURL returns the Engine API endpoint for storage server interfaces.
func (s *StorageServerInterface) EndpointURL(ctx context.Context) (*url.URL, error) {
	return endpointURL(ctx, s, "/api/dynamic_volume/v1/storage_server_interfaces.json")
}

// GetIdentifier returns the identifier of the storage server interface.
func (s *StorageServerInterface) GetIdentifier(_ context.Context) (string, error) {
	return s.Identifier, nil
}
