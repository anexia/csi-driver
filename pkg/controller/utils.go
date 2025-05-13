package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	dynamicvolumev1 "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/hashicorp/go-multierror"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
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
		return ErrVolumeIDNotProvided
	}

	return nil
}

func checkValidateVolumeCapabilitiesRequest(req *csi.ValidateVolumeCapabilitiesRequest) error {
	if req.VolumeId == "" {
		return ErrVolumeIDNotProvided
	}

	if len(req.VolumeCapabilities) == 0 {
		return ErrVolumeCapabilitiesNotProvided
	}

	return nil
}

func sizeFromCapacityRange(capacityRange *csi.CapacityRange) int64 {
	size := defaultVolumeSize

	// required bytes set? -> use value
	if capacityRange.GetRequiredBytes() > 0 {
		size = capacityRange.GetRequiredBytes()
	}

	// limit bytes set and less than size? -> use value
	if capacityRange.GetLimitBytes() > 0 && capacityRange.GetLimitBytes() < size {
		size = capacityRange.GetLimitBytes()
	}

	return size
}

func createAnexiaDynamicVolumeFromRequest(ctx context.Context, engine types.API, req *csi.CreateVolumeRequest) (*dynamicvolumev1.Volume, error) {
	volume := dynamicvolumev1.Volume{
		Name:                    req.GetName(),
		Size:                    sizeFromCapacityRange(req.GetCapacityRange()),
		StorageServerInterfaces: &[]dynamicvolumev1.StorageServerInterface{{Identifier: req.Parameters["csi.anx.io/storage-server-identifier"]}},
		ADSClass:                req.Parameters["csi.anx.io/ads-class"],
	}
	klog.V(4).InfoS("Creating new ADV volume", "volume", volume)

	if err := engine.Create(ctx, &volume); err != nil {
		httpError := api.HTTPError{}
		if errors.As(err, &httpError) && httpError.StatusCode() == http.StatusUnprocessableEntity {
			klog.V(4).InfoS("Volume already exists at engine", "name", req.GetName())
			// if we land here, probably there exists another volume with the same name
			return handleIdempotency(ctx, engine, req)
		}

		return nil, fmt.Errorf("create volume: %w", err)
	}

	klog.V(4).InfoS("ADV volume created, awaiting completion", "engine_identifier", volume.Identifier)
	if err := gs.AwaitCompletion(ctx, engine, &volume); err != nil {
		return nil, fmt.Errorf("failed awaiting completion: %w", err)
	}

	return &volume, nil
}

func handleIdempotency(ctx context.Context, engine types.API, req *csi.CreateVolumeRequest) (*dynamicvolumev1.Volume, error) {
	klog.V(2).InfoS("Searching for existing volume with same name", "name", req.GetName())
	original, err := findVolumeByName(ctx, engine, req.GetName())
	if err != nil {
		// chosen codes.Internal over NotFound
		// because NotFound might be confusing in CreateVolume context
		return nil, status.Errorf(codes.Internal, "failed finding original: %s", err)
	}

	klog.V(4).InfoS("Existing volume found, comparing values", "name", req.GetName(), "engine_identifier", original.Identifier)
	if original.Size != sizeFromCapacityRange(req.GetCapacityRange()) {
		klog.V(4).Info("A volume with the same name, but a different capacity range already exists at the Anexia Engine")
		return nil, status.Error(codes.AlreadyExists, "volume with same name already exists")
	}

	klog.V(4).InfoS("Waiting for volume to transition into completion")
	if err := gs.AwaitCompletion(ctx, engine, original); err != nil {
		klog.V(2).ErrorS(err, "Volume did not transition into completion")
		return nil, fmt.Errorf("failed awaiting completion: %w", err)
	}

	return original, nil
}

func findVolumeByName(ctx context.Context, engine types.API, name string) (*dynamicvolumev1.Volume, error) {
	var channel types.ObjectChannel
	if err := engine.List(ctx, &dynamicvolumev1.Volume{Name: name}, api.ObjectChannel(&channel)); err != nil {
		return nil, fmt.Errorf("failed listing volumes: %s", err)
	}

	var listResult dynamicvolumev1.Volume

	for retriever := range channel {
		if err := retriever(&listResult); err != nil {
			return nil, fmt.Errorf("failed retrieving volume: %s", err)
		}

		if listResult.Name == name {
			if err := engine.Get(ctx, &listResult); err != nil {
				return nil, fmt.Errorf("failed retrieving full volume object: %w", err)
			}

			return &listResult, nil
		}
	}

	return nil, api.ErrNotFound
}

func getDynamicStorageServer(ctx context.Context, engine types.API, req *csi.CreateVolumeRequest) (*dynamicvolumev1.StorageServerInterface, error) {
	storageServer := dynamicvolumev1.StorageServerInterface{Identifier: req.Parameters["csi.anx.io/storage-server-identifier"]}
	if err := engine.Get(ctx, &storageServer); err != nil {
		return nil, err
	}

	if storageServer.IPAddress.Name == "" {
		return nil, ErrQueryingIPAddressesFailed
	}

	return &storageServer, nil
}

func createMountURL(volume *dynamicvolumev1.Volume, storageServer *dynamicvolumev1.StorageServerInterface) string {
	return fmt.Sprintf("%s:%s", storageServer.IPAddress.Name, volume.Path)
}
