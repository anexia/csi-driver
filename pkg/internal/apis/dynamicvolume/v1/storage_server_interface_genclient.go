package v1

import (
	"net/url"

	"golang.org/x/net/context"
)

func (s *StorageServerInterface) EndpointURL(ctx context.Context) (*url.URL, error) {
	return endpointURL(ctx, s, "/api/dynamic_volume/v1/storage_server_interfaces.json")
}

func (s *StorageServerInterface) GetIdentifier(ctx context.Context) (string, error) {
	return s.Identifier, nil
}
