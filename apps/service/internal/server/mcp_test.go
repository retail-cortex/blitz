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
	"os"
	"path/filepath"
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

// A workspace's own MCP servers: added, listed with the global ones by
// scope, run by that workspace alone; a global one turned off there
// stays on elsewhere; Make global moves one to every workspace.
func TestWorkspaceMCPServersOverTheAPI(t *testing.T) {
	c, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir, other := t.TempDir(), t.TempDir()
	running := func(ws string) map[string]*pb.MCPServer {
		res, err := c.workspaces.ListMCPServers(ctx, connect.NewRequest(&pb.ListMCPServersRequest{Workspace: ws}))
		require.NoError(t, err)
		out := map[string]*pb.MCPServer{}
		for _, m := range res.Msg.Servers {
			out[m.Name] = m
		}
		return out
	}
	listed := func(ws string) map[string]*pb.MCPServerConfig {
		res, err := cfg.ListMCPServerConfigs(ctx, connect.NewRequest(&pb.ListMCPServerConfigsRequest{Workspace: ws}))
		require.NoError(t, err)
		out := map[string]*pb.MCPServerConfig{}
		for _, m := range res.Msg.Servers {
			out[m.Name] = m
		}
		return out
	}
	running(dir) // open both: changes reach open workspaces
	running(other)
	_, err := cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Server: &pb.MCPServerConfig{Name: "docs", Url: "http://127.0.0.1:1/docs"}}))
	require.NoError(t, err)
	_, err = cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Workspace: dir, Server: &pb.MCPServerConfig{Name: "jira", Url: "http://127.0.0.1:1/jira"}}))
	require.NoError(t, err)

	l := listed(dir)
	require.Len(t, l, 2)
	assert.Equal(t, "global", l["docs"].Scope)
	assert.Equal(t, "workspace", l["jira"].Scope)
	assert.Contains(t, running(dir), "jira")
	assert.NotContains(t, running(other), "jira", "a workspace's own stays there")
	assert.NotContains(t, listed(""), "jira")

	_, err = cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Workspace: dir, Name: "docs", Enabled: false}))
	require.NoError(t, err)
	assert.True(t, listed(dir)["docs"].OffHere)
	assert.False(t, listed(dir)["docs"].Disabled, "its own file's setting is unchanged")
	assert.Equal(t, "disabled", running(dir)["docs"].State)
	assert.Equal(t, "idle", running(other)["docs"].State, "on in the other workspace")

	_, err = cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Workspace: dir, Name: "jira", Enabled: false}))
	require.NoError(t, err)
	assert.True(t, listed(dir)["jira"].Disabled, "its own is disabled in its file")

	_, err = cfg.RemoveMCPServer(ctx, connect.NewRequest(&pb.RemoveMCPServerRequest{Workspace: dir, Name: "docs"}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "a workspace never removes a global one")

	_, err = cfg.MakeMCPServerGlobal(ctx, connect.NewRequest(&pb.MakeMCPServerGlobalRequest{Workspace: dir, Name: "jira"}))
	require.NoError(t, err)
	assert.Equal(t, "global", listed(dir)["jira"].Scope)
	assert.Contains(t, running(other), "jira", "every workspace has it now")
	_, err = cfg.MakeMCPServerGlobal(ctx, connect.NewRequest(&pb.MakeMCPServerGlobalRequest{Workspace: dir, Name: "jira"}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "moved already")
	_, err = cfg.MakeMCPServerGlobal(ctx, connect.NewRequest(&pb.MakeMCPServerGlobalRequest{Name: "jira"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "needs a workspace")

	// A workspace server named as a global one stands in for it; it can't
	// be made global while the global one is there.
	_, err = cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Workspace: dir, Server: &pb.MCPServerConfig{Name: "docs", Command: "local-docs"}}))
	require.NoError(t, err)
	assert.Equal(t, "workspace", listed(dir)["docs"].Scope)
	_, err = cfg.MakeMCPServerGlobal(ctx, connect.NewRequest(&pb.MakeMCPServerGlobalRequest{Workspace: dir, Name: "docs"}))
	assert.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))

	// The project's servers are listed by scope, as they are.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".blitz"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".blitz", "settings.toml"), []byte("[[mcp.servers]]\nname = \"issues\"\ncommand = \"node\"\nargs = [\"tools/issues.js\"]\n\n[[mcp.servers]]\nname = \"wiki\"\nurl = \"https://mcp.example/wiki\"\n"), 0o644))
	l = listed(dir)
	assert.Equal(t, "project", l["issues"].Scope)
	assert.Equal(t, "node tools/issues.js", l["issues"].Command)
	assert.Equal(t, "https://mcp.example/wiki", l["wiki"].Url)

	// A scope that isn't an absolute folder is refused by every call.
	bad := "not/absolute"
	for name, call := range map[string]func() error{
		"list":        func() error { _, err := cfg.ListMCPServerConfigs(ctx, connect.NewRequest(&pb.ListMCPServerConfigsRequest{Workspace: bad})); return err },
		"save":        func() error { _, err := cfg.SaveMCPServer(ctx, connect.NewRequest(&pb.SaveMCPServerRequest{Workspace: bad, Server: &pb.MCPServerConfig{Name: "x", Command: "x"}})); return err },
		"remove":      func() error { _, err := cfg.RemoveMCPServer(ctx, connect.NewRequest(&pb.RemoveMCPServerRequest{Workspace: bad, Name: "x"})); return err },
		"enable":      func() error { _, err := cfg.SetMCPServerEnabled(ctx, connect.NewRequest(&pb.SetMCPServerEnabledRequest{Workspace: bad, Name: "x"})); return err },
		"make global": func() error { _, err := cfg.MakeMCPServerGlobal(ctx, connect.NewRequest(&pb.MakeMCPServerGlobalRequest{Workspace: bad, Name: "x"})); return err },
		"check key":   func() error { _, err := cfg.CheckApiKey(ctx, connect.NewRequest(&pb.CheckApiKeyRequest{Workspace: bad, Provider: "gemini", Key: "x"})); return err },
	} {
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(call()), name)
	}
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
