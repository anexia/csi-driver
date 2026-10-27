package kubernetes_test

import (
	"errors"
	"io"
	"os"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestProvisionerCanProtectRestoreSource(t *testing.T) {
	t.Parallel()
	file, err := os.Open("rbac.yaml")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	const provisionerRole = "csi-driver-anexia-external-provisioner"
	var grantsUpdate, bindsController bool
	decoder := yaml.NewDecoder(file)
	for {
		var manifest struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
			Rules []struct {
				APIGroups []string `yaml:"apiGroups"`
				Resources []string `yaml:"resources"`
				Verbs     []string `yaml:"verbs"`
			} `yaml:"rules"`
			RoleRef struct {
				Kind string `yaml:"kind"`
				Name string `yaml:"name"`
			} `yaml:"roleRef"`
			Subjects []struct {
				Kind      string `yaml:"kind"`
				Name      string `yaml:"name"`
				Namespace string `yaml:"namespace"`
			} `yaml:"subjects"`
		}
		if decodeErr := decoder.Decode(&manifest); errors.Is(decodeErr, io.EOF) {
			break
		} else if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if manifest.Kind == "ClusterRole" && manifest.Metadata.Name == provisionerRole {
			for _, rule := range manifest.Rules {
				if slices.Contains(rule.APIGroups, "snapshot.storage.k8s.io") &&
					slices.Contains(rule.Resources, "volumesnapshots") && slices.Contains(rule.Verbs, "update") {
					grantsUpdate = true
				}
			}
		}
		if manifest.Kind == "ClusterRoleBinding" && manifest.RoleRef.Kind == "ClusterRole" && manifest.RoleRef.Name == provisionerRole {
			for _, subject := range manifest.Subjects {
				if subject.Kind == "ServiceAccount" && subject.Name == "csi-driver-anexia-controller" && subject.Namespace == "kube-system" {
					bindsController = true
				}
			}
		}
	}
	if !grantsUpdate {
		t.Error("provisioner role must grant update on volumesnapshots for the restore-source protection finalizer")
	}
	if !bindsController {
		t.Error("provisioner role must be bound to the controller service account")
	}
}
