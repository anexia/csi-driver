package controller

import (
	"context"

	dynamicvolumev1 "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
	"github.com/anexia/csi-driver/pkg/internal/mockapi"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/mock/gomock"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Controller Service", func() {
	var (
		cs     *controller
		engine *mockapi.MockAPI
	)

	BeforeEach(func() {
		c := gomock.NewController(GinkgoT())
		engine = mockapi.NewMockAPI(c)
		cs = &controller{engine: engine}
	})

	Context("CreateVolume", func() {
		var (
			validRequest                *csi.CreateVolumeRequest
			testStorageServerIdentifier = "mock-storage-server-identifier"
		)

		BeforeEach(func() {
			validRequest = &csi.CreateVolumeRequest{
				Name: "foo",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 12345,
				},
				VolumeCapabilities: []*csi.VolumeCapability{},
				Parameters: map[string]string{
					"csi.anx.io/ads-class":                 "ENT2",
					"csi.anx.io/storage-server-identifier": testStorageServerIdentifier,
				},
			}
		})

		It("can create volumes", func() {
			testVolumeIdentifier := "test-identifier"

			volume := dynamicvolumev1.Volume{
				Name:                    "foo",
				Size:                    12345,
				ADSClass:                "ENT2",
				StorageServerInterfaces: &[]dynamicvolumev1.StorageServerInterface{{Identifier: testStorageServerIdentifier}},
			}

			createdVolume := volume
			createdVolume.Identifier = testVolumeIdentifier
			createdVolume.Path = "/foo/bar/baz"

			// Get StorageServer
			engine.EXPECT().Get(gomock.Any(), &dynamicvolumev1.StorageServerInterface{Identifier: testStorageServerIdentifier}).DoAndReturn(func(_ any, v *dynamicvolumev1.StorageServerInterface, _ ...any) error {
				v.IPAddress = dynamicvolumev1.IPAddress{
					Name: "mock-storage-server.anx.io",
				}
				return nil
			})

			// Create
			engine.EXPECT().
				Create(gomock.Any(), &volume).DoAndReturn(func(_ any, v *dynamicvolumev1.Volume, _ ...any) error {
				v.Identifier = testVolumeIdentifier
				v.Path = "/foo/bar/baz"
				return nil
			})

			// AwaitCompletion
			engine.EXPECT().Get(gomock.Any(), &createdVolume).DoAndReturn(func(_ any, v *dynamicvolumev1.Volume, _ ...any) error {
				v.State.Type = gs.StateTypeOK
				return nil
			})

			res, err := cs.CreateVolume(context.TODO(), validRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())

			Expect(res.Volume.VolumeId).To(Equal(testVolumeIdentifier))
			Expect(res.Volume.VolumeContext["mountURL"]).To(Equal("mock-storage-server.anx.io:/foo/bar/baz"))
		})

		It("returns an InvalidArgument error when request check failed", func() {
			// empty CreateVolumeRequest is not valid
			resp, err := cs.CreateVolume(context.TODO(), &csi.CreateVolumeRequest{})
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(resp).To(BeNil())
		})

		It("returns an error when the configured storage server couldn't be retrieved", func() {
			engine.EXPECT().Get(gomock.Any(), &dynamicvolumev1.StorageServerInterface{Identifier: testStorageServerIdentifier}).Return(api.ErrNotFound)
			resp, err := cs.CreateVolume(context.TODO(), validRequest)
			Expect(err).To(HaveOccurred())
			Expect(resp).To(BeNil())
		})

		It("returns an error when the volume couldn't be created", func() {
			// Get StorageServer
			engine.EXPECT().Get(gomock.Any(), &dynamicvolumev1.StorageServerInterface{Identifier: testStorageServerIdentifier}).DoAndReturn(func(_ any, v *dynamicvolumev1.StorageServerInterface, _ ...any) error {
				v.IPAddress = dynamicvolumev1.IPAddress{
					Name: "mock-storage-server.anx.io",
				}
				return nil
			})

			engine.EXPECT().Create(gomock.Any(), gomock.Any()).Return(api.NewHTTPError(500, "POST", nil, nil))

			resp, err := cs.CreateVolume(context.TODO(), validRequest)

			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(resp).To(BeNil())
		})
	})

	Context("DeleteVolume", func() {
		It("can delete volumes", func() {
			testVolumeIdentifier := "test-identifier"

			engine.EXPECT().Destroy(gomock.Any(), &dynamicvolumev1.Volume{Identifier: testVolumeIdentifier})

			res, err := cs.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{
				VolumeId: testVolumeIdentifier,
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())
		})

		It("returns an InvalidArgument error when request check failed", func() {
			// an empty DeleteVolumeRequest is not valid
			resp, err := cs.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{})
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(resp).To(BeNil())
		})

		It("returns a Internal error when the volume couldn't be deleted", func() {
			testVolumeIdentifier := "test-identifier"

			engine.EXPECT().Destroy(gomock.Any(), &dynamicvolumev1.Volume{Identifier: testVolumeIdentifier}).Return(api.NewHTTPError(500, "DELETE", nil, nil))

			res, err := cs.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{
				VolumeId: testVolumeIdentifier,
			})

			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(res).To(BeNil())
		})
	})

	Context("ValidateVolumeCapabilitiesRequest", func() {
		It("returns an InvalidArgument error when request check failed", func() {
			// an empty ValidateVolumeCapabilitiesRequest is not valid
			resp, err := cs.ValidateVolumeCapabilities(context.TODO(), &csi.ValidateVolumeCapabilitiesRequest{})
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(resp).To(BeNil())
		})

		It("returns an error when the requested volume doesn't exist", func() {
			engine.EXPECT().Get(gomock.Any(), &dynamicvolumev1.Volume{Identifier: "foo"}).Return(api.NewHTTPError(404, "GET", nil, nil))
			resp, err := cs.ValidateVolumeCapabilities(context.TODO(), &csi.ValidateVolumeCapabilitiesRequest{
				VolumeId:           "foo",
				VolumeCapabilities: []*csi.VolumeCapability{{}},
			})
			Expect(status.Code(err)).To(Equal(codes.NotFound))
			Expect(resp).To(BeNil())
		})

		It("returns an error when the requested volume capabilities are not supported", func() {
			engine.EXPECT().Get(gomock.Any(), &dynamicvolumev1.Volume{Identifier: "foo"}).Return(nil)
			resp, err := cs.ValidateVolumeCapabilities(context.TODO(), &csi.ValidateVolumeCapabilitiesRequest{
				VolumeId:           "foo",
				VolumeCapabilities: []*csi.VolumeCapability{{AccessType: &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}}}},
			})
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(resp).To(BeNil())
		})

		It("confirms requested capabilities if supported", func() {
			engine.EXPECT().Get(gomock.Any(), &dynamicvolumev1.Volume{Identifier: "foo"}).Return(nil)
			capabilities := []*csi.VolumeCapability{{AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}}}}
			resp, err := cs.ValidateVolumeCapabilities(context.TODO(), &csi.ValidateVolumeCapabilitiesRequest{
				VolumeId:           "foo",
				VolumeCapabilities: capabilities,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Confirmed.VolumeCapabilities).To(Equal(capabilities))
		})
	})

	Context("ControllerPublishVolume", func() {
		It("returns an empty response without any errors", func() {
			resp, err := cs.ControllerPublishVolume(context.TODO(), &csi.ControllerPublishVolumeRequest{})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(Equal(&csi.ControllerPublishVolumeResponse{}))
		})
	})

	Context("ControllerUnpublishVolume", func() {
		It("returns an empty response without any errors", func() {
			resp, err := cs.ControllerUnpublishVolume(context.TODO(), &csi.ControllerUnpublishVolumeRequest{})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(Equal(&csi.ControllerUnpublishVolumeResponse{}))
		})
	})
})
