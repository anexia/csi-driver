package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testPV(name, volumeHandle string) *corev1.PersistentVolume {
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{Driver: "csi.anx.io", VolumeHandle: volumeHandle},
			},
		},
	}
}

func testVolumeAttachment(pvName string) *storagev1.VolumeAttachment {
	return &storagev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{Name: "va-" + pvName},
		Spec: storagev1.VolumeAttachmentSpec{
			Attacher: "csi.anx.io",
			NodeName: "node-1",
			Source:   storagev1.VolumeAttachmentSource{PersistentVolumeName: new(pvName)},
		},
	}
}

var _ = Describe("sourceInUse", func() {
	ctx := context.Background()

	It("returns false without a kubernetes client", func() {
		cs := &controller{}
		Expect(cs.sourceInUse(ctx, "vol-1")).To(BeFalse())
	})

	It("returns true if the source PV is attached", func() {
		cs := &controller{kube: fake.NewClientset(testPV("pv-1", "vol-1"), testVolumeAttachment("pv-1"))}
		Expect(cs.sourceInUse(ctx, "vol-1")).To(BeTrue())
	})

	It("returns false if the source PV is not attached", func() {
		cs := &controller{kube: fake.NewClientset(testPV("pv-1", "vol-1"))}
		Expect(cs.sourceInUse(ctx, "vol-1")).To(BeFalse())
	})

	It("returns false if only another PV is attached", func() {
		cs := &controller{kube: fake.NewClientset(
			testPV("pv-1", "vol-1"), testPV("pv-2", "vol-2"), testVolumeAttachment("pv-2"),
		)}
		Expect(cs.sourceInUse(ctx, "vol-1")).To(BeFalse())
	})

	It("returns false if no PV matches the volume id", func() {
		cs := &controller{kube: fake.NewClientset()}
		Expect(cs.sourceInUse(ctx, "vol-1")).To(BeFalse())
	})
})
