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

package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
)

type echoArgs struct {
	Text string `json:"text"`
}

// inMemoryMCP starts an MCP server with the given tool names and returns an
// ADK toolset connected to it.
func inMemoryMCP(t *testing.T, names ...string) tool.Toolset {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	for _, n := range names {
		mcp.AddTool(server, &mcp.Tool{Name: n, Description: "echo " + n}, func(ctx context.Context, req *mcp.CallToolRequest, in echoArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: in.Text}}}, nil, nil
		})
	}
	clientT, serverT := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ss, err := server.Connect(ctx, serverT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { ss.Close() }) // ends the client session over the pipe too
	ts, err := mcptoolset.New(mcptoolset.Config{Transport: clientT})
	require.NoError(t, err)
	return ts
}

func toolNames(ts []tool.Tool) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name())
	}
	return out
}

func TestMCPManagerRecordsAndFilters(t *testing.T) {
	m := newMCPManagerWithToolsets(map[string]tool.Toolset{
		"alpha": inMemoryMCP(t, "search", "read_file"), // read_file shadows a built-in
		"beta":  inMemoryMCP(t, "search", "deploy"),    // duplicate "search"
	}, map[string]bool{"beta": true}, []string{"read_file"})
	var warnings []string
	m.Warn = func(s string) { warnings = append(warnings, s) }

	var all []string
	for _, ts := range m.Toolsets() {
		tl, err := ts.Tools(createTestToolContext())
		require.NoError(t, err)
		all = append(all, toolNames(tl)...)
	}
	assert.Equal(t, "search,deploy", strings.Join(all, ","), "tools = %v", all)
	assert.Len(t, warnings, 2, "expected shadow + duplicate warnings, got %v", warnings)
	srv, auto, ok := m.Lookup("search")
	assert.True(t, ok, "lookup search = %s %v %v", srv, auto, ok)
	assert.Equal(t, "alpha", srv, "lookup search = %s %v %v", srv, auto, ok)
	assert.False(t, auto, "lookup search = %s %v %v", srv, auto, ok)
	srv, auto, ok = m.Lookup("deploy")
	assert.True(t, ok, "lookup deploy = %s %v %v", srv, auto, ok)
	assert.Equal(t, "beta", srv, "lookup deploy = %s %v %v", srv, auto, ok)
	assert.True(t, auto, "lookup deploy = %s %v %v", srv, auto, ok)
	_, _, ok = m.Lookup("read_file")
	assert.False(t, ok, "built-in name should not be owned by MCP")
}

func TestRegistryApprovesMCPTools(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	reg, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer reg.Close()
	reg.mcp = newMCPManagerWithToolsets(map[string]tool.Toolset{"gh": inMemoryMCP(t, "create_issue")}, nil, nil)
	for _, ts := range reg.mcp.Toolsets() {
		ts.Tools(createTestToolContext())
	}

	h, reqs := approverHooks(false)
	reg.hooks = h
	err = reg.ApproveMCP(context.Background(), "create_issue", map[string]any{"title": "bug"})
	require.Error(t, err, "expected denied approval, got %v (%d prompts)", err, len(*reqs))
	require.Len(t, *reqs, 1, "expected denied approval, got %v (%d prompts)", err, len(*reqs))
	req := (*reqs)[0]
	assert.Equal(t, api.ActionMCP, req.Kind, "approval request %+v", req)
	assert.Equal(t, "mcp:gh:create_issue", req.Key, "approval request %+v", req)
	assert.Contains(t, req.Detail, "title=bug", "approval request %+v", req)
	// Non-MCP tools pass straight through.
	assert.NoError(t, reg.ApproveMCP(context.Background(), "grep", nil), "built-in tool gated as MCP")
}

func TestNewMCPManagerValidation(t *testing.T) {
	for name, cfgs := range map[string][]config.MCPServerConfig{
		"no name":   {{Command: "x"}},
		"both":      {{Name: "a", Command: "x", URL: "http://x"}},
		"neither":   {{Name: "a"}},
		"duplicate": {{Name: "a", Command: "x"}, {Name: "a", URL: "http://y"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewMCPManager(cfgs, nil, nil)
			assert.Error(t, err, "%s: expected error", name)
		})
	}
	m, err := NewMCPManager([]config.MCPServerConfig{{Name: "local", Command: "true"}, {Name: "remote", URL: "https://example.com/mcp"}}, nil, nil)
	assert.NoError(t, err, "valid config: %v %v", err, m.Servers())
	assert.Equal(t, "local,remote", strings.Join(m.Servers(), ","), "valid config: %v %v", err, m.Servers())
	m.Close()
	// An unreachable server yields no tools and a warning, not an error.
	var warned bool
	m.Warn = func(string) { warned = true }
	for _, ts := range m.Toolsets() {
		tl, err := ts.Tools(createTestToolContext())
		assert.NoError(t, err, "broken server: %v", tl)
		assert.Len(t, tl, 0, "broken server: %v %v", tl, err)
	}
	assert.True(t, warned, "expected a warning for unreachable servers")
}

