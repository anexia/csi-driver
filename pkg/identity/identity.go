// Package identity implements the CSI Identity service.
package identity

import (
	"context"

	"github.com/anexia/csi-driver/pkg/types"
	"github.com/anexia/csi-driver/pkg/version"
	"github.com/container-storage-interface/spec/lib/go/csi"
)

type identity struct {
	csi.UnimplementedIdentityServer

	components types.Components
}

// New creates a fresh instance of the Identitiy component, ready to register to a GRPC server.
func New(components types.Components) (csi.IdentityServer, error) {
	return identity{
		components: components,
	}, nil
}

func (identity) GetPluginInfo(_ context.Context, _ *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          "csi.anx.io",
		VendorVersion: version.Version,
	}, nil
}

func (is identity) GetPluginCapabilities(_ context.Context, _ *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	capabilities := make([]*csi.PluginCapability, 0, 1)

	if is.components.Has(types.Controller) {
		capabilities = append(capabilities, &csi.PluginCapability{
			Type: &csi.PluginCapability_Service_{
				Service: &csi.PluginCapability_Service{
					Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
				},
			},
		})
	}

	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: capabilities,
	}, nil
}

func (identity) Probe(_ context.Context, _ *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}
