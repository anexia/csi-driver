package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// sourceInUse reports whether the volume with the given ID is currently attached to a node,
// i.e. mounted by a pod. Without a Kubernetes client (outside a cluster) it always returns false.
func (cs *controller) sourceInUse(ctx context.Context, volumeID string) (bool, error) {
	if cs.kube == nil {
		return false, nil
	}

	pvs, err := cs.kube.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("list persistent volumes: %w", err)
	}

	pvName := ""
	for _, pv := range pvs.Items {
		if pv.Spec.CSI != nil && pv.Spec.CSI.Driver == "csi.anx.io" && pv.Spec.CSI.VolumeHandle == volumeID {
			pvName = pv.Name
			break
		}
	}
	if pvName == "" {
		return false, nil
	}

	vas, err := cs.kube.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("list volume attachments: %w", err)
	}
	for _, va := range vas.Items {
		if va.Spec.Source.PersistentVolumeName != nil && *va.Spec.Source.PersistentVolumeName == pvName {
			return true, nil
		}
	}

	return false, nil
}
