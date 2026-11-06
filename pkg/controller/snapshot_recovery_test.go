package controller

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	dynamicvolumev1 "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
	"github.com/anexia/csi-driver/pkg/internal/mockapi"
	"github.com/golang/mock/gomock"
	"k8s.io/mount-utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Snapshot recovery sanity", func() {
	It("recovers an interrupted snapshot copy after a manager restart", func() {
		manager, source, snapshot, _ := recoveryFixture()
		source.Path = GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(source.Path, "data"), []byte("snapshot data"), 0o600)).To(Succeed())
		installRecoveryCopier(true)
		_, err := manager.Create(context.Background(), "snapshot", source, snapshot, true)
		Expect(err).To(HaveOccurred())
		var metadata snapshotMetadata
		marker := filepath.Join(snapshot.Path, snapshotMetadataFile)
		Expect(readJSON(marker, &metadata)).To(Succeed())
		Expect(metadata.Complete).To(BeFalse())
		Expect(metadata.SourceVolumeID).To(Equal(source.Identifier))

		restarted := &directorySnapshotDataManager{engine: manager.engine, mounter: &recoveryMounter{mount.NewFakeMounter(nil)}, workingDir: manager.workingDir}
		installRecoveryCopier(false)
		createdAt, err := restarted.Create(context.Background(), "snapshot", source, snapshot, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(readJSON(marker, &metadata)).To(Succeed())
		Expect(metadata.Complete).To(BeTrue())
		Expect(metadata.CreatedAt).To(Equal(createdAt))
		_, err = os.Stat(filepath.Join(snapshot.Path, snapshotDataDirectory, "partial"))
		Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
		data, err := os.ReadFile(filepath.Join(snapshot.Path, snapshotDataDirectory, "data"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal("snapshot data"))
		// A completed snapshot must not change when retried after source writes.
		Expect(os.WriteFile(filepath.Join(source.Path, "data"), []byte("later data"), 0o600)).To(Succeed())
		installRecoveryCopier(true)
		_, err = restarted.Create(context.Background(), "snapshot", source, snapshot, false)
		Expect(err).ToNot(HaveOccurred())
		data, err = os.ReadFile(filepath.Join(snapshot.Path, snapshotDataDirectory, "data"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal("snapshot data"))
	})

	It("rejects an incomplete snapshot before touching the restore destination", func() {
		manager, snapshot, destination, handle := recoveryFixture()
		Expect(writeJSON(filepath.Join(snapshot.Path, snapshotMetadataFile), snapshotMetadata{Version: 1, SourceVolumeID: "source", Complete: false})).To(Succeed())
		path := filepath.Join(destination.Path, "workload")
		Expect(os.WriteFile(path, []byte("keep"), 0o600)).To(Succeed())
		Expect(manager.Restore(context.Background(), handle, snapshot, destination, false)).To(MatchError(ContainSubstring("snapshot copy is incomplete")))
		data, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal("keep"))
	})

	It("recovers after cancellation during a copy", func() {
		manager, snapshot, destination, handle := recoveryFixture()
		// Stop the copier after writing partial data so cancellation is deterministic.
		installRecoveryCopyScript("#!/bin/sh\nfor argument do target=$argument; done\nprintf partial > \"$target/partial\"\nkill -STOP $$\n")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			result <- manager.Restore(ctx, handle, snapshot, destination, true)
		}()
		Eventually(func() error { _, err := os.Stat(filepath.Join(destination.Path, "partial")); return err }, 10*time.Second).Should(Succeed())
		cancel()
		Eventually(result, 10*time.Second).Should(Receive(HaveOccurred()))
		var metadata restoreMetadata
		Expect(readJSON(filepath.Join(destination.Path, restoreMetadataFile), &metadata)).To(Succeed())
		Expect(metadata.Complete).To(BeFalse())
		installRecoveryCopier(false)
		restarted := &directorySnapshotDataManager{engine: manager.engine, mounter: &recoveryMounter{mount.NewFakeMounter(nil)}, workingDir: manager.workingDir}
		Expect(restarted.Restore(context.Background(), handle, snapshot, destination, false)).To(Succeed())
		Expect(readJSON(filepath.Join(destination.Path, restoreMetadataFile), &metadata)).To(Succeed())
		Expect(metadata.Complete).To(BeTrue())
	})

	It("keeps the restore intent while clearing partial data", func() {
		destination := GinkgoT().TempDir()
		marker := filepath.Join(destination, restoreMetadataFile)
		Expect(writeJSON(marker, restoreMetadata{Version: 1, SnapshotID: "snapshot"})).To(Succeed())
		Expect(os.WriteFile(filepath.Join(destination, "partial"), []byte("partial"), 0o600)).To(Succeed())
		Expect(clearDirectory(destination, filepath.Dir(destination))).To(Succeed())
		var metadata restoreMetadata
		Expect(readJSON(marker, &metadata)).To(Succeed())
		Expect(metadata.SnapshotID).To(Equal("snapshot"))
		Expect(metadata.Complete).To(BeFalse())
	})

	It("retries a failed copy after a manager restart and leaves completed restores alone", func() {
		manager, snapshot, destination, handle := recoveryFixture()
		installRecoveryCopier(true)
		Expect(manager.Restore(context.Background(), handle, snapshot, destination, true)).ToNot(Succeed())
		var metadata restoreMetadata
		Expect(readJSON(filepath.Join(destination.Path, restoreMetadataFile), &metadata)).To(Succeed())
		Expect(metadata.Complete).To(BeFalse())
		Expect(metadata.SnapshotID).To(Equal(handle))
		_, err := os.Stat(filepath.Join(destination.Path, "partial"))
		Expect(err).ToNot(HaveOccurred())

		// No in-memory operation state survives the restart.
		restarted := &directorySnapshotDataManager{engine: manager.engine, mounter: &recoveryMounter{mount.NewFakeMounter(nil)}, workingDir: manager.workingDir}
		installRecoveryCopier(false)
		Expect(restarted.Restore(context.Background(), handle, snapshot, destination, false)).To(Succeed())
		Expect(readJSON(filepath.Join(destination.Path, restoreMetadataFile), &metadata)).To(Succeed())
		Expect(metadata.Complete).To(BeTrue())
		_, err = os.Stat(filepath.Join(destination.Path, "partial"))
		Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
		data, err := os.ReadFile(filepath.Join(destination.Path, "data"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal("snapshot data"))
		Expect(os.WriteFile(filepath.Join(destination.Path, "data"), []byte("new workload data"), 0o600)).To(Succeed())
		installRecoveryCopier(true)
		Expect(restarted.Restore(context.Background(), handle, snapshot, destination, false)).To(Succeed())
		data, err = os.ReadFile(filepath.Join(destination.Path, "data"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal("new workload data"))
	})

	It("does not clear an existing volume with no restore intent", func() {
		manager, snapshot, destination, handle := recoveryFixture()
		path := filepath.Join(destination.Path, "workload")
		Expect(os.WriteFile(path, []byte("keep"), 0o600)).To(Succeed())
		installRecoveryCopier(false)
		Expect(manager.Restore(context.Background(), handle, snapshot, destination, false)).ToNot(Succeed())
		data, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal("keep"))
	})
	It("rejects an unmarked non-empty restore destination", func() {
		destination := GinkgoT().TempDir()
		path := filepath.Join(destination, "application-data")
		Expect(os.WriteFile(path, []byte("must survive"), 0o600)).To(Succeed())
		_, _, err := restoreMetadataForOperation(filepath.Join(destination, restoreMetadataFile), "snapshot", false)
		Expect(err).To(HaveOccurred())
		data, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal("must survive"))
	})

	It("recognizes legacy completed snapshots without recopying", func() {
		path := filepath.Join(GinkgoT().TempDir(), snapshotMetadataFile)
		Expect(os.WriteFile(path, []byte(`{"version":1,"snapshot_name":"snapshot","source_volume_id":"source","created_at":"2026-09-07T10:00:00Z"}`), 0o600)).To(Succeed())
		_, complete, err := snapshotMetadataForOperation(path, "snapshot", "source", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(complete).To(BeTrue())
	})

	It("recognizes legacy completed restores without overwriting workload data", func() {
		path := filepath.Join(GinkgoT().TempDir(), restoreMetadataFile)
		Expect(os.WriteFile(path, []byte(`{"version":1,"snapshot_id":"snapshot"}`), 0o600)).To(Succeed())
		_, complete, err := restoreMetadataForOperation(path, "snapshot", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(complete).To(BeTrue())
	})

	It("does not infer ownership from incomplete JSON fields", func() {
		path := filepath.Join(GinkgoT().TempDir(), restoreMetadataFile)
		Expect(os.WriteFile(path, []byte(`{"version":1,"complete":false}`), 0o600)).To(Succeed())
		_, _, err := restoreMetadataForOperation(path, "snapshot", false)
		Expect(err).To(HaveOccurred())
	})
})

