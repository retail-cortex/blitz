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

package tray

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Stop asks the service to stop before it signals it.
func TestStopAsksFirst(t *testing.T) {
	f, a := newFake(t)
	f.running = true
	asked := 0
	a.Shutdown = func() error { asked++; f.running = false; return nil }
	require.NoError(t, a.Stop())
	assert.Equal(t, 1, asked)

	// A service that can't be asked is signalled (here: no process, so it
	// stays up).
	f.running = true
	a.Shutdown = func() error { return errors.New("unimplemented") }
	assert.ErrorContains(t, a.Stop(), "still running")
}

// Shutdown reaches the service's Shutdown over its socket.
func TestShutdown(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "blitz.sock")
	ln, err := socket.Listen(sock)
	require.NoError(t, err)
	asked := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.Handle(pb.NewWorkspaceServiceHandler(shutdownService{asked: asked}))
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })
	require.NoError(t, Shutdown(context.Background(), sock))
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("not asked")
	}
	assert.Error(t, Shutdown(context.Background(), filepath.Join(shortTemp(t), "none.sock")))
}

type shutdownService struct {
	pb.UnimplementedWorkspaceServiceHandler
	asked chan struct{}
}

func (s shutdownService) Shutdown(context.Context, *connect.Request[pb.ShutdownRequest]) (*connect.Response[pb.ShutdownResponse], error) {
	s.asked <- struct{}{}
	return connect.NewResponse(&pb.ShutdownResponse{}), nil
}
