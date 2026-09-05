package node

import (
	"path/filepath"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"
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

	Context("checkNodeGetVolumeStatsRequest", func() {
		var req *csi.NodeGetVolumeStatsRequest
		BeforeEach(func() {
			req = &csi.NodeGetVolumeStatsRequest{
				VolumeId:   "foo",
				VolumePath: "/foo/bar",
			}
		})

		It("returns no error if request contains all necessary data", func() {
			err := checkNodeGetVolumeStatsRequest(req)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns an error when no volume id was provided", func() {
			req.VolumeId = ""
			err := checkNodeGetVolumeStatsRequest(req)
			Expect(err).To(MatchError(ErrVolumeIDNotProvided))
		})

		It("returns an error when no volume path was provided", func() {
			req.VolumePath = ""
			err := checkNodeGetVolumeStatsRequest(req)
			Expect(err).To(MatchError(ErrVolumePathNotProvided))
		})
	})

	Context("volumeStats", func() {
		// Sizes are reported in bytes, so block counts have to be scaled by the block
		// size. The blocks reserved for root count as neither used nor available, so
		// Used and Available deliberately fall short of Total by that reserve.
		It("converts filesystem statistics into byte and inode usage", func() {
			bytes, inodes := volumeStats(unix.Statfs_t{
				Bsize:  4096,
				Blocks: 1000,
				Bfree:  400,
				Bavail: 300,
				Files:  500,
				Ffree:  120,
			})

			Expect(bytes).To(Equal(&csi.VolumeUsage{
				Unit:      csi.VolumeUsage_BYTES,
				Total:     4096000,
				Used:      2457600,
				Available: 1228800,
			}))

			Expect(inodes).To(Equal(&csi.VolumeUsage{
				Unit:      csi.VolumeUsage_INODES,
				Total:     500,
				Used:      380,
				Available: 120,
			}))
		})

		// Some filesystems report more free blocks or inodes than they claim to have
		// in total. Subtracting those unguarded yields a negative used count, which
		// kubelet would publish as a nonsensical negative gauge, so it stays at zero.
		It("does not report negative usage when more is free than exists", func() {
			bytes, inodes := volumeStats(unix.Statfs_t{
				Bsize:  4096,
				Blocks: 1,
				Bfree:  2,
				Files:  1,
				Ffree:  2,
			})

			Expect(bytes.Used).To(BeZero())
			Expect(inodes.Used).To(BeZero())
		})

		// A filesystem without inode accounting reports zeroes rather than real counts.
		It("reports zero inode usage for filesystems without inode accounting", func() {
			_, inodes := volumeStats(unix.Statfs_t{Bsize: 4096, Files: 0, Ffree: 0})

			Expect(inodes.Total).To(BeZero())
			Expect(inodes.Used).To(BeZero())
			Expect(inodes.Available).To(BeZero())
		})

		It("reads the statistics of a real path", func() {
			stats, err := statfsUsage(GinkgoT().TempDir())

			Expect(err).ToNot(HaveOccurred())
			Expect(stats.Blocks).To(BeNumerically(">", 0))
		})

		It("returns an error for a path that does not exist", func() {
			_, err := statfsUsage(filepath.Join(GinkgoT().TempDir(), "gone"))
			Expect(err).To(HaveOccurred())
		})
	})
})
