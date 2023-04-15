package driver_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/anexia/csi-driver/pkg/driver"
	"github.com/anexia/csi-driver/pkg/types"
	"github.com/kubernetes-csi/csi-test/v5/pkg/sanity"
)

func TestDriver(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	storageServer := os.Getenv("ANEXIA_STORAGE_SERVER_IDENTIFIER")

	config := sanity.NewTestConfig()
	config.Address = "unix:///tmp/anexia-csi-driver.sock"
	config.TestVolumeParameters = map[string]string{
		"csi.anx.io/ads-class":                 "ENT2",
		"csi.anx.io/storage-server-identifier": storageServer,
	}
	config.TestVolumeSize = 1024 * 1024 * 1024 // 1 GiB

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	nodeID, err := os.Hostname()
	if err != nil {
		t.Error(fmt.Errorf("error retrieving hostname as node ID: %w", err))
	}

	t.Logf("Node ID initialized from hostname: %q", nodeID)

	starting := make(chan struct{}, 0)
	go func() {
		starting <- struct{}{}

		err := driver.Run(ctx, types.Controller|types.Node, nodeID, config.Address)
		if err != nil && !errors.Is(err, context.Canceled) {
			panic(fmt.Errorf("driver crashed: %w", err))
		}
	}()

	<-starting
	time.Sleep(10)

	sanity.Test(t, config)
}
