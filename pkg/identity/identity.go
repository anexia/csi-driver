package identity

import (
	"context"

	"github.com/anexia/csi-driver/pkg/types"
	"github.com/anexia/csi-driver/pkg/version"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
)

type identity struct {
	csi.UnimplementedIdentityServer
	logger     logr.Logger
	components types.Components
}

// New creates a fresh instance of the Identitiy component, ready to register to a GRPC server.
func New(logger logr.Logger, components types.Components) (csi.IdentityServer, error) {
	return identity{
		logger:     logger,
		components: components,
	}, nil
}

func (is identity) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          "csi.anx.io",
		VendorVersion: version.Version,
	}, nil
}

func (is identity) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
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

func (is identity) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}
