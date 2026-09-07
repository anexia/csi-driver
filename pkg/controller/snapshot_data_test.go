package controller

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Directory snapshot data", func() {
	It("serializes operations only when they use the same key", func() {
		var locks operationLocks

		Expect(locks.TryAcquire("snapshot:first")).To(BeTrue())
		Expect(locks.TryAcquire("snapshot:first")).To(BeFalse())
		Expect(locks.TryAcquire("snapshot:second")).To(BeTrue())
		locks.Release("snapshot:first")
		Expect(locks.TryAcquire("snapshot:first")).To(BeTrue())
	})

	It("round-trips opaque snapshot handles", func() {
		expected := snapshotHandle{
			BackingVolumeID: "snapshot-volume",
			SourceVolumeID:  "source-volume",
		}

		encoded, err := encodeSnapshotHandle(expected)
		Expect(err).ToNot(HaveOccurred())
		Expect(encoded).To(HavePrefix(snapshotHandlePrefix))

		decoded, err := decodeSnapshotHandle(encoded)
		Expect(err).ToNot(HaveOccurred())
		Expect(decoded).To(Equal(expected))
	})

	DescribeTable("rejects invalid snapshot handles", func(value string) {
		_, err := decodeSnapshotHandle(value)
		Expect(err).To(HaveOccurred())
	},
		Entry("unknown format", "snapshot-volume"),
		Entry("invalid base64", snapshotHandlePrefix+"%%%"),
		Entry("incomplete data", snapshotHandlePrefix+"e30"),
	)

	It("recursively copies a directory while preserving symlinks", func() {
		source := GinkgoT().TempDir()
		destination := GinkgoT().TempDir()
		Expect(os.Mkdir(filepath.Join(source, "nested"), 0o750)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(source, "nested", "file.txt"), []byte("snapshot data"), 0o640)).To(Succeed())
		Expect(os.Symlink(filepath.Join("nested", "file.txt"), filepath.Join(source, "link"))).To(Succeed())

		Expect(copyDirectory(context.Background(), source, destination)).To(Succeed())

		data, err := os.ReadFile(filepath.Join(destination, "nested", "file.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(data).To(Equal([]byte("snapshot data")))
		linkTarget, err := os.Readlink(filepath.Join(destination, "link"))
		Expect(err).ToNot(HaveOccurred())
		Expect(linkTarget).To(Equal(filepath.Join("nested", "file.txt")))
	})

	It("clears only the contents of a safe destination directory", func() {
		destination := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(destination, "file.txt"), []byte("data"), 0o600)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(destination, "nested"), 0o750)).To(Succeed())

		allowedRoot := filepath.Dir(destination)
		Expect(clearDirectory(destination, allowedRoot)).To(Succeed())
		entries, err := os.ReadDir(destination)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
		Expect(clearDirectory("relative/path", allowedRoot)).To(MatchError(ContainSubstring("unsafe path")))
		Expect(clearDirectory(string(filepath.Separator), allowedRoot)).To(MatchError(ContainSubstring("unsafe path")))
		Expect(clearDirectory(allowedRoot, allowedRoot)).To(MatchError(ContainSubstring("unsafe path")))
	})

	It("writes and reads snapshot metadata", func() {
		path := filepath.Join(GinkgoT().TempDir(), "metadata.json")
		expected := snapshotMetadata{
			Version:        1,
			SourceVolumeID: "source-volume",
			CreatedAt:      time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC),
		}

		Expect(writeJSON(path, expected)).To(Succeed())
		var actual snapshotMetadata
		Expect(readJSON(path, &actual)).To(Succeed())
		Expect(actual).To(Equal(expected))
	})
})
