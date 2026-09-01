package controller

import (
	"context"

	dynamicvolumev1 "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

// ControllerExpandVolume implements the support for the [Volume Expansion API].
//
// ControllerExpandVolume RPC call can be made when volume is ONLINE or OFFLINE
// depending on VolumeExpansion plugin capability. Where ONLINE and OFFLINE means:
//
//   - ONLINE : Volume is currently published or available on a node.
//   - OFFLINE : Volume is currently not published or available on a node.
//
// Because ADV supports online volume expansion, no implementation of NodeExpandVolume is required.
// This is indicated by the NodeExpansionRequired field in the response, which is always set to false.
//
// [Volume Expansion API]: https://kubernetes-csi.github.io/docs/volume-expansion.html
func (cs *controller) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	klog.V(2).InfoS("Expanding volume", "id", req.GetVolumeId(), "request", req)
	if err := checkControllerExpandVolumeRequest(req); err != nil {
		klog.V(2).ErrorS(err, "Volume expansion request validation failed", "request", req)
		return nil, status.Errorf(codes.InvalidArgument, "request check failed: %s", err)
	}

	newCapacityBytes := sizeFromCapacityRange(req.CapacityRange)

	klog.V(2).InfoS("Updating ADV volume to resize to new capacity", "new_capacity_bytes", newCapacityBytes)
	v := dynamicvolumev1.Volume{
		Identifier: req.GetVolumeId(),
		Size:       newCapacityBytes,
	}
	if err := cs.engine.Update(ctx, &v); err != nil {
		klog.V(2).ErrorS(err, "ADV volume could not be updated", "id", req.GetVolumeId())
		return nil, engineErrorToGRPC(err)
	}
	if err := gs.AwaitCompletion(ctx, cs.engine, &v); err != nil {
		klog.V(2).ErrorS(err, "ADV volume expansion did not complete", "id", req.GetVolumeId())
		return nil, engineErrorToGRPC(err)
	}

	klog.V(2).InfoS("Volume expanded successfully", "id", req.GetVolumeCapability())
	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes: newCapacityBytes,

		// There's no adjustment required on the node itself, the mountpoint will continue to work as previously.
		NodeExpansionRequired: false,
	}, nil
}
