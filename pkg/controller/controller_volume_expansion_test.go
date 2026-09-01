package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	dynamicvolumev1 "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
	"github.com/anexia/csi-driver/pkg/internal/mockapi"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/mock/gomock"
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestControllerExpandVolume(t *testing.T) {
	t.Parallel()

	type testBundle struct {
		controller *controller
		api        *mockapi.MockAPI
	}
	setup := func(t *testing.T) testBundle {
		t.Helper()

		ctrl := gomock.NewController(t)
		api := mockapi.NewMockAPI(ctrl)

		return testBundle{
			controller: &controller{engine: api},
			api:        api,
		}
	}

	t.Run("engine errors are properly returned", func(t *testing.T) {
		t.Parallel()
		var (
			bundle = setup(t)
			ctx    = context.TODO()
		)

		bundle.api.EXPECT().
			Update(gomock.Any(), gomock.Eq(&dynamicvolumev1.Volume{
				Identifier: "expand-volume",
				Size:       oneGibibyteInBytes,
			})).
			Return(errors.New("mock error"))

		_, err := bundle.controller.ControllerExpandVolume(ctx, &csi.ControllerExpandVolumeRequest{
			VolumeId:      "expand-volume",
			CapacityRange: &csi.CapacityRange{RequiredBytes: oneGibibyteInBytes},
		})
		if err == nil {
			t.Fatalf("Expected error, got none")
		}
		if !strings.Contains(err.Error(), "mock error") {
			t.Fatalf("Expected substring 'mock error' inside error message, got: %s", err)
		}
	})
	t.Run("missing volume ID is rejected", func(t *testing.T) {
		t.Parallel()
		bundle := setup(t)

		resp, err := bundle.controller.ControllerExpandVolume(context.TODO(), &csi.ControllerExpandVolumeRequest{
			CapacityRange: &csi.CapacityRange{RequiredBytes: oneGibibyteInBytes},
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Expected InvalidArgument, got %s: %v", status.Code(err), err)
		}
		if resp != nil {
			t.Fatalf("Expected no response, got %#v", resp)
		}
	})
	t.Run("missing capacity range is rejected", func(t *testing.T) {
		t.Parallel()
		bundle := setup(t)

		resp, err := bundle.controller.ControllerExpandVolume(context.TODO(), &csi.ControllerExpandVolumeRequest{
			VolumeId: "expand-volume",
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Expected InvalidArgument, got %s: %v", status.Code(err), err)
		}
		if resp != nil {
			t.Fatalf("Expected no response, got %#v", resp)
		}
	})
	t.Run("engine is called with proper parameters", func(t *testing.T) {
		t.Parallel()
		var (
			bundle = setup(t)
			ctx    = context.TODO()
		)

		bundle.api.EXPECT().
			Update(gomock.Any(), gomock.Eq(&dynamicvolumev1.Volume{
				Identifier: "expand-volume",
				Size:       oneGibibyteInBytes,
			})).
			Return(nil)
		bundle.api.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, volume *dynamicvolumev1.Volume, _ ...any) error {
				volume.State.Type = gs.StateTypeOK
				return nil
			})

		resp, err := bundle.controller.ControllerExpandVolume(ctx, &csi.ControllerExpandVolumeRequest{
			VolumeId:      "expand-volume",
			CapacityRange: &csi.CapacityRange{RequiredBytes: oneGibibyteInBytes},
		})
		if err != nil {
			t.Fatalf("Expected no error, got %#v", err)
		}
		if resp.CapacityBytes != oneGibibyteInBytes {
			t.Fatalf("Returned capacity in bytes does not match expected value, got %d, want %d", resp.CapacityBytes, oneGibibyteInBytes)
		}
	})
	t.Run("completion errors are properly returned", func(t *testing.T) {
		t.Parallel()
		var (
			bundle = setup(t)
			ctx    = context.TODO()
		)

		bundle.api.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
		bundle.api.EXPECT().Get(gomock.Any(), gomock.Any()).Return(errors.New("completion error"))

		resp, err := bundle.controller.ControllerExpandVolume(ctx, &csi.ControllerExpandVolumeRequest{
			VolumeId:      "expand-volume",
			CapacityRange: &csi.CapacityRange{RequiredBytes: oneGibibyteInBytes},
		})
		if err == nil || !strings.Contains(err.Error(), "completion error") {
			t.Fatalf("Expected completion error, got: %v", err)
		}
		if resp != nil {
			t.Fatalf("Expected no response, got %#v", resp)
		}
	})
}
