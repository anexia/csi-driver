// Command csi-driver is the entry point of the Anexia CSI driver.
package main

import (
	"context"
	"flag"
	"os/signal"
	"syscall"

	"k8s.io/klog/v2"

	"github.com/anexia/csi-driver/pkg/driver"
	"github.com/anexia/csi-driver/pkg/types"
)

func main() {
	components := types.Controller | types.Node
	flag.Var(&components, "components", "Components to enable, one of 'controller', 'node' or 'combined'")

	var (
		endpoint         = flag.String("endpoint", "unix:///tmp/csi.sock", "CSI endpoint. unix:// is interpreted as relative path, tcp://hostname:port")
		nodeID           = flag.String("nodeid", "", "node ID")
		sourceInUseCheck = flag.Bool("source-in-use-check", false, "Reject cloning of volumes that are attached to a node. Requires in-cluster access to list PersistentVolumes and VolumeAttachments")
	)

	klog.InitFlags(nil)                               // Setup klog using the default flagset.
	flag.Parse()                                      // Parse remaining flags (aka ours)
	defer klog.FlushAndExit(klog.ExitFlushTimeout, 0) // Flush the logs on exit.

	// Pass the default, now initialized klog logger, via the context and
	// cancel it on SIGINT/SIGTERM to shut the server down gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx = klog.NewContext(ctx, klog.Background())

	err := driver.Run(ctx, components, *nodeID, *endpoint, *sourceInUseCheck)
	if err != nil {
		klog.Error(err)
		klog.FlushAndExit(klog.ExitFlushTimeout, 1)
	}
}
