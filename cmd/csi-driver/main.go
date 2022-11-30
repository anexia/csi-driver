package main

import (
	"context"
	"flag"
	"os"

	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"

	"github.com/anexia/csi-driver/pkg/driver"
	"github.com/anexia/csi-driver/pkg/types"
)

func main() {
	components := types.Controller | types.Node
	flag.Var(&components, "components", "Components to enable, one of 'controller', 'node' or 'combined'")

	var (
		endpoint = flag.String("endpoint", "unix:///tmp/csi.sock", "CSI endpoint. unix:// is interpreted as relative path, tcp://hostname:port")
		nodeID   = flag.String("nodeid", "", "node ID")
	)

	klog.InitFlags(nil)
	flag.Parse()

	ctx := logr.NewContext(context.Background(), klogr.New())

	err := driver.Run(ctx, components, *nodeID, *endpoint)
	if err != nil {
		klog.Error(err)
		os.Exit(-1)
	}
}
