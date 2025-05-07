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
}
