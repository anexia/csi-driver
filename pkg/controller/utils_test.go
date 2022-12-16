package controller

import (
	"context"
	"reflect"

	dv "github.com/anexia/csi-driver/pkg/internal/apis/dynamicvolume/v1"
	"github.com/anexia/csi-driver/pkg/internal/mockapi"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.anx.io/go-anxcloud/pkg/api"
	"go.anx.io/go-anxcloud/pkg/api/types"
	"go.anx.io/go-anxcloud/pkg/apis/common/gs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ = Describe("Controller Service Utils", func() {
	var (
		a *mockapi.MockAPI
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		a = mockapi.NewMockAPI(ctrl)
	})

	Context("engineErrorToGRPC", func() {
		DescribeTable("convert engine errors to grpc errors", func(engineError error, grpcCode codes.Code) {
			err := engineErrorToGRPC(engineError)
			Expect(status.Code(err)).To(Equal(grpcCode))
		},
			Entry("404 Not Found", api.NewHTTPError(404, "", nil, nil), codes.NotFound),
			Entry("500 Internal Server Error", api.NewHTTPError(500, "", nil, nil), codes.Internal),
			Entry("unspecified error", api.NewHTTPError(0, "", nil, nil), codes.Unknown),
			Entry("err with code already set", status.Errorf(codes.Internal, "foo"), codes.Internal),
		)
	})

	Context("checkCreateVolumeRequest", func() {
		var req *csi.CreateVolumeRequest

		BeforeEach(func() {
			req = &csi.CreateVolumeRequest{
				Name:               "mocked-volume-name",
				CapacityRange:      &csi.CapacityRange{RequiredBytes: 12345},
				VolumeCapabilities: []*csi.VolumeCapability{},
			}
		})

		It("returns no error if request contains all necessary data", func() {
			err := checkCreateVolumeRequest(req)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns an error when no volume name was provided", func() {
			req.Name = ""
			err := checkCreateVolumeRequest(req)
			Expect(err).To(HaveOccurred())
		})

		It("returns an error when no volume capacity range was provided", func() {
			req.CapacityRange = nil
			err := checkCreateVolumeRequest(req)
			Expect(err).To(HaveOccurred())
		})

		It("returns an error when no volume capabilities have been provided", func() {
			req.VolumeCapabilities = nil
			err := checkCreateVolumeRequest(req)
			Expect(err).To(HaveOccurred())
		})

		It("returns an error when unsupported volume capabilities have been provided", func() {
			req.VolumeCapabilities = []*csi.VolumeCapability{
				{AccessType: &csi.VolumeCapability_Block{Block: &csi.VolumeCapability_BlockVolume{}}},
			}
			err := checkCreateVolumeRequest(req)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("checkValidateVolumeCapabilitiesRequest", func() {
		var req *csi.ValidateVolumeCapabilitiesRequest

		BeforeEach(func() {
			req = &csi.ValidateVolumeCapabilitiesRequest{
				VolumeId:           "test-identifier",
				VolumeCapabilities: []*csi.VolumeCapability{{}},
			}
		})

		It("returns an error when no volume id was provided", func() {
			req.VolumeId = ""
			err := checkValidateVolumeCapabilitiesRequest(req)
			Expect(err).To(MatchError(ErrVolumeIdNotProvided))
		})

		It("returns an error when no volume capabilities have been provided", func() {
			req.VolumeCapabilities = nil
			err := checkValidateVolumeCapabilitiesRequest(req)
			Expect(err).To(MatchError(ErrVolumeCapabilitiesNotProvided))
		})
	})

	Context("createAnexiaDynamicVolumeFromRequest", func() {
		var (
			expectedVolumeCreate      dv.Volume
			expectedVolumeAfterCreate dv.Volume

			req *csi.CreateVolumeRequest
		)

		BeforeEach(func() {
			expectedVolumeCreate = dv.Volume{
				Name:                    "mocked-volume-name",
				Size:                    12345,
				StorageServerInterfaces: &[]dv.StorageServerInterface{{Identifier: "mocked-storage-server-identifier"}},
				ADSClass:                "ENT6",
			}
			expectedVolumeAfterCreate = expectedVolumeCreate
			expectedVolumeAfterCreate.Identifier = "mocked-volume-identifier"

			req = &csi.CreateVolumeRequest{
				Name: "mocked-volume-name",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 12345,
				},
				Parameters: map[string]string{
					"csi.anx.io/ads-class":                 "ENT6",
					"csi.anx.io/storage-server-identifier": "mocked-storage-server-identifier",
				},
			}
		})

		It("can successfully create a dynamic volume from a valid request", func() {
			a.EXPECT().Create(gomock.Any(), &expectedVolumeCreate).DoAndReturn(func(_ any, v *dv.Volume, _ ...any) error {
				v.Identifier = "mocked-volume-identifier"
				return nil
			})

			// AwaitCompletion
			a.EXPECT().Get(gomock.Any(), &expectedVolumeAfterCreate).DoAndReturn(func(_ any, v *dv.Volume, _ ...any) error {
				v.HasState.State.Type = gs.StateTypeOK
				return nil
			})

			volume, err := createAnexiaDynamicVolumeFromRequest(context.TODO(), a, req)

			Expect(err).ToNot(HaveOccurred())
			Expect(volume.Identifier).To(Equal("mocked-volume-identifier"))
		})

		It("returns an error when api.Create wasn't successful", func() {
			a.EXPECT().Create(gomock.Any(), &expectedVolumeCreate).Return(api.ErrNotFound)

			volume, err := createAnexiaDynamicVolumeFromRequest(context.TODO(), a, req)

			Expect(err).To(MatchError(api.ErrNotFound))
			Expect(volume).To(BeNil())
		})

		It("returns an error when gs.AwaitCompletion (internally api.Get) wasn't successful", func() {
			a.EXPECT().Create(gomock.Any(), &expectedVolumeCreate).DoAndReturn(func(_ any, v *dv.Volume, _ ...any) error {
				v.Identifier = "mocked-volume-identifier"
				return nil
			})

			// AwaitCompletion
			a.EXPECT().Get(gomock.Any(), &expectedVolumeAfterCreate).Return(api.ErrNotFound)

			_, err := createAnexiaDynamicVolumeFromRequest(context.TODO(), a, req)

			Expect(err).To(MatchError(api.ErrNotFound))
		})

		Context("idempotency", func() {
			BeforeEach(func() {
				// Create succeeds
				a.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, v *dv.Volume, _ ...any) error {
					v.Identifier = "duplicate"
					return nil
				})
				// AwaitCompletion fails because volume name is not unique
				a.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, v *dv.Volume, _ ...any) error {
					v.HasState.State.Type = gs.StateTypeError
					v.Error = "foo bar is not unique"
					return nil
				})
				// List returns the a volume
				a.EXPECT().List(gomock.Any(), &dv.Volume{Name: "mocked-volume-name"}, gomock.Any()).DoAndReturn(func(_ any, v *dv.Volume, opts ...types.ListOption) error {
					options := types.ListOptions{}
					for _, opt := range opts {
						opt.ApplyToList(&options)
					}

					Expect(options.ObjectChannel).ToNot(BeNil())

					c := make(chan types.ObjectRetriever, 1)
					*options.ObjectChannel = c
					c <- func(o types.Object) error {
						reflect.ValueOf(o).Elem().Set(reflect.ValueOf(dv.Volume{Identifier: "original", Name: "mocked-volume-name"}))
						return nil
					}

					return nil
				})

				// retrieve full object
				a.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, v *dv.Volume, _ ...any) error {
					v.Size = 12345
					return nil
				})

				// delete newly created duplicate volume
				a.EXPECT().Destroy(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, v *dv.Volume, _ ...any) error {
					Expect(v.Identifier).To(Equal("duplicate"))
					return nil
				})
			})

			It("returns an error when a volume with the same name but different size already exists", func() {
				req.CapacityRange = &csi.CapacityRange{RequiredBytes: 54321}
				v, err := createAnexiaDynamicVolumeFromRequest(context.TODO(), a, req)
				Expect(status.Code(err)).To(Equal(codes.AlreadyExists))
				Expect(v).To(BeNil())
			})

			It("returns the original if a volume with the same name and size already exists", func() {
				v, err := createAnexiaDynamicVolumeFromRequest(context.TODO(), a, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(v).ToNot(BeNil())
				Expect(v.Identifier).To(Equal("original"))
			})
		})
	})

	Context("getDynamicStorageServer", func() {
		It("can successfully resolve a server with valid `csi.anx.io/storage-server-identifier` set", func() {
			a.EXPECT().Get(gomock.Any(), &dv.StorageServerInterface{Identifier: "foobar"}).DoAndReturn(func(_ any, s *dv.StorageServerInterface, _ ...any) error {
				s.Name = "test-name"
				return nil
			})

			storageServer, err := getDynamicStorageServer(context.TODO(), a, &csi.CreateVolumeRequest{
				Parameters: map[string]string{
					"csi.anx.io/storage-server-identifier": "foobar",
				},
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(storageServer.Name).To(Equal("test-name"))
		})

		It("returns an error when api.Get wasn't successful", func() {
			a.EXPECT().Get(gomock.Any(), &dv.StorageServerInterface{Identifier: "does-not-exist"}).DoAndReturn(func(_ any, s *dv.StorageServerInterface, _ ...any) error {
				return api.ErrNotFound
			})

			storageServer, err := getDynamicStorageServer(context.TODO(), a, &csi.CreateVolumeRequest{
				Parameters: map[string]string{
					"csi.anx.io/storage-server-identifier": "does-not-exist",
				},
			})

			Expect(err).To(MatchError(api.ErrNotFound))
			Expect(storageServer).To(BeNil())
		})
	})

	Context("createMountURL", func() {
		It("correctly creates a mount URL from volume + storage server", func() {
			volume := dv.Volume{Path: "/foo/bar"}
			storageServer := dv.StorageServerInterface{IPAddress: dv.IPAddress{Name: "1.2.3.4"}}

			mountURL := createMountURL(&volume, &storageServer)

			Expect(mountURL).To(Equal("1.2.3.4:/foo/bar"))
		})
	})
})
