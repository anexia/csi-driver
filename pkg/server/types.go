package server

import (
	"context"
	"errors"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

var (
	// ErrNoToken is returned when we have no API token for Anexia Engine.
	ErrNoToken = errors.New("no token configured")

	// ErrInvalidEndpoint is returned when requesting to serve with an unknown protocol.
	ErrInvalidEndpoint = errors.New("invalid endpoint")
)

// Options configures a Server instance to create.
type Options struct {
	NodeID   string
	Endpoint string

	Identity   csi.IdentityServer
	Controller csi.ControllerServer
	Node       csi.NodeServer
}

// Server takes incoming CSI requests and delivers them to the components.
type Server interface {
	Run(ctx context.Context) error
}
