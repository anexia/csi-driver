package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	dv "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hashicorp/go-multierror"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func engineErrorToGRPC(err error) error {
	// no err || already has known code? -> keep it
	if err == nil || status.Code(err) != codes.Unknown {
		return err
	}

	httpError := api.HTTPError{}
	if errors.As(err, &httpError) {
		switch httpError.StatusCode() {
		case http.StatusNotFound:
			return status.Errorf(codes.NotFound, "resource not found: %s", err)
		case http.StatusInternalServerError:
			return status.Errorf(codes.Internal, "internal server error: %s", err)
		}
	}

	return err
}

func checkVolumeCapabilities(volumeCapabilities []*csi.VolumeCapability) error {
	var res error

	for _, capability := range volumeCapabilities {
		if capability.GetBlock() != nil {
			res = multierror.Append(res, ErrVolumeCapabilityBlockNotSupported)
		}
	}

	return res
}

func checkCreateVolumeRequest(req *csi.CreateVolumeRequest) error {
	if req.Name == "" {
		return ErrNameNotProvided
	}

	if req.CapacityRange == nil {
		return ErrCapacityRangeNotProvided
	}

	if req.VolumeCapabilities == nil {
		return ErrVolumeCapabilitiesNotProvided
	}

	if err := checkVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return fmt.Errorf("unsuported volume capabilities: %w", err)
	}

	return nil
}

func checkDeleteVolumeRequest(req *csi.DeleteVolumeRequest) error {
	if req.VolumeId == "" {
		return ErrVolumeIdNotProvided
	}

	return nil
}

func checkValidateVolumeCapabilitiesRequest(req *csi.ValidateVolumeCapabilitiesRequest) error {
	if req.VolumeId == "" {
		return ErrVolumeIdNotProvided
	}

	if req.VolumeCapabilities == nil || len(req.VolumeCapabilities) == 0 {
		return ErrVolumeCapabilitiesNotProvided
	}

	return nil
}

func createAnexiaDynamicVolumeFromRequest(ctx context.Context, engine types.API, req *csi.CreateVolumeRequest) (*dv.Volume, error) {
	newVolume := dv.Volume{
		Name:                    req.GetName(),
		Size:                    req.GetCapacityRange().GetRequiredBytes(),
		StorageServerInterfaces: &[]dv.StorageServerInterface{{Identifier: req.Parameters["csi.anx.io/storage-server-identifier"]}},
		ADSClass:                req.Parameters["csi.anx.io/ads-class"],
	}

	if err := engine.Create(ctx, &newVolume); err != nil {
		return nil, fmt.Errorf("create volume: %w", err)
	}

	if err := gs.AwaitCompletion(ctx, engine, &newVolume); err != nil {
		// volume with name already exist?
		// -> same capacity: ok; different capacity: error
		if strings.HasSuffix(newVolume.Error, "is not unique") {
			original, err := findVolumeByNameExcept(ctx, engine, req.GetName(), newVolume.Identifier)

			if err != nil || original == nil {
				return nil, status.Errorf(codes.Internal, "failed finding original: %s", err)
			}

			if err := engine.Destroy(ctx, &newVolume); err != nil {
				return nil, status.Errorf(codes.Internal, "failed deleting duplicate: %s", err)
			}

			if original.Size != newVolume.Size {
				return nil, status.Error(codes.AlreadyExists, "volume with same name already exists")
			}

			return original, nil
		}

		return nil, err
	}

	return &newVolume, nil
}

func findVolumeByNameExcept(ctx context.Context, engine types.API, name, except string) (*dv.Volume, error) {
	var channel types.ObjectChannel
	if err := engine.List(ctx, &dv.Volume{Name: name}, api.ObjectChannel(&channel)); err != nil {
		return nil, fmt.Errorf("failed listing volumes: %s", err)
	}

	var listResult dv.Volume

	for retriever := range channel {
		if err := retriever(&listResult); err != nil {
			return nil, fmt.Errorf("failed retrieving volume: %s", err)
		}

		if listResult.Name == name && listResult.Identifier != except {
			if err := engine.Get(ctx, &listResult); err != nil {
				return nil, fmt.Errorf("failed retrieving full volume object: %w", err)
			}

			return &listResult, nil
		}
	}

	return nil, api.ErrNotFound
}

func getDynamicStorageServer(ctx context.Context, engine types.API, req *csi.CreateVolumeRequest) (*dv.StorageServerInterface, error) {
	storageServer := dv.StorageServerInterface{Identifier: req.Parameters["csi.anx.io/storage-server-identifier"]}
	if err := engine.Get(ctx, &storageServer); err != nil {
		return nil, err
	}

	return &storageServer, nil
}

func createMountURL(volume *dv.Volume, storageServer *dv.StorageServerInterface) string {
	return fmt.Sprintf("%s:%s", storageServer.IPAddress.Name, volume.Path)
}
