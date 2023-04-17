#!/usr/bin/env bash

function cleanup {
    if [ -n "$csi_driver_pid" ]; then
      kill $csi_driver_pid
    fi
}

trap cleanup EXIT

./csi-driver --components combined --endpoint 'unix:///tmp/anexia-csi-driver.sock' --nodeid $(hostname) &> csi-driver.log &
csi_driver_pid=$!

sed -i "s/<storage-server-identifier>/$ANEXIA_STORAGE_SERVER_IDENTIFIER/g" tests/sanity/volume-parameters.yaml

go run github.com/kubernetes-csi/csi-test/v5/cmd/csi-sanity@latest \
  --csi.endpoint='unix:///tmp/anexia-csi-driver.sock' \
  --csi.testvolumeparameters='./tests/sanity/volume-parameters.yaml' \
  --csi.testvolumesize=1073741824
