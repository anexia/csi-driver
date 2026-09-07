package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	dynamicvolumev1 "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
	"go.anx.io/go-anxcloud/pkg/api/types"
	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
)

const (
	snapshotHandlePrefix  = "anx-directory-snapshot-v1:"
	snapshotDataDirectory = ".csi-anx-snapshot-data"
	snapshotMetadataFile  = ".csi-anx-snapshot.json"
	restoreMetadataFile   = ".csi-anx-restore.json"
)

type snapshotHandle struct {
	BackingVolumeID string `json:"backing_volume_id"`
	SourceVolumeID  string `json:"source_volume_id"`
}

func encodeSnapshotHandle(handle snapshotHandle) (string, error) {
	data, err := json.Marshal(handle)
	if err != nil {
		return "", fmt.Errorf("encode snapshot handle: %w", err)
	}

	return snapshotHandlePrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeSnapshotHandle(value string) (snapshotHandle, error) {
	if !strings.HasPrefix(value, snapshotHandlePrefix) {
		return snapshotHandle{}, errors.New("snapshot handle has an unknown format")
	}

	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, snapshotHandlePrefix))
	if err != nil {
		return snapshotHandle{}, fmt.Errorf("decode snapshot handle: %w", err)
	}

	var handle snapshotHandle
	if err := json.Unmarshal(data, &handle); err != nil {
		return snapshotHandle{}, fmt.Errorf("decode snapshot handle: %w", err)
	}
	if handle.BackingVolumeID == "" || handle.SourceVolumeID == "" {
		return snapshotHandle{}, errors.New("snapshot handle is incomplete")
	}

	return handle, nil
}

type snapshotDataManager interface {
	Create(context.Context, *dynamicvolumev1.Volume, *dynamicvolumev1.Volume, bool) (time.Time, error)
	Restore(context.Context, string, *dynamicvolumev1.Volume, *dynamicvolumev1.Volume, bool) error
}

type directorySnapshotDataManager struct {
	engine     types.API
	mounter    mount.Interface
	workingDir string
}

