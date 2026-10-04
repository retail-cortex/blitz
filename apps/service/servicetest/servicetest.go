// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

// RunOverrides are a private service's run settings (blitzd's hidden
// flags, which the CLI sets for a run of its own).
type RunOverrides = daemon.RunOverrides

// RunWith runs a private service, as a client starts for a run of its own,
// on socket, with the configuration file config ("" for the usual search)
// and the run's settings, until ctx is done.
func RunWith(ctx context.Context, socket, config string, run RunOverrides) error {
	return daemon.Run(ctx, daemon.Options{Socket: socket, Config: config, Version: "test", Run: run, Private: true})
}
