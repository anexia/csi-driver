package driver

import (
	"context"
	"fmt"

	"github.com/anexia/csi-driver/pkg/controller"
	"github.com/anexia/csi-driver/pkg/identity"
	"github.com/anexia/csi-driver/pkg/node"
	"github.com/anexia/csi-driver/pkg/server"
	"github.com/anexia/csi-driver/pkg/types"
	"github.com/go-logr/logr"
)

// Run initializes the csi-driver instance with the given configuration and
// executes the main loop of the server.
func Run(ctx context.Context, components types.Components, nodeID, endpoint string) error {
	opts := server.Options{
		Endpoint: endpoint,
		NodeID:   nodeID,
	}

	componentLogger := logr.FromContextOrDiscard(ctx).WithName("component")

	var err error

	if opts.Identity, err = identity.New(componentLogger.WithName("identity"), components); err != nil {
		return fmt.Errorf("error initializing identity server: %w", err)
	}

	if components.Has(types.Controller) {
		if opts.Controller, err = controller.New(componentLogger.WithName("controller")); err != nil {
			return fmt.Errorf("error initializing controller server: %w", err)
		}
	}

	if components.Has(types.Node) {
		if opts.Node, err = node.New(componentLogger.WithName("node"), nodeID); err != nil {
			return fmt.Errorf("error initializing node server: %w", err)
		}
	}

	srv, err := server.New(opts)
	if err != nil {
		return fmt.Errorf("error initializing server: %w", err)
	}

	if err := srv.Run(ctx); err != nil {
		return fmt.Errorf("error running server: %w", err)
	}

	return nil
}
