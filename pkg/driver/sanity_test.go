package driver_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/anexia/csi-driver/pkg/driver"
	"github.com/anexia/csi-driver/pkg/types"
	"github.com/kubernetes-csi/csi-test/v5/pkg/sanity"
)

func TestDriver(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	config := sanity.NewTestConfig()
	config.Address = "unix:///tmp/anexia-csi-driver.sock"
	config.TestVolumeParameters = map[string]string{
		"csi.anx.io/ads-class": "ENT2",
		// todo: read from env
		"csi.anx.io/storage-server-identifier": "2014322f13e54dfb82c491b961df12c7", // csi-test
	}

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	nodeID, _ := os.Hostname()

	go func() {
		err := driver.Run(ctx, types.Controller|types.Node, nodeID, config.Address)
		if err != nil && !errors.Is(err, context.Canceled) {
			panic(fmt.Errorf("driver crashed: %w", err))
		}
	}()

	sanity.Test(t, config)
}