// Substitute only NFS mounting. The manager still performs real filesystem
// metadata reads/writes, cleanup and subprocess execution.
type recoveryMounter struct{ *mount.FakeMounter }

func (m *recoveryMounter) Mount(source, target, fsType string, options []string) error {
	if err := os.Remove(target); err != nil {
		return err
	}
	if err := os.Symlink(strings.TrimPrefix(source, "test:"), target); err != nil {
		return err
	}
	return m.FakeMounter.Mount(source, target, fsType, options)
}

func recoveryFixture() (*directorySnapshotDataManager, *dynamicvolumev1.Volume, *dynamicvolumev1.Volume, string) {
	engine := mockapi.NewMockAPI(gomock.NewController(GinkgoT()))
	engine.EXPECT().Get(gomock.Any(), &dynamicvolumev1.StorageServerInterface{Identifier: "server"}).DoAndReturn(func(_ context.Context, server *dynamicvolumev1.StorageServerInterface, _ ...any) error {
		server.IPAddress.Name = "test"
		return nil
	}).AnyTimes()
	snapshot := &dynamicvolumev1.Volume{Identifier: "snapshot", Path: GinkgoT().TempDir(), Size: 1024, StorageServerInterfaces: &[]dynamicvolumev1.StorageServerInterface{{Identifier: "server"}}}
	destination := &dynamicvolumev1.Volume{Identifier: "destination", Path: GinkgoT().TempDir(), Size: 1024, StorageServerInterfaces: snapshot.StorageServerInterfaces}
	Expect(os.Mkdir(filepath.Join(snapshot.Path, snapshotDataDirectory), 0o750)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(snapshot.Path, snapshotDataDirectory, "data"), []byte("snapshot data"), 0o600)).To(Succeed())
	Expect(writeJSON(filepath.Join(snapshot.Path, snapshotMetadataFile), snapshotMetadata{Version: 1, SourceVolumeID: "source", SnapshotName: "snapshot", CreatedAt: time.Now(), Complete: true})).To(Succeed())
	handle, err := encodeSnapshotHandle(snapshotHandle{BackingVolumeID: "snapshot", SourceVolumeID: "source"})
	Expect(err).ToNot(HaveOccurred())
	return &directorySnapshotDataManager{engine: engine, mounter: &recoveryMounter{mount.NewFakeMounter(nil)}, workingDir: GinkgoT().TempDir()}, snapshot, destination, handle
}

func installRecoveryCopier(fail bool) {
	script := "#!/bin/sh\nprevious=''\ntarget=''\nfor argument do previous=$target; target=$argument; done\n"
	if fail {
		script += "printf partial > \"$target/partial\"\nexit 1\n"
	} else {
		script += "exec /bin/cp -R \"$previous\" \"$target\"\n"
	}
	installRecoveryCopyScript(script)
}

func installRecoveryCopyScript(script string) {
	// Docker's constrained /tmp is noexec. Keep executable fixtures on the
	// regular filesystem while leaving volume data on the constrained tmpfs.
	directory, err := os.MkdirTemp("/var/tmp", "snapshot-copier-test-")
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() { Expect(os.RemoveAll(directory)).To(Succeed()) })
	path := filepath.Join(directory, "cp")
	Expect(os.WriteFile(path, []byte(script), 0o700)).To(Succeed())
	GinkgoT().Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	resolved, err := exec.LookPath("cp")
	Expect(err).ToNot(HaveOccurred())
	Expect(resolved).To(Equal(path), "fault injection must not fall back to the real copier")
}
