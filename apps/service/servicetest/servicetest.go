// Package servicetest runs the Blitz service in a test, for packages
// outside the service that test against it (the client, the CLI, the
// desktop app). Only tests may use it.
package servicetest

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/retail-cortex/blitz/apps/service/internal/daemon"
	"github.com/retail-cortex/blitz/apps/service/internal/server"
	"github.com/retail-cortex/blitz/pkg/engine"
)

// Server is the service's API over workspaces the test opens.
type Server = server.Server

// New returns a service whose workspaces open, and whose Handler serves
// the API, with open (typically a workspace on a mock model).
func New(open func(ctx context.Context, dir string) (*engine.Workspace, error)) *Server {
	return server.New(open)
}

// Serve serves h on l until ctx is done, as the service does.
func Serve(ctx context.Context, l net.Listener, h http.Handler, grace time.Duration) error {
	return server.Serve(ctx, l, h, grace)
}

// Run runs the whole service, as blitzd does, on socket with the default
// configuration, until ctx is done.
func Run(ctx context.Context, socket string) error {
	return daemon.Run(ctx, daemon.Options{Socket: socket, Version: "test"})
}
