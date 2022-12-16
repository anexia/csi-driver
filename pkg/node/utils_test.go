package node

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Node Service Utils", func() {

	Context("checkNodePublishVolumeRequest", func() {
		var req *csi.NodePublishVolumeRequest
		BeforeEach(func() {
			req = &csi.NodePublishVolumeRequest{
				VolumeId:         "foo",
				TargetPath:       "/foo/bar",
				VolumeCapability: &csi.VolumeCapability{},
				VolumeContext: map[string]string{
					"mountURL": "baz",
				},
			}
		})

		It("returns no error if request contains all necessary data", func() {
			err := checkNodePublishVolumeRequest(req)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns an error when no volume id was provided", func() {
			req.VolumeId = ""
			err := checkNodePublishVolumeRequest(req)
			Expect(err).To(MatchError(ErrVolumeIDNotProvided))
		})

		It("returns an error when no target path was provided", func() {
			req.TargetPath = ""
			err := checkNodePublishVolumeRequest(req)
			Expect(err).To(MatchError(ErrTargetPathNotProvided))
		})

		It("returns an error when no volume capability was provided", func() {
			req.VolumeCapability = nil
			err := checkNodePublishVolumeRequest(req)
			Expect(err).To(MatchError(ErrVolumeCapabilityNotProvided))
		})

		It("returns an error when mountURL is not present in VolumeContext", func() {
			req.VolumeContext = nil
			err := checkNodePublishVolumeRequest(req)
			Expect(err).To(MatchError(ErrMountURLNotPresentInPublishContext))
		})
	})

	Context("checkNodeUnpublishVolumeRequest", func() {
		var req *csi.NodeUnpublishVolumeRequest
		BeforeEach(func() {
			req = &csi.NodeUnpublishVolumeRequest{
				VolumeId:   "foo",
				TargetPath: "/foo/bar",
			}
		})

		It("returns no error if request contains all necessary data", func() {
			err := checkNodeUnpublishVolumeRequest(req)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns an error when no volume id was provided", func() {
			req.VolumeId = ""
			err := checkNodeUnpublishVolumeRequest(req)
			Expect(err).To(MatchError(ErrVolumeIDNotProvided))
		})

		It("returns an error when no target path was provided", func() {
			req.TargetPath = ""
			err := checkNodeUnpublishVolumeRequest(req)
			Expect(err).To(MatchError(ErrTargetPathNotProvided))
		})
	})
})
