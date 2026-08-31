package node

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/mount-utils"
)

type failingMounter struct {
	*mount.FakeMounter
}

func (fm *failingMounter) Mount(source string, target string, fstype string, options []string) error {
	return errors.New("foo")
}

func (fm *failingMounter) Unmount(target string) error {
	return errors.New("foo")
}

var _ = Describe("Node Service", func() {
	Context("NodeGetCapabilities", func() {
		It("advertises the volume stats capability", func() {
			n := &node{}

			capabilities, err := n.NodeGetCapabilities(context.TODO(), &csi.NodeGetCapabilitiesRequest{})

			Expect(err).ToNot(HaveOccurred())
			Expect(capabilities.Capabilities).To(HaveLen(1))
			Expect(capabilities.Capabilities[0].GetRpc().GetType()).
				To(Equal(csi.NodeServiceCapability_RPC_GET_VOLUME_STATS))
		})
	})

	Context("NodeGetVolumeStats", func() {
		var (
			volumePath   string
			mounter      *mount.FakeMounter
			validRequest *csi.NodeGetVolumeStatsRequest
		)

		BeforeEach(func() {
			var err error
			// resolve symlinks so the fixture also holds on macOS, where the temp dir
			// is reached through /var -> /private/var
			volumePath, err = filepath.EvalSymlinks(GinkgoT().TempDir())
			Expect(err).ToNot(HaveOccurred())

			// the volume path kubelet asks about is normally a live mount, so the
			// default fixture models exactly that
			mounter = mount.NewFakeMounter([]mount.MountPoint{
				{Device: "mock-server.test:/foo/bar", Path: volumePath, Type: "nfs"},
			})
			validRequest = &csi.NodeGetVolumeStatsRequest{
				VolumeId:   "foo",
				VolumePath: volumePath,
			}
		})

		It("returns byte and inode usage for a valid request", func() {
			n := &node{mounter: mounter}

			stats, err := n.NodeGetVolumeStats(context.TODO(), validRequest)

			Expect(err).ToNot(HaveOccurred())
			Expect(stats.Usage).To(HaveLen(2))
			Expect(stats.Usage[0].Unit).To(Equal(csi.VolumeUsage_BYTES))
			Expect(stats.Usage[1].Unit).To(Equal(csi.VolumeUsage_INODES))
		})

		// The numbers kubelet turns into kubelet_volume_stats_* metrics have to be
		// the real filesystem numbers. Asserting only on the units would still pass
		// if we reported raw block counts as bytes, or swapped total and available,
		// so pin the values against a filesystem whose statistics we control.
		It("reports the actual filesystem numbers, not just plausible ones", func() {
			var measured string
			n := &node{
				mounter: mounter,
				statfs: func(path string) (unix.Statfs_t, error) {
					measured = path
					return unix.Statfs_t{
						Bsize:  4096,
						Blocks: 1000, // 4096000 bytes total
						Bfree:  400,  // 600 blocks used  -> 2457600 bytes
						Bavail: 300,  // 300 blocks free to us -> 1228800 bytes
						Files:  500,
						Ffree:  120,
					}, nil
				},
			}

			stats, err := n.NodeGetVolumeStats(context.TODO(), validRequest)

			Expect(err).ToNot(HaveOccurred())
			// the numbers have to describe the volume kubelet asked about, not some
			// other filesystem such as the node root
			Expect(measured).To(Equal(volumePath))
			Expect(stats.Usage).To(HaveLen(2))

			Expect(stats.Usage[0]).To(Equal(&csi.VolumeUsage{
				Unit:      csi.VolumeUsage_BYTES,
				Total:     4096000,
				Used:      2457600,
				Available: 1228800,
			}))
			Expect(stats.Usage[1]).To(Equal(&csi.VolumeUsage{
				Unit:      csi.VolumeUsage_INODES,
				Total:     500,
				Used:      380,
				Available: 120,
			}))
		})

		It("returns an InvalidArgument error when the request check failed", func() {
			n := &node{mounter: mounter}

			_, err := n.NodeGetVolumeStats(context.TODO(), &csi.NodeGetVolumeStatsRequest{})

			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
		})

		It("returns a NotFound error when the volume path does not exist", func() {
			n := &node{mounter: mounter}
			validRequest.VolumePath = filepath.Join(volumePath, "gone")

			_, err := n.NodeGetVolumeStats(context.TODO(), validRequest)

			Expect(status.Code(err)).To(Equal(codes.NotFound))
		})

		// A directory that exists but carries no mount is not the volume kubelet is
		// asking about - it is the node's own root filesystem showing through.
		// Reporting its usage would attribute hundreds of gigabytes of node disk to
		// a small PVC, so this has to be NotFound rather than a successful answer.
		It("returns a NotFound error when the volume path is not a mount point", func() {
			n := &node{mounter: mount.NewFakeMounter(nil)}

			_, err := n.NodeGetVolumeStats(context.TODO(), validRequest)

			Expect(status.Code(err)).To(Equal(codes.NotFound))
		})

		// The volume can be unmounted between the mount check and the measurement,
		// which kubelet sees as the volume being gone rather than as a driver fault.
		It("returns a NotFound error when the volume disappears while being measured", func() {
			n := &node{
				mounter: mounter,
				statfs: func(path string) (unix.Statfs_t, error) {
					return unix.Statfs_t{}, os.ErrNotExist
				},
			}

			_, err := n.NodeGetVolumeStats(context.TODO(), validRequest)

			Expect(status.Code(err)).To(Equal(codes.NotFound))
		})

		It("returns an Internal error when the volume path cannot be inspected", func() {
			// a mounter that cannot answer the mount check at all stands in for
			// errors like EACCES, which are neither "missing" nor a usable answer
			failing := mount.NewFakeMounter(nil)
			failing.MountCheckErrors = map[string]error{volumePath: errors.New("permission denied")}
			n := &node{mounter: failing}

			_, err := n.NodeGetVolumeStats(context.TODO(), validRequest)

			Expect(status.Code(err)).To(Equal(codes.Internal))
		})

		It("returns an Internal error when collecting the stats fails", func() {
			n := &node{
				mounter: mounter,
				statfs: func(path string) (unix.Statfs_t, error) {
					return unix.Statfs_t{}, errors.New("statfs failed")
				},
			}

			_, err := n.NodeGetVolumeStats(context.TODO(), validRequest)

			Expect(status.Code(err)).To(Equal(codes.Internal))
		})
	})

	Context("NodePublishVolume", func() {
		var (
			targetPath   string
			validRequest *csi.NodePublishVolumeRequest
		)

		BeforeEach(func() {
			targetPath = GinkgoT().TempDir()
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
			Expect(mounts[0].Path).To(Equal(targetPath))
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
			n := &node{mounter: mount.NewFakeMounter([]mount.MountPoint{
				{Device: "foo", Path: targetPath, Type: "nfs"},
			})}

			_, err := n.NodeUnpublishVolume(context.TODO(), validRequest)

			Expect(err).ToNot(HaveOccurred())
		})

		It("returns an InvalidArgument error when the request check failed", func() {
			n := &node{}

			_, err := n.NodeUnpublishVolume(context.TODO(), &csi.NodeUnpublishVolumeRequest{})

			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
		})

		It("returns an Internal error if the unmount operation failed", func() {
			n := &node{mounter: &failingMounter{mount.NewFakeMounter([]mount.MountPoint{
				{Device: "foo", Path: targetPath, Type: "nfs"},
			})}}

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
