package node

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/mount-utils"
)

type failingMounter struct {
	*mount.FakeMounter
}

func (*failingMounter) Mount(_ string, _ string, _ string, _ []string) error {
	return errors.New("foo")
}

func (*failingMounter) Unmount(_ string) error {
	return errors.New("foo")
}

var _ = Describe("Node Service", func() {
	Context("NodeGetCapabilities", func() {
		n := &node{}

		capabilities, err := n.NodeGetCapabilities(context.TODO(), &csi.NodeGetCapabilitiesRequest{})

		Expect(err).ToNot(HaveOccurred())
		Expect(capabilities.Capabilities).To(BeEmpty())
	})

	Context("NodePublishVolume", func() {
		var (
			targetPath          string
			canonicalTargetPath string
			validRequest        *csi.NodePublishVolumeRequest
		)

		BeforeEach(func() {
			targetPath = GinkgoT().TempDir()
			var err error
			canonicalTargetPath, err = filepath.EvalSymlinks(targetPath)
			Expect(err).ToNot(HaveOccurred())

			validRequest = &csi.NodePublishVolumeRequest{
				VolumeId:         "foo",
				TargetPath:       targetPath,
				VolumeCapability: &csi.VolumeCapability{},
				VolumeContext: map[string]string{
					"mountURL": "mock-server.test:/foo/bar",
				},
			}
		})

		It("mounts successfully", func() {
			mounter := mount.NewFakeMounter(nil)
			n := &node{mounter: mounter}

			_, err := n.NodePublishVolume(context.TODO(), validRequest)

			Expect(err).ToNot(HaveOccurred())

			mounts, err := mounter.List()
			Expect(err).ToNot(HaveOccurred())
			Expect(mounts).To(HaveLen(1))
			Expect(mounts[0].Type).To(Equal("nfs"))
			Expect(mounts[0].Path).To(Equal(canonicalTargetPath))
			Expect(mounts[0].Device).To(Equal("mock-server.test:/foo/bar"))
			Expect(mounts[0].Opts).ToNot(ContainElement("ro"))
		})

		It("supports readonly mounts", func() {
			validRequest.Readonly = true
			mounter := mount.NewFakeMounter(nil)
			n := &node{mounter: mounter}

			_, err := n.NodePublishVolume(context.TODO(), validRequest)

			Expect(err).ToNot(HaveOccurred())
			mounts, err := mounter.List()
			Expect(err).ToNot(HaveOccurred())
			Expect(mounts).To(HaveLen(1))
			Expect(mounts[0].Opts).To(ContainElement("ro"))
		})

		It("returns an InvalidArgument error when the request check failed", func() {
			n := &node{}

			// empty request is not valid
			_, err := n.NodePublishVolume(context.TODO(), &csi.NodePublishVolumeRequest{})

			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
		})

		It("returns an Internal error when the mount operation failed", func() {
			n := &node{mounter: &failingMounter{mount.NewFakeMounter(nil)}}

			_, err := n.NodePublishVolume(context.TODO(), validRequest)

			Expect(status.Code(err)).To(Equal(codes.Internal))
		})
	})

	Context("NodeUnpublishVolume", func() {
		var (
			validRequest *csi.NodeUnpublishVolumeRequest
			targetPath   string
		)

		BeforeEach(func() {
			var err error
			targetPath, err = os.MkdirTemp("", "csi-driver-*")
			Expect(err).ToNot(HaveOccurred())

			DeferCleanup(func() {
				_ = os.RemoveAll(targetPath)
			})

			validRequest = &csi.NodeUnpublishVolumeRequest{
				VolumeId:   "foo",
				TargetPath: targetPath,
			}
		})

		It("succeeds with a valid request", func() {
			mounter := mount.NewFakeMounter(nil)
			Expect(mounter.Mount("foo", targetPath, "nfs", nil)).To(Succeed())
			n := &node{mounter: mounter}

			_, err := n.NodeUnpublishVolume(context.TODO(), validRequest)

			Expect(err).ToNot(HaveOccurred())
		})

		It("returns an InvalidArgument error when the request check failed", func() {
			n := &node{}

			_, err := n.NodeUnpublishVolume(context.TODO(), &csi.NodeUnpublishVolumeRequest{})

			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
		})

		It("returns an Internal error if the unmount operation failed", func() {
			mounter := mount.NewFakeMounter(nil)
			Expect(mounter.Mount("foo", targetPath, "nfs", nil)).To(Succeed())
			n := &node{mounter: &failingMounter{mounter}}

			_, err := n.NodeUnpublishVolume(context.TODO(), validRequest)

			Expect(status.Code(err)).To(Equal(codes.Internal))
		})
	})

	Context("NodeGetInfo", func() {
		n := &node{nodeID: "foo"}

		nodeInfo, err := n.NodeGetInfo(context.TODO(), &csi.NodeGetInfoRequest{})

		Expect(err).ToNot(HaveOccurred())
		Expect(nodeInfo).To(Equal(&csi.NodeGetInfoResponse{NodeId: "foo"}))
	})
})
