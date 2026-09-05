package identity

import (
	"context"

	"github.com/anexia/csi-driver/pkg/types"
	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetPluginCapabilities", func() {
	pluginCapabilities := func(components types.Components) []*csi.PluginCapability {
		is, err := New(components)
		Expect(err).NotTo(HaveOccurred())

		res, err := is.GetPluginCapabilities(context.Background(), &csi.GetPluginCapabilitiesRequest{})
		Expect(err).NotTo(HaveOccurred())

		return res.GetCapabilities()
	}

	It("advertises the controller service and online volume expansion when the controller is enabled", func() {
		Expect(pluginCapabilities(types.Controller | types.Node)).To(ConsistOf(
			&csi.PluginCapability{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			&csi.PluginCapability{
				Type: &csi.PluginCapability_VolumeExpansion_{
					VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
						Type: csi.PluginCapability_VolumeExpansion_ONLINE,
					},
				},
			},
		))
	})

	It("advertises no capabilities when only the node component is enabled", func() {
		Expect(pluginCapabilities(types.Node)).To(BeEmpty())
	})
})
