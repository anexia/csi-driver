# csi-driver for Anexia

[![Build Status](https://github.com/anexia/csi-driver/actions/workflows/push.yml/badge.svg?branch=main&event=push)](https://github.com/anexia/csi-driver/actions/workflows/push.yml)
[![Code Climate](https://codeclimate.com/github/anexia/csi-driver.png)](https://codeclimate.com/github/anexia/csi-driver)
[![Test Coverage](https://api.codeclimate.com/v1/badges/9f866bbcd866440b1f64/test_coverage)](https://codeclimate.com/github/anexia/csi-driver/test_coverage)

This is a csi-driver for Anexia!

## Prerequirements

1. Anexia Engine account with `Dynamic Volume` (ADV) module enabled
2. ADV Storage Server Interface created in a IPAM prefix with role `SCND unrouted unique`
3. K8s nodes with network access to the Storage Server Interface
4. Anexia Engine service-account token with:
  - full ADV permissions
  - IP Space - View All
5. `rpc-statd` service running on hosts (included in `nfs-common` package)
    * alternatively add the 'nolock' option to `mountOptions` on `StorageClass` or `PersistentVolume.spec` if global file locking is not needed

## Installation

> [!NOTE]
> If you create a K8s cluster using Anexias Kubernetes Engine (AKE) with a `SCND unrouted unique` IPAM prefix configured and have an ADV enabled Engine account,
> the CSI driver will automatically be deployed and configured with a default `StorageClass` using ENT2 storage called `anexia-ent2`.

To install the CSI driver and configure a StorageClass in a K8s cluster not managed by Anexia, you can apply the following steps to get it running:

> [!IMPORTANT]
> Make sure to set both `$ANEXIA_TOKEN` and `$STORAGE_SERVER_INTERFACE_ID` with the data from the prerequirements.
> We recommend to execute the following steps in an empty directory as it creates a new file (`kustomization.yaml`) in the current working directory.

```bash
# Create secret from Anexia Engine service-account token
$ kubectl create secret generic csi-driver-anexia --from-literal=token=$ANEXIA_TOKEN -n kube-system

# Create a deployment kustomization using the id of the storage-server-interface
# Note: you can also apply deploy/kubernetes/{rbac,driver}.yaml individualy if you don't want to use kustomize
$ cat <<EOT > ./kustomization.yaml
resources:
  - github.com/anexia/csi-driver/deploy/kubernetes

patches:
  - patch: |-
      - op: replace
        path: /parameters/csi.anx.io~1storage-server-identifier
        value: "$STORAGE_SERVER_INTERFACE_ID"
    target:
      kind: StorageClass
      name: anexia-ent2
EOT

# Apply the deployment kustomization
$ kubectl apply -k .
```

## Configuration

### StorageClass

> [!IMPORTANT]
> Make sure to set the `$STORAGE_SERVER_INTERFACE_ID` environment variable with the ADV Storage Server Interface identifier set as value.

```bash
kubectl apply -f - <<EOF
kind: StorageClass
apiVersion: storage.k8s.io/v1
metadata:
  name: anexia-ent2
provisioner: csi.anx.io
parameters:
  csi.anx.io/ads-class: ENT2
  csi.anx.io/storage-server-identifier: $STORAGE_SERVER_INTERFACE_ID
EOF
```

### Enable fsGroup support (optional)

Example configuration:

```bash
kubectl apply -f - <<EOF
apiVersion: storage.k8s.io/v1
kind: CSIDriver
metadata:
  name: csi.anx.io
spec:
  fsGroupPolicy: File
EOF
```

Consult the [Kubernetes CSI Developer Documentation](https://kubernetes-csi.github.io/docs/support-fsgroup.html) for further information.