func TestMCPPrefixedTools(t *testing.T) {
	m := NewMCPManagerFromToolsets([]MCPToolset{{
		Config:  config.MCPServerConfig{Name: "github", Prefix: "gh"},
		Toolset: inMemoryMCP(t, "create_issue", "grep"), // "grep" would shadow a built-in without the prefix
	}}, []string{"grep"})
	tl, err := m.Toolsets()[0].Tools(createTestToolContext())
	require.NoError(t, err)
	got := strings.Join(toolNames(tl), ",")
	require.Equal(t, "gh__create_issue,gh__grep", got, "prefixed names = %s", got)
	pt := tl[0].(*managedTool)
	d := pt.Declaration()
	assert.NotNil(t, d, "declaration name %+v", d)
	assert.Equal(t, "gh__create_issue", d.Name, "declaration name %+v", d)
	// The call reaches the server under the original name.
	res, err := pt.Run(createTestToolContext(), map[string]any{"text": "hello from gh"})
	assert.NoError(t, err, "run through prefix: %v", res)
	assert.Contains(t, fmt.Sprint(res), "hello from gh", "run through prefix: %v %v", res, err)
	// Registered in the request under the prefixed name.
	req := &model.LLMRequest{}
	require.NoError(t, pt.ProcessRequest(createTestToolContext(), req))
	_, ok := req.Tools["gh__create_issue"]
	assert.True(t, ok, "request tools %v", req.Tools)
	srv, _, ok := m.Lookup("gh__grep")
	assert.True(t, ok, "lookup prefixed: %s %v", srv, ok)
	assert.Equal(t, "github", srv, "lookup prefixed: %s %v", srv, ok)
	_, _, ok = m.Lookup("create_issue")
	assert.False(t, ok, "the unprefixed name must not be routed")
}

func TestMCPToolsetsForAgents(t *testing.T) {
	m := NewMCPManagerFromToolsets([]MCPToolset{
		{Config: config.MCPServerConfig{Name: "primary-only"}, Toolset: inMemoryMCP(t, "a")},
		{Config: config.MCPServerConfig{Name: "kitten", Agents: []string{"qa"}}, Toolset: inMemoryMCP(t, "b")},
		{Config: config.MCPServerConfig{Name: "everyone", Agents: []string{"*"}}, Toolset: inMemoryMCP(t, "c")},
	}, nil)
	names := func(ts []tool.Toolset) string {
		var out []string
		for _, x := range ts {
			out = append(out, x.Name())
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		agent   string
		primary bool
		want    string
	}{
		{"blitz", true, "mcp:primary-only,mcp:everyone"},
		{"qa", false, "mcp:kitten,mcp:everyone"},
		{"qa", true, "mcp:primary-only,mcp:kitten,mcp:everyone"},
		{"helios", false, "mcp:everyone"},
	}
	for _, c := range cases {
		got := names(m.ToolsetsFor(c.agent, c.primary))
		assert.Equal(t, c.want, got, "ToolsetsFor(%s, %v) = %s, want %s", c.agent, c.primary, got, c.want)
	}
	var nilManager *MCPManager
	assert.Nil(t, nilManager.ToolsetsFor("x", true), "nil manager should offer nothing")
}

// Closing the manager ends an HTTP server's session: the ADK toolset has
// no Close, so without closingTransport its goroutines would outlive the
// workspace (goleak, in TestMain, would catch them).
func TestMCPManagerCloseEndsHTTPSessions(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "ping", Description: "ping"}, func(ctx context.Context, req *mcp.CallToolRequest, in echoArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	m, err := NewMCPManager([]config.MCPServerConfig{{Name: "remote", URL: srv.URL}}, nil, nil)
	require.NoError(t, err)
	tl, err := m.Toolsets()[0].Tools(createTestToolContext())
	require.NoError(t, err, "tools = %v,", tl)
	require.Len(t, tl, 1, "tools = %v, %v", tl, err)
	m.Close()
	tl, _ = m.Toolsets()[0].Tools(createTestToolContext())
	assert.Len(t, tl, 0, "a closed manager reconnected")
	for ss := range server.Sessions() { // the test server's side of it
		ss.Close()
	}
}
