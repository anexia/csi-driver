package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/anexia/csi-driver/pkg/version"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

type server struct {
	nodeID string

	listener net.Listener
	server   *grpc.Server
}

// New creates a new Server instance, checking some parts of the configuration
// and registering the components.
func New(opts Options) (Server, error) {
	klog.V(4).InfoS("Starting new server with options", "options", opts)
	protocol, endpoint, err := parseEndpoint(opts.Endpoint)
	if err != nil {
		return nil, err
	}

	if protocol == "unix" {
		if err := os.Remove(endpoint); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("error deleting existing object at socket path: %w", err)
		}
	}

	listener, err := net.Listen(protocol, endpoint)
	if err != nil {
		return nil, fmt.Errorf("error listening on endpoint: %w", err)
	}

	grpcServer := grpc.NewServer()

	if opts.Identity != nil {
		csi.RegisterIdentityServer(grpcServer, opts.Identity)
	}

	if opts.Controller != nil {
		csi.RegisterControllerServer(grpcServer, opts.Controller)
	}

	if opts.Node != nil {
		csi.RegisterNodeServer(grpcServer, opts.Node)
	}

	return &server{
		nodeID:   opts.NodeID,
		listener: listener,
		server:   grpcServer,
	}, nil
}

// Run serves requests until the server fails or the given context is
// cancelled. On cancellation the server is stopped gracefully and nil is
// returned.
func (s *server) Run(ctx context.Context) error {
	klog.V(2).InfoS(
		"Starting server",
		"version", version.Version,
		"node-id", s.nodeID,
	)

	ec := make(chan error, 1)
	go func() {
		ec <- s.server.Serve(s.listener)
	}()

	select {
	case err := <-ec:
		return err
	case <-ctx.Done():
		klog.V(2).InfoS("Shutting down server")
		s.server.GracefulStop()
		if err := <-ec; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return err
		}
		return nil
	}
}
