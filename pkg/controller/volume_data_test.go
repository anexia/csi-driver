package controller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Directory volume data", func() {
	It("serializes operations only when they use the same key", func() {
		var locks operationLocks

		Expect(locks.TryAcquire("clone:first")).To(BeTrue())
		Expect(locks.TryAcquire("clone:first")).To(BeFalse())
		Expect(locks.TryAcquire("clone:second")).To(BeTrue())
		locks.Release("clone:first")
		Expect(locks.TryAcquire("clone:first")).To(BeTrue())
	})

	It("recursively copies a directory while preserving symlinks", func() {
		if runtime.GOOS != "linux" {
			Skip("requires the Linux runtime's GNU cp; covered by the runtime image test")
		}
		source := GinkgoT().TempDir()
		destination := GinkgoT().TempDir()
		Expect(os.Mkdir(filepath.Join(source, "nested"), 0o750)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(source, "nested", "file.txt"), []byte("clone data"), 0o640)).To(Succeed())
		Expect(os.Symlink(filepath.Join("nested", "file.txt"), filepath.Join(source, "link"))).To(Succeed())

		Expect(copyDirectory(context.Background(), source, destination)).To(Succeed())

		data, err := os.ReadFile(filepath.Join(destination, "nested", "file.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(data).To(Equal([]byte("clone data")))
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

	It("rejects incompatible metadata instead of recovering it", func() {
		metadataPath := filepath.Join(GinkgoT().TempDir(), cloneMetadataFile)
		Expect(writeJSON(metadataPath, cloneMetadata{Version: 1, SourceID: "other-clone"})).To(Succeed())

		_, _, err := cloneMetadataForOperation(metadataPath, "clone-id", false)
		Expect(err).To(MatchError(ContainSubstring("incompatible content source")))
		Expect(errors.Is(err, errIncompatibleContentSource)).To(BeTrue())
	})
})
