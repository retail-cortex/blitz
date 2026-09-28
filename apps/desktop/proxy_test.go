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

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// The page's API calls, streamed turns and approvals included, reach the
// service through the proxy.
func TestProxyReachesTheService(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	dir, err := os.MkdirTemp("/tmp", "cpd") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")

	create := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "create_file", Args: map[string]any{"path": "a.txt", "content": "x"}}}}}
	s := servicetest.New(func(ctx context.Context, d string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = d
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{Model: runtime.NewMockLLM("m", create, genai.NewContentFromText("done", genai.RoleModel))})
	})
	l, err := socket.Listen(path)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	go servicetest.Serve(ctx, l, s.Handler(), time.Second)
	t.Cleanup(func() { cancel(); s.Close() })

	page := httptest.NewServer(serviceProxy(path))
	defer page.Close()
	sessions := pb.NewSessionServiceClient(http.DefaultClient, page.URL)
	ws := t.TempDir()
	sess, err := sessions.NewSession(context.Background(), connect.NewRequest(&pb.NewSessionRequest{Workspace: ws}))
	require.NoError(t, err, "through the proxy")
	stream, err := sessions.RunTurn(context.Background(), connect.NewRequest(&pb.RunTurnRequest{Workspace: ws, SessionId: sess.Msg.Session.Id, Turn: &pb.Turn{Text: "go"}}))
	require.NoError(t, err)
	var output string
	for stream.Receive() {
		ev := stream.Msg().Event
		if ar := ev.GetApprovalRequest(); ar != nil {
			_, err := sessions.Approve(context.Background(), connect.NewRequest(&pb.ApproveRequest{Workspace: ws, RequestId: ar.RequestId, Decision: pb.Decision_DECISION_ONCE}))
			assert.NoError(t, err, "approve")
		}
		if f := ev.GetFinished(); f != nil {
			output = f.Output
		}
	}
	err = stream.Err()
	require.NoError(t, err, "streamed turn: %q", output)
	require.Equal(t, "done", output, "streamed turn: %q %v", output, err)
	_, err = os.Stat(filepath.Join(ws, "a.txt"))
	assert.NoError(t, err, "approved write")

	// Only the API is forwarded.
	res, err := http.Get(page.URL + "/index.html")
	assert.NoError(t, err, "non-API path: %v", res)
	assert.Equal(t, http.StatusNotFound, res.StatusCode, "non-API path: %v %v", res, err)
}

// With no service, the page gets an error its client can read.
func TestProxyWithoutAService(t *testing.T) {
	page := httptest.NewServer(serviceProxy(filepath.Join(t.TempDir(), "none.sock")))
	defer page.Close()
	c := pb.NewWorkspaceServiceClient(http.DefaultClient, page.URL)
	_, err := c.GetModel(context.Background(), connect.NewRequest(&pb.GetModelRequest{Workspace: "/x"}))
	assert.Equal(t, connect.CodeUnavailable, connect.CodeOf(err), "err %v (code %v)", err, connect.CodeOf(err))
}
