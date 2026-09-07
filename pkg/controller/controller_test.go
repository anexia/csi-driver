package controller

import (
	"context"
	"errors"
	"time"

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

type fakeSnapshotDataManager struct {
	create  func(context.Context, string, *dynamicvolumev1.Volume, *dynamicvolumev1.Volume, bool) (time.Time, error)
	restore func(context.Context, string, *dynamicvolumev1.Volume, *dynamicvolumev1.Volume, bool) error
}

func (m *fakeSnapshotDataManager) Create(
	ctx context.Context,
	snapshotName string,
	source *dynamicvolumev1.Volume,
	snapshot *dynamicvolumev1.Volume,
	newlyCreated bool,
) (time.Time, error) {
	return m.create(ctx, snapshotName, source, snapshot, newlyCreated)
}

func (m *fakeSnapshotDataManager) Restore(
	ctx context.Context,
	snapshotID string,
	snapshot *dynamicvolumev1.Volume,
	destination *dynamicvolumev1.Volume,
	newlyCreated bool,
) error {
	return m.restore(ctx, snapshotID, snapshot, destination, newlyCreated)
}

var _ = Describe("Controller Service", func() {
	var (
		cs     *controller
		engine *mockapi.MockAPI
	)

	BeforeEach(func() {
		c := gomock.NewController(GinkgoT())
		engine = mockapi.NewMockAPI(c)
		cs = &controller{
			engine:                    engine,
			volumeDeleteRetryInterval: time.Nanosecond,
		}
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
				VolumeCapabilities: []*csi.VolumeCapability{{AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}}}},
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

		It("returns an OutOfRange error when the capacity range cannot be satisfied", func() {
			validRequest.CapacityRange = &csi.CapacityRange{RequiredBytes: maxVolumeSize + 1}

			resp, err := cs.CreateVolume(context.TODO(), validRequest)
			Expect(status.Code(err)).To(Equal(codes.OutOfRange))
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

		It("restores a newly created volume from a snapshot", func() {
			snapshotID, err := encodeSnapshotHandle(snapshotHandle{
				BackingVolumeID: "snapshot-volume",
				SourceVolumeID:  "source-volume",
			})
			Expect(err).ToNot(HaveOccurred())
			validRequest.VolumeContentSource = &csi.VolumeContentSource{
				Type: &csi.VolumeContentSource_Snapshot{
					Snapshot: &csi.VolumeContentSource_SnapshotSource{SnapshotId: snapshotID},
				},
			}

			snapshot := &dynamicvolumev1.Volume{Identifier: "snapshot-volume"}
			engine.EXPECT().Get(gomock.Any(), snapshot).DoAndReturn(func(_ any, volume *dynamicvolumev1.Volume, _ ...any) error {
				volume.Size = 12000
				return nil
			})
			engine.EXPECT().Get(gomock.Any(), &dynamicvolumev1.StorageServerInterface{Identifier: testStorageServerIdentifier}).DoAndReturn(func(_ any, storageServer *dynamicvolumev1.StorageServerInterface, _ ...any) error {
				storageServer.IPAddress.Name = "mock-storage-server.anx.io"
				return nil
			})
			engine.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, volume *dynamicvolumev1.Volume, _ ...any) error {
				volume.Identifier = "restored-volume"
				volume.Path = "/restored"
				return nil
			})
			engine.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, volume *dynamicvolumev1.Volume, _ ...any) error {
				volume.State.Type = gs.StateTypeOK
				return nil
			})

			cs.snapshotData = &fakeSnapshotDataManager{
				restore: func(_ context.Context, actualSnapshotID string, actualSnapshot, destination *dynamicvolumev1.Volume, newlyCreated bool) error {
					Expect(actualSnapshotID).To(Equal(snapshotID))
					Expect(actualSnapshot.Identifier).To(Equal("snapshot-volume"))
					Expect(destination.Identifier).To(Equal("restored-volume"))
					Expect(newlyCreated).To(BeTrue())
					return nil
				},
			}

			response, err := cs.CreateVolume(context.Background(), validRequest)

			Expect(err).ToNot(HaveOccurred())
			Expect(response.Volume.VolumeId).To(Equal("restored-volume"))
			Expect(response.Volume.ContentSource).To(Equal(validRequest.VolumeContentSource))
		})

		It("rejects a restore when the destination is smaller than the snapshot", func() {
			snapshotID, err := encodeSnapshotHandle(snapshotHandle{
				BackingVolumeID: "snapshot-volume",
				SourceVolumeID:  "source-volume",
			})
			Expect(err).ToNot(HaveOccurred())
			validRequest.VolumeContentSource = &csi.VolumeContentSource{
				Type: &csi.VolumeContentSource_Snapshot{
					Snapshot: &csi.VolumeContentSource_SnapshotSource{SnapshotId: snapshotID},
				},
			}

			engine.EXPECT().Get(gomock.Any(), &dynamicvolumev1.Volume{Identifier: "snapshot-volume"}).DoAndReturn(func(_ any, volume *dynamicvolumev1.Volume, _ ...any) error {
				volume.Size = 12346
				return nil
			})

			response, err := cs.CreateVolume(context.Background(), validRequest)

			Expect(status.Code(err)).To(Equal(codes.OutOfRange))
			Expect(response).To(BeNil())
		})
	})

	Context("Snapshots", func() {
		It("creates a directory-copy snapshot in a backing ADV volume", func() {
			createdAt := time.Date(2026, time.September, 7, 10, 0, 0, 0, time.UTC)
			source := &dynamicvolumev1.Volume{Identifier: "source-volume"}
			engine.EXPECT().Get(gomock.Any(), source).DoAndReturn(func(_ any, volume *dynamicvolumev1.Volume, _ ...any) error {
				volume.Size = 12345
				volume.ADSClass = "ENT2"
				volume.StorageServerInterfaces = &[]dynamicvolumev1.StorageServerInterface{{Identifier: "storage-server"}}
				return nil
			})
			engine.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, volume *dynamicvolumev1.Volume, _ ...any) error {
				Expect(volume.Name).To(Equal(snapshotBackingVolumeName("snapshot-name")))
				Expect(volume.Size).To(Equal(int64(12345)))
				Expect(volume.ADSClass).To(Equal("ENT2"))
				volume.Identifier = "snapshot-volume"
				return nil
			})
			engine.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, volume *dynamicvolumev1.Volume, _ ...any) error {
				volume.State.Type = gs.StateTypeOK
				return nil
			})
			cs.snapshotData = &fakeSnapshotDataManager{
				create: func(_ context.Context, snapshotName string, actualSource, snapshot *dynamicvolumev1.Volume, newlyCreated bool) (time.Time, error) {
					Expect(snapshotName).To(Equal("snapshot-name"))
					Expect(actualSource.Identifier).To(Equal("source-volume"))
					Expect(snapshot.Identifier).To(Equal("snapshot-volume"))
					Expect(newlyCreated).To(BeTrue())
					return createdAt, nil
				},
			}

			response, err := cs.CreateSnapshot(context.Background(), &csi.CreateSnapshotRequest{
				Name:           "snapshot-name",
				SourceVolumeId: "source-volume",
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(response.Snapshot.ReadyToUse).To(BeTrue())
			Expect(response.Snapshot.SizeBytes).To(Equal(int64(12345)))
			Expect(response.Snapshot.SourceVolumeId).To(Equal("source-volume"))
			Expect(response.Snapshot.CreationTime.AsTime()).To(Equal(createdAt))
			handle, err := decodeSnapshotHandle(response.Snapshot.SnapshotId)
			Expect(err).ToNot(HaveOccurred())
			Expect(handle).To(Equal(snapshotHandle{BackingVolumeID: "snapshot-volume", SourceVolumeID: "source-volume"}))
		})

		It("removes a newly created backing volume when copying fails", func() {
			engine.EXPECT().Get(gomock.Any(), &dynamicvolumev1.Volume{Identifier: "source-volume"}).DoAndReturn(func(_ any, volume *dynamicvolumev1.Volume, _ ...any) error {
				volume.Size = 12345
				volume.ADSClass = "ENT2"
				volume.StorageServerInterfaces = &[]dynamicvolumev1.StorageServerInterface{{Identifier: "storage-server"}}
				return nil
			})
			engine.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, volume *dynamicvolumev1.Volume, _ ...any) error {
				volume.Identifier = "snapshot-volume"
				return nil
			})
			engine.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, volume *dynamicvolumev1.Volume, _ ...any) error {
				volume.State.Type = gs.StateTypeOK
				return nil
			})
			engine.EXPECT().Destroy(gomock.Any(), gomock.Any()).DoAndReturn(func(_ any, volume *dynamicvolumev1.Volume, _ ...any) error {
				Expect(volume.Identifier).To(Equal("snapshot-volume"))
				return nil
			})
			cs.snapshotData = &fakeSnapshotDataManager{
				create: func(context.Context, string, *dynamicvolumev1.Volume, *dynamicvolumev1.Volume, bool) (time.Time, error) {
					return time.Time{}, errors.New("copy failed")
				},
			}

			response, err := cs.CreateSnapshot(context.Background(), &csi.CreateSnapshotRequest{
				Name:           "snapshot-name",
				SourceVolumeId: "source-volume",
			})

			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(response).To(BeNil())
		})

		It("deletes the backing volume from an opaque snapshot handle", func() {
			snapshotID, err := encodeSnapshotHandle(snapshotHandle{
				BackingVolumeID: "snapshot-volume",
				SourceVolumeID:  "source-volume",
			})
			Expect(err).ToNot(HaveOccurred())
			engine.EXPECT().Destroy(gomock.Any(), &dynamicvolumev1.Volume{Identifier: "snapshot-volume"})

			response, err := cs.DeleteSnapshot(context.Background(), &csi.DeleteSnapshotRequest{SnapshotId: snapshotID})

			Expect(err).ToNot(HaveOccurred())
			Expect(response).To(Equal(&csi.DeleteSnapshotResponse{}))
		})

		It("validates required snapshot fields", func() {
			response, err := cs.CreateSnapshot(context.Background(), &csi.CreateSnapshotRequest{})
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(response).To(BeNil())

			response, err = cs.CreateSnapshot(context.Background(), &csi.CreateSnapshotRequest{Name: "snapshot-name"})
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(response).To(BeNil())

			deleteResponse, err := cs.DeleteSnapshot(context.Background(), &csi.DeleteSnapshotRequest{})
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(deleteResponse).To(BeNil())
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

		It("retries deletion while another Engine operation is finishing", func() {
			testVolumeIdentifier := "test-identifier"
			volume := &dynamicvolumev1.Volume{Identifier: testVolumeIdentifier}

			gomock.InOrder(
				engine.EXPECT().Destroy(gomock.Any(), volume).Return(api.NewHTTPError(422, "DELETE", nil, nil)),
				engine.EXPECT().Destroy(gomock.Any(), volume).Return(nil),
			)

			res, err := cs.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{
				VolumeId: testVolumeIdentifier,
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(&csi.DeleteVolumeResponse{}))
		})

		It("stops retrying temporarily blocked deletions after the configured attempts", func() {
			testVolumeIdentifier := "test-identifier"
			cs.volumeDeleteMaxAttempts = 2

			engine.EXPECT().
				Destroy(gomock.Any(), &dynamicvolumev1.Volume{Identifier: testVolumeIdentifier}).
				Return(api.NewHTTPError(422, "DELETE", nil, nil)).
				Times(2)

			res, err := cs.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{
				VolumeId: testVolumeIdentifier,
			})

			Expect(err).To(HaveOccurred())
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
