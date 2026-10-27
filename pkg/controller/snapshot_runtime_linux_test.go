//go:build runtimeimage

package controller

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
	"k8s.io/mount-utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("runtime image copier", func() {
	It("can retry a disk-full snapshot without rewriting its intent before cleanup", func() {
		manager, source, snapshot, _ := recoveryFixture()
		source.Path = runtimeMetadataDirectory()
		payload := bytes.Repeat([]byte{0x5a}, 24*1024*1024)
		Expect(os.WriteFile(filepath.Join(source.Path, "data"), payload, 0o600)).To(Succeed())
		filler := filepath.Join(GinkgoT().TempDir(), "filler")
		Expect(os.WriteFile(filler, bytes.Repeat([]byte{0x7f}, 12*1024*1024), 0o600)).To(Succeed())
		var metadata snapshotMetadata
		marker := filepath.Join(snapshot.Path, snapshotMetadataFile)
		for _, newlyCreated := range []bool{true, false} {
			_, err := manager.Create(context.Background(), "snapshot", source, snapshot, newlyCreated)
			Expect(err).To(MatchError(ContainSubstring("copy source volume into snapshot")))
			Expect(err).To(MatchError(ContainSubstring("No space left on device")))
			Expect(readJSON(marker, &metadata)).To(Succeed())
			Expect(metadata.Complete).To(BeFalse())
			manager = &directorySnapshotDataManager{engine: manager.engine, mounter: &recoveryMounter{mount.NewFakeMounter(nil)}, workingDir: manager.workingDir}
		}
		Expect(os.Remove(filler)).To(Succeed())
		_, err := manager.Create(context.Background(), "snapshot", source, snapshot, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(readJSON(marker, &metadata)).To(Succeed())
		Expect(metadata.Complete).To(BeTrue())
		data, err := os.ReadFile(filepath.Join(snapshot.Path, snapshotDataDirectory, "data"))
		Expect(err).ToNot(HaveOccurred())
		Expect(data).To(Equal(payload))
	})

	It("leaves a disk-full restore incomplete and retries it after a restart", func() {
		manager, snapshot, destination, handle := recoveryFixture()
		// CI mounts /tmp as a 32 MiB tmpfs. Keep the source outside that
		// filesystem, otherwise the test would exhaust space before restoring.
		source, err := os.MkdirTemp("/var/tmp", "snapshot-space-test-")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { Expect(os.RemoveAll(source)).To(Succeed()) })
		Expect(copyDirectory(context.Background(), snapshot.Path, source)).To(Succeed())
		snapshot.Path = source
		payload := bytes.Repeat([]byte{0x5a}, 24*1024*1024)
		Expect(os.WriteFile(filepath.Join(source, snapshotDataDirectory, "data"), payload, 0o600)).To(Succeed())
		filler := filepath.Join(GinkgoT().TempDir(), "filler")
		Expect(os.WriteFile(filler, bytes.Repeat([]byte{0x7f}, 12*1024*1024), 0o600)).To(Succeed())
		err = manager.Restore(context.Background(), handle, snapshot, destination, true)
		Expect(err).To(MatchError(ContainSubstring("No space left on device")))
		var metadata restoreMetadata
		Expect(readJSON(filepath.Join(destination.Path, restoreMetadataFile), &metadata)).To(Succeed())
		Expect(metadata.Complete).To(BeFalse())
		Expect(metadata.SnapshotID).To(Equal(handle))
		restarted := &directorySnapshotDataManager{engine: manager.engine, mounter: &recoveryMounter{mount.NewFakeMounter(nil)}, workingDir: manager.workingDir}
		// A retry while still full must reach the copier, not get stuck trying
		// to rewrite its intent on an already exhausted filesystem.
		Expect(restarted.Restore(context.Background(), handle, snapshot, destination, false)).To(MatchError(ContainSubstring("No space left on device")))
		Expect(readJSON(filepath.Join(destination.Path, restoreMetadataFile), &metadata)).To(Succeed())
		Expect(metadata.Complete).To(BeFalse())
		Expect(os.Remove(filler)).To(Succeed())
		Expect(restarted.Restore(context.Background(), handle, snapshot, destination, false)).To(Succeed())
		Expect(readJSON(filepath.Join(destination.Path, restoreMetadataFile), &metadata)).To(Succeed())
		Expect(metadata.Complete).To(BeTrue())
		for _, path := range []string{source, destination.Path} {
			dataPath := filepath.Join(path, "data")
			if path == source {
				dataPath = filepath.Join(path, snapshotDataDirectory, "data")
			}
			data, readErr := os.ReadFile(dataPath)
			Expect(readErr).ToNot(HaveOccurred())
			Expect(data).To(Equal(payload))
		}
	})

	It("copies sparse files through snapshot and restore within the 32 MiB filesystem", func() {
		source := GinkgoT().TempDir()
		snapshot := GinkgoT().TempDir()
		restored := GinkgoT().TempDir()
		file, err := os.Create(filepath.Join(source, "sparse"))
		Expect(err).ToNot(HaveOccurred())
		const logicalSize = 64 * 1024 * 1024
		_, err = file.WriteAt([]byte("start"), 0)
		Expect(err).ToNot(HaveOccurred())
		_, err = file.WriteAt([]byte("end"), logicalSize-3)
		Expect(err).ToNot(HaveOccurred())
		Expect(file.Close()).To(Succeed())
		previous := source
		for _, destination := range []string{snapshot, restored} {
			Expect(copyDirectory(context.Background(), previous, destination)).To(Succeed())
			var stat unix.Stat_t
			Expect(unix.Stat(filepath.Join(destination, "sparse"), &stat)).To(Succeed())
			Expect(stat.Size).To(Equal(int64(logicalSize)))
			Expect(stat.Blocks * 512).To(BeNumerically("<", logicalSize/16))
			data, readErr := os.ReadFile(filepath.Join(destination, "sparse"))
			Expect(readErr).ToNot(HaveOccurred())
			Expect(string(data[:5])).To(Equal("start"))
			Expect(string(data[len(data)-3:])).To(Equal("end"))
			Expect(data[5 : len(data)-3]).To(Equal(make([]byte, logicalSize-8)))
			previous = destination
		}
	})

	It("preserves ACLs, xattrs, sparse files and file metadata through snapshot and restore", func() {
		// Docker Desktop's tmpfs may not support POSIX ACLs. Exercise metadata
		// on the regular container filesystem, without skipping ACL assertions.
		// The separate tmpfs test still catches sparse-file expansion under quota.
		source := runtimeMetadataDirectory()
		snapshot := runtimeMetadataDirectory()
		restored := runtimeMetadataDirectory()
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
			Expect(unix.Setxattr(path, "system.posix_acl_access", acl, 0)).To(Succeed(), "set source ACL on %s; the metadata test requires POSIX ACL support", path)
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

func runtimeMetadataDirectory() string {
	path, err := os.MkdirTemp("/var/tmp", "snapshot-metadata-test-")
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() { Expect(os.RemoveAll(path)).To(Succeed()) })
	return path
}

func runtimeXattr(path, name string) []byte {
	data := make([]byte, 4096)
	size, err := unix.Getxattr(path, name, data)
	Expect(err).ToNot(HaveOccurred())
	return data[:size]
}
