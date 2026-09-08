//go:build runtimeimage

package controller

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("runtime image copier", func() {
	It("preserves ACLs, xattrs, sparse files and file metadata through snapshot and restore", func() {
		source := GinkgoT().TempDir()
		snapshot := GinkgoT().TempDir()
		restored := GinkgoT().TempDir()
		filePath := filepath.Join(source, "sparse")
		file, err := os.Create(filePath)
		Expect(err).ToNot(HaveOccurred())
		const logicalSize = 64 * 1024 * 1024
		_, err = file.WriteAt([]byte("start"), 0)
		Expect(err).ToNot(HaveOccurred())
		_, err = file.WriteAt([]byte("end"), logicalSize-3)
		Expect(err).ToNot(HaveOccurred())
		Expect(file.Close()).To(Succeed())
		Expect(os.Link(filePath, filepath.Join(source, "hardlink"))).To(Succeed())
		Expect(os.Symlink("sparse", filepath.Join(source, "symlink"))).To(Succeed())

		// Linux POSIX ACL xattr: owner rw, named user r, group r, mask r,
		// other none. Use the kernel API so the runtime needs no test tools.
		acl := binary.LittleEndian.AppendUint32(nil, 2)
		for _, entry := range []struct {
			tag, permissions uint16
			id               uint32
		}{
			{1, 6, 0xffffffff}, {2, 4, 23456}, {4, 4, 0xffffffff},
			{16, 4, 0xffffffff}, {32, 0, 0xffffffff},
		} {
			acl = binary.LittleEndian.AppendUint16(acl, entry.tag)
			acl = binary.LittleEndian.AppendUint16(acl, entry.permissions)
			acl = binary.LittleEndian.AppendUint32(acl, entry.id)
		}
		modified := time.Unix(1700000000, 0)
		for _, path := range []string{source, filePath} {
			Expect(os.Chown(path, 12345, 12346)).To(Succeed())
			Expect(unix.Setxattr(path, "user.snapshot-test", []byte("preserve me"), 0)).To(Succeed())
			Expect(unix.Setxattr(path, "system.posix_acl_access", acl, 0)).To(Succeed())
			Expect(os.Chtimes(path, modified, modified)).To(Succeed())
		}

		previous := source
		for _, destination := range []string{snapshot, restored} {
			Expect(copyDirectory(context.Background(), previous, destination)).To(Succeed())
			for _, relative := range []string{".", "sparse"} {
				original := filepath.Join(source, relative)
				copied := filepath.Join(destination, relative)
				for _, attribute := range []string{"user.snapshot-test", "system.posix_acl_access"} {
					Expect(runtimeXattr(copied, attribute)).To(Equal(runtimeXattr(original, attribute)))
				}
				var originalStat, copiedStat unix.Stat_t
				Expect(unix.Stat(original, &originalStat)).To(Succeed())
				Expect(unix.Stat(copied, &copiedStat)).To(Succeed())
				Expect(copiedStat.Uid).To(Equal(originalStat.Uid))
				Expect(copiedStat.Gid).To(Equal(originalStat.Gid))
				Expect(copiedStat.Mode).To(Equal(originalStat.Mode))
				Expect(copiedStat.Mtim).To(Equal(originalStat.Mtim))
			}
			var sparseStat, linkStat unix.Stat_t
			Expect(unix.Stat(filepath.Join(destination, "sparse"), &sparseStat)).To(Succeed())
			Expect(unix.Stat(filepath.Join(destination, "hardlink"), &linkStat)).To(Succeed())
			Expect(sparseStat.Size).To(Equal(int64(logicalSize)))
			Expect(sparseStat.Blocks * 512).To(BeNumerically("<", logicalSize/16))
			Expect(linkStat.Ino).To(Equal(sparseStat.Ino))
			target, linkErr := os.Readlink(filepath.Join(destination, "symlink"))
			Expect(linkErr).ToNot(HaveOccurred())
			Expect(target).To(Equal("sparse"))
			data, readErr := os.ReadFile(filepath.Join(destination, "sparse"))
			Expect(readErr).ToNot(HaveOccurred())
			Expect(string(data[:5])).To(Equal("start"))
			Expect(string(data[len(data)-3:])).To(Equal("end"))
			Expect(data[5 : len(data)-3]).To(Equal(make([]byte, logicalSize-8)))
			previous = destination
		}
	})
})

func runtimeXattr(path, name string) []byte {
	data := make([]byte, 4096)
	size, err := unix.Getxattr(path, name, data)
	Expect(err).ToNot(HaveOccurred())
	return data[:size]
}
