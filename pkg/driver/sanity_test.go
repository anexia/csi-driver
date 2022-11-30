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
	config := sanity.NewTestConfig()
	config.Address = "unix:///tmp/anexia-csi-driver.sock"

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