type snapshotMetadata struct {
	Version        int       `json:"version"`
	SourceVolumeID string    `json:"source_volume_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type restoreMetadata struct {
	Version    int    `json:"version"`
	SnapshotID string `json:"snapshot_id"`
}

func newDirectorySnapshotDataManager(engine types.API) snapshotDataManager {
	return &directorySnapshotDataManager{
		engine:     engine,
		mounter:    mount.New(""),
		workingDir: filepath.Join(os.TempDir(), "csi-anx-snapshots"),
	}
}

func (m *directorySnapshotDataManager) Create(
	ctx context.Context,
	source *dynamicvolumev1.Volume,
	snapshot *dynamicvolumev1.Volume,
	newlyCreated bool,
) (_ time.Time, returnErr error) {
	snapshotMount, err := m.mountVolume(ctx, snapshot, false)
	if err != nil {
		return time.Time{}, fmt.Errorf("mount snapshot volume: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, snapshotMount.cleanup())
	}()

	metadataPath := filepath.Join(snapshotMount.path, snapshotMetadataFile)
	if !newlyCreated {
		var metadata snapshotMetadata
		if readErr := readJSON(metadataPath, &metadata); readErr != nil {
			return time.Time{}, fmt.Errorf("read existing snapshot metadata: %w", readErr)
		}
		if metadata.Version != 1 || metadata.SourceVolumeID != source.Identifier {
			return time.Time{}, errors.New("snapshot name is already used for a different source volume")
		}
		if metadata.CreatedAt.IsZero() {
			return time.Time{}, errors.New("existing snapshot has no creation time")
		}

		return metadata.CreatedAt, nil
	}

	sourceMount, err := m.mountVolume(ctx, source, true)
	if err != nil {
		return time.Time{}, fmt.Errorf("mount source volume: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, sourceMount.cleanup())
	}()

	dataPath := filepath.Join(snapshotMount.path, snapshotDataDirectory)
	if err := os.RemoveAll(dataPath); err != nil {
		return time.Time{}, fmt.Errorf("remove incomplete snapshot data: %w", err)
	}
	if err := os.Mkdir(dataPath, 0o750); err != nil {
		return time.Time{}, fmt.Errorf("create snapshot data directory: %w", err)
	}
	klog.V(2).InfoS("Copying volume data into directory snapshot", "source_volume_id", source.Identifier, "snapshot_volume_id", snapshot.Identifier)
	if err := copyDirectory(ctx, sourceMount.path, dataPath); err != nil {
		return time.Time{}, fmt.Errorf("copy source volume into snapshot: %w", err)
	}
	klog.V(2).InfoS("Finished copying volume data into directory snapshot", "source_volume_id", source.Identifier, "snapshot_volume_id", snapshot.Identifier)

	createdAt := time.Now().UTC()
	metadata := snapshotMetadata{
		Version:        1,
		SourceVolumeID: source.Identifier,
		CreatedAt:      createdAt,
	}
	if err := writeJSON(metadataPath, metadata); err != nil {
		return time.Time{}, fmt.Errorf("write snapshot metadata: %w", err)
	}

	return createdAt, nil
}

func (m *directorySnapshotDataManager) Restore(
	ctx context.Context,
	snapshotID string,
	snapshot *dynamicvolumev1.Volume,
	destination *dynamicvolumev1.Volume,
	newlyCreated bool,
) (returnErr error) {
	snapshotMount, err := m.mountVolume(ctx, snapshot, true)
	if err != nil {
		return fmt.Errorf("mount snapshot volume: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, snapshotMount.cleanup())
	}()

	var snapshotInfo snapshotMetadata
	if readErr := readJSON(filepath.Join(snapshotMount.path, snapshotMetadataFile), &snapshotInfo); readErr != nil {
		return fmt.Errorf("read snapshot metadata: %w", readErr)
	}
	if snapshotInfo.Version != 1 {
		return fmt.Errorf("unsupported snapshot metadata version %d", snapshotInfo.Version)
	}
	handle, err := decodeSnapshotHandle(snapshotID)
	if err != nil {
		return err
	}
	if snapshotInfo.SourceVolumeID != handle.SourceVolumeID || snapshot.Identifier != handle.BackingVolumeID {
		return errors.New("snapshot metadata does not match its handle")
	}

	destinationMount, err := m.mountVolume(ctx, destination, false)
	if err != nil {
		return fmt.Errorf("mount destination volume: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, destinationMount.cleanup())
	}()

	restoreMetadataPath := filepath.Join(destinationMount.path, restoreMetadataFile)
	if !newlyCreated {
		var metadata restoreMetadata
		if err := readJSON(restoreMetadataPath, &metadata); err != nil {
			return fmt.Errorf("existing volume is not a completed restore: %w", err)
		}
		if metadata.Version != 1 || metadata.SnapshotID != snapshotID {
			return errors.New("volume name is already used for a different content source")
		}

		return nil
	}

	if err := clearDirectory(destinationMount.path, m.workingDir); err != nil {
		return fmt.Errorf("clear destination volume: %w", err)
	}
	klog.V(2).InfoS("Restoring directory snapshot into volume", "snapshot_volume_id", snapshot.Identifier, "destination_volume_id", destination.Identifier)
	if err := copyDirectory(ctx, filepath.Join(snapshotMount.path, snapshotDataDirectory), destinationMount.path); err != nil {
		return fmt.Errorf("copy snapshot into destination volume: %w", err)
	}
	klog.V(2).InfoS("Finished restoring directory snapshot into volume", "snapshot_volume_id", snapshot.Identifier, "destination_volume_id", destination.Identifier)
	if err := writeJSON(restoreMetadataPath, restoreMetadata{Version: 1, SnapshotID: snapshotID}); err != nil {
		return fmt.Errorf("write restore metadata: %w", err)
	}

	return nil
}

type mountedVolume struct {
	path    string
	mounter mount.Interface
}

func (m *directorySnapshotDataManager) mountVolume(ctx context.Context, volume *dynamicvolumev1.Volume, readOnly bool) (*mountedVolume, error) {
	mountURL, err := m.mountURL(ctx, volume)
	if err != nil {
		return nil, err
	}
	if mkdirErr := os.MkdirAll(m.workingDir, 0o750); mkdirErr != nil {
		return nil, fmt.Errorf("create snapshot working directory: %w", mkdirErr)
	}

	target, err := os.MkdirTemp(m.workingDir, "volume-")
	if err != nil {
		return nil, fmt.Errorf("create temporary mount directory: %w", err)
	}

	options := []string(nil)
	if readOnly {
		options = []string{"ro"}
	}
	if err := m.mounter.Mount(mountURL, target, "nfs", options); err != nil {
		if removeErr := os.Remove(target); removeErr != nil {
			klog.ErrorS(removeErr, "Failed to remove temporary mount directory", "path", target)
		}
		return nil, err
	}

	return &mountedVolume{path: target, mounter: m.mounter}, nil
}

func (m *directorySnapshotDataManager) mountURL(ctx context.Context, volume *dynamicvolumev1.Volume) (string, error) {
	if volume.Path == "" || volume.StorageServerInterfaces == nil || len(*volume.StorageServerInterfaces) == 0 {
		if err := m.engine.Get(ctx, volume); err != nil {
			return "", fmt.Errorf("retrieve volume: %w", err)
		}
	}
	if volume.StorageServerInterfaces == nil || len(*volume.StorageServerInterfaces) == 0 {
		return "", errors.New("volume has no storage server interface")
	}

	storageServer := &dynamicvolumev1.StorageServerInterface{Identifier: (*volume.StorageServerInterfaces)[0].Identifier}
	if err := m.engine.Get(ctx, storageServer); err != nil {
		return "", fmt.Errorf("retrieve storage server interface: %w", err)
	}

	return createMountURL(volume, storageServer)
}

func (m *mountedVolume) cleanup() error {
	// CleanupMountPoint only removes the temporary directory after the NFS
	// mount has been successfully detached. Never recursively remove this path:
	// if unmounting failed, doing so would delete data from the remote volume.
	return mount.CleanupMountPoint(m.path, m.mounter, true)
}

func copyDirectory(ctx context.Context, source, destination string) error {
	// The paths are private mount points made with os.MkdirTemp, and exec does
	// not invoke a shell, so neither path can inject command syntax.
	// #nosec G204
	command := exec.CommandContext(ctx, "cp", "-a", source+string(filepath.Separator)+".", destination)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cp -a: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

func clearDirectory(path, allowedRoot string) error {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(allowedRoot)
	relativePath, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || !filepath.IsAbs(cleanPath) || !filepath.IsAbs(cleanRoot) || relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to clear unsafe path %q", path)
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(cleanPath, entry.Name())); err != nil {
			return err
		}
	}

	return nil
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, target)
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	temporaryPath := path + ".tmp"
	if err := os.WriteFile(temporaryPath, data, 0o600); err != nil {
		return err
	}

	return os.Rename(temporaryPath, path)
}
