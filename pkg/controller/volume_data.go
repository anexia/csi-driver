package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	dynamicvolumev1 "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
	"go.anx.io/go-anxcloud/pkg/api/types"
	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
)

const (
	cloneMetadataFile = ".csi-anx-clone.json"
)

var errIncompatibleContentSource = errors.New("existing resource has an incompatible content source")

// volumeDataManager copies volume content on the file level, used for cloning.
type volumeDataManager interface {
	Clone(context.Context, *dynamicvolumev1.Volume, *dynamicvolumev1.Volume, bool) error
}

// directoryDataManager implements volumeDataManager by mounting volumes into a
// local working directory and copying the files with cp.
type directoryDataManager struct {
	engine     types.API
	mounter    mount.Interface
	workingDir string
}

// cloneMetadata is stored in the root of a clone target. It records which source
// the volume is filled from and whether the copy finished, so retried
// CreateVolume calls don't copy again or overwrite foreign data.
type cloneMetadata struct {
	Version  int    `json:"version"`
	SourceID string `json:"source_id"`
	Complete bool   `json:"complete"`
}

func (m *cloneMetadata) UnmarshalJSON(data []byte) error {
	type plain cloneMetadata
	decoded := plain{Complete: true}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*m = cloneMetadata(decoded)
	return nil
}

// newDirectoryManager creates a volumeDataManager that works in a temporary directory.
func newDirectoryDataManager(engine types.API) volumeDataManager {
	return &directoryDataManager{
		engine:     engine,
		mounter:    mount.New(""),
		workingDir: filepath.Join(os.TempDir(), "csi-anx-volume-data"),
	}
}

// Clone copies the content of the source volume into the destination volume.
// The source is mounted read-only. Clone metadata in the destination makes
// retries idempotent: a finished clone is not copied again.
func (m *directoryDataManager) Clone(
	ctx context.Context,
	source *dynamicvolumev1.Volume,
	destination *dynamicvolumev1.Volume,
	newlyCreated bool,
) (returnErr error) {
	sourceMount, err := m.mountVolume(ctx, source, true)
	if err != nil {
		return fmt.Errorf("mount source volume: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, sourceMount.cleanup())
	}()

	destinationMount, err := m.mountVolume(ctx, destination, false)
	if err != nil {
		return fmt.Errorf("mount destination volume: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, destinationMount.cleanup())
	}()

	cloneMetadataPath := filepath.Join(destinationMount.path, cloneMetadataFile)
	metadata, complete, err := cloneMetadataForOperation(cloneMetadataPath, "clone:"+source.Identifier, newlyCreated)
	if err != nil {
		return err
	}
	if complete {
		return nil
	}

	if err := writeMissingOperationMetadata(cloneMetadataPath, metadata); err != nil {
		return fmt.Errorf("write incomplete clone metadata: %w", err)
	}
	if err := clearDirectory(destinationMount.path, m.workingDir); err != nil {
		return fmt.Errorf("clear destination volume: %w", err)
	}

	klog.V(2).InfoS("Cloning volume", "source_volume_id", source.Identifier, "destination_volume_id", destination.Identifier)
	if err := copyDirectory(ctx, sourceMount.path, destinationMount.path); err != nil {
		return fmt.Errorf("copy source volume into destination volume: %w", err)
	}
	klog.V(2).InfoS("Finished cloning volume", "source_volume_id", source.Identifier, "destination_volume_id", destination.Identifier)

	metadata.Complete = true
	if err := writeJSON(cloneMetadataPath, metadata); err != nil {
		return fmt.Errorf("write clone metadata: %w", err)
	}

	return nil
}

// cloneMetadataForOperation reads the clone metadata of a target volume. It returns
// errIncompatibleContentSource if the volume belongs to a different operation or
// contains data without metadata.
func cloneMetadataForOperation(path, operationID string, newlyCreated bool) (cloneMetadata, bool, error) {
	metadata := cloneMetadata{Version: 1, SourceID: operationID}
	if newlyCreated {
		return metadata, false, nil
	}

	readErr := readJSON(path, &metadata)
	if os.IsNotExist(readErr) {
		return metadata, false, requireEmptyUnmarkedVolume(path)
	}
	if readErr != nil {
		return cloneMetadata{}, false, fmt.Errorf("read existing clone metadata: %w", readErr)
	}
	if metadata.Version != 1 || metadata.SourceID != operationID {
		return cloneMetadata{}, false, fmt.Errorf("%w: volume name is already used for a different source", errIncompatibleContentSource)
	}

	return metadata, metadata.Complete, nil
}

// requireEmptyUnmarkedVolume fails if a volume without clone metadata already contains data.
func requireEmptyUnmarkedVolume(metadataPath string) error {
	entries, err := os.ReadDir(filepath.Dir(metadataPath))
	if err != nil {
		return fmt.Errorf("inspect unmarked volume: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("%w: refusing to overwrite a non-empty volume without operation metadata", errIncompatibleContentSource)
	}
	return nil
}

// Existing metadata has already been validated by the operation helper. Keep
// that intent intact: after ENOSPC there may be no room to rewrite it until the
// partial copy has been removed. New operations record intent before copying.
func writeMissingOperationMetadata(path string, metadata any) error {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return writeJSON(path, metadata)
	} else if err != nil {
		return err
	}
	return nil
}

// mountedVolume is a volume mounted into the working directory.
type mountedVolume struct {
	path    string
	mounter mount.Interface
}

// mountVolume mounts the NFS export if the volume into a new temporary directory.
func (m *directoryDataManager) mountVolume(ctx context.Context, volume *dynamicvolumev1.Volume, readOnly bool) (*mountedVolume, error) {
	mountURL, err := m.mountURL(ctx, volume)
	if err != nil {
		return nil, err
	}
	if mkdirErr := os.MkdirAll(m.workingDir, 0o750); mkdirErr != nil {
		return nil, fmt.Errorf("create working directory: %w", mkdirErr)
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

// mountURL returns the NFS mount source if the volume.
func (m *directoryDataManager) mountURL(ctx context.Context, volume *dynamicvolumev1.Volume) (string, error) {
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

// cleanup unmounts the volume and removes its mount point.
func (m *mountedVolume) cleanup() error {
	// CleanupMountPoint only removes the temporary directory after the NFS
	// mount has been successfully detached. Never recursively remove this path:
	// if unmounting failed, doing so would delete data from the remote volume.
	return mount.CleanupMountPoint(m.path, m.mounter, true)
}

// copyDirectory copies the content of source into destination, preserving
// permissions, ownership, timestamps, symlinks and sparse files.
func copyDirectory(ctx context.Context, source, destination string) error {
	// The paths are private mount points made with os.MkdirTemp, and exec does
	// not invoke a shell, so neither path can inject command syntax.
	// #nosec G204
	// GNU cp preserves ACLs through mode preservation. List xattr explicitly:
	// --archive/--preserve=all alone can suppress attribute preservation errors.
	command := exec.CommandContext(ctx, "cp", "--recursive", "--no-dereference",
		"--preserve=mode,ownership,timestamps,links,xattr", "--sparse=always", "--",
		source+string(filepath.Separator)+".", destination)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy directory preserving metadata and sparse files: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// clearDirectory removes everything in path except the clone metadata file.
// It refuses paths outside of allowedRoot.
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
		if entry.Name() == cloneMetadataFile {
			continue
		}
		if err := os.RemoveAll(filepath.Join(cleanPath, entry.Name())); err != nil {
			return err
		}
	}

	return nil
}

// readJSON decodes the JSON file at path into target.
func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, target)
}

// writeJSON encodes value as JSON into the file at path.
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
