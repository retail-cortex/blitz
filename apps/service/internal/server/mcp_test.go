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

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MCP servers are added, edited, turned off and on and removed in the
// global settings, and an open workspace follows at once; a running one
// restarts.
func TestMCPServersOverTheAPI(t *testing.T) {
	c, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()
	running := func() map[string]*pb.MCPServer {
		res, err := c.workspaces.ListMCPServers(ctx, connect.NewRequest(&pb.ListMCPServersRequest{Workspace: dir}))
		require.NoError(t, err)
		out := map[string]*pb.MCPServer{}
		for _, m := range res.Msg.Servers {
			out[m.Name] = m
		}
		return out
	}
	require.Empty(t, running())
	code := func(err error) connect.Code { return connect.CodeOf(err) }

	off := false
	_, err := cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Server: &pb.MCPServerConfig{
		Name: "docs", Url: "http://127.0.0.1:1/mcp", Headers: map[string]string{"X-Team": "core"}, AutoApprove: true, Sandbox: &off, TimeoutSeconds: 30,
	}}))
	require.NoError(t, err)
	list, err := cfg.ListMCPServerConfigs(ctx, connect.NewRequest(&pb.ListMCPServerConfigsRequest{}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Servers, 1)
	got := list.Msg.Servers[0]
	assert.Equal(t, "docs", got.Name)
	assert.Equal(t, map[string]string{"X-Team": "core"}, got.Headers)
	assert.True(t, got.AutoApprove)
	require.NotNil(t, got.Sandbox)
	assert.False(t, *got.Sandbox)
	assert.EqualValues(t, 30, got.TimeoutSeconds)
	assert.NotEmpty(t, list.Msg.Path)
	assert.Equal(t, "idle", running()["docs"].State, "the open workspace has it at once")

	_, err = cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Server: &pb.MCPServerConfig{Name: "docs", Command: "x"}}))
	assert.Equal(t, connect.CodeInvalidArgument, code(err), "a name taken")
	_, err = cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Name: "nope", Server: &pb.MCPServerConfig{Name: "nope", Command: "x"}}))
	assert.Equal(t, connect.CodeNotFound, code(err))

	_, err = cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Name: "docs", Enabled: false}))
	require.NoError(t, err)
	assert.Equal(t, "disabled", running()["docs"].State)
	_, err = c.workspaces.RestartMCPServer(ctx, connect.NewRequest(&pb.RestartMCPServerRequest{Workspace: dir, Name: "docs"}))
	assert.Equal(t, connect.CodeNotFound, code(err), "a disabled server isn't running")
	_, err = cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Name: "docs", Enabled: true}))
	require.NoError(t, err)
	assert.Equal(t, "idle", running()["docs"].State)
	_, err = c.workspaces.RestartMCPServer(ctx, connect.NewRequest(&pb.RestartMCPServerRequest{Workspace: dir, Name: "docs"}))
	require.NoError(t, err)

	_, err = cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Name: "docs", Server: &pb.MCPServerConfig{Name: "wiki", Url: "http://127.0.0.1:1/wiki"}}))
	require.NoError(t, err)
	r := running()
	assert.Contains(t, r, "wiki")
	assert.NotContains(t, r, "docs", "renamed")
	assert.Equal(t, "http://127.0.0.1:1/wiki", r["wiki"].Target)

	_, err = cfg.RemoveMCPServer(ctx, connect.NewRequest(&pb.RemoveMCPServerRequest{Name: "wiki"}))
	require.NoError(t, err)
	assert.Empty(t, running())
	_, err = cfg.RemoveMCPServer(ctx, connect.NewRequest(&pb.RemoveMCPServerRequest{Name: "wiki"}))
	assert.Equal(t, connect.CodeNotFound, code(err))
	_, err = cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Name: "wiki"}))
	assert.Equal(t, connect.CodeNotFound, code(err))
}

// DraftManifest refuses a kind it has no brief for, and answers a problem
// (not an error) when the model can't be had.
func TestDraftManifestOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil)
	ctx := context.Background()
	dir := t.TempDir()
	_, err := c.workspaces.DraftManifest(ctx, connect.NewRequest(&pb.DraftManifestRequest{Workspace: dir, Kind: "poem", Request: "x"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	res, err := c.workspaces.DraftManifest(ctx, connect.NewRequest(&pb.DraftManifestRequest{Workspace: dir, Kind: "agent", Description: "a reviewer"}))
	require.NoError(t, err)
	assert.True(t, res.Msg.Text != "" || res.Msg.Problem != "", "a draft or why not")
}
