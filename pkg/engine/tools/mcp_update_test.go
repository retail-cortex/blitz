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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pingServer is an MCP server over HTTP with one tool; its sessions close
// with the test.
func pingServer(t *testing.T) string {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "ping", Description: "ping"}, func(context.Context, *mcp.CallToolRequest, echoArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{}, nil, nil
	})
	srv := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(func() {
		for ss := range server.Sessions() {
			ss.Close()
		}
		srv.Close()
	})
	return srv.URL
}

// list lists every server's tools, as a turn does before calling the model.
func list(m *MCPManager) {
	for _, ts := range m.ToolsetsFor("", true) {
		_, _ = ts.Tools(createTestToolContext())
	}
}

func states(m *MCPManager) map[string]MCPState {
	out := map[string]MCPState{}
	for _, s := range m.States() {
		out[s.Name] = s
	}
	return out
}

// Update keeps unchanged servers running, restarts changed ones, stops
// removed and disabled ones and adds new ones; States says how each is.
func TestMCPManagerUpdate(t *testing.T) {
	url := pingServer(t)
	m, err := NewMCPManager([]config.MCPServerConfig{{Name: "a", URL: url}, {Name: "dead", URL: "http://127.0.0.1:1/mcp"}}, nil, nil)
	require.NoError(t, err)
	defer m.Close()

	st := states(m)
	assert.Equal(t, "idle", st["a"].State)
	list(m)
	st = states(m)
	assert.Equal(t, MCPState{Name: "a", State: "connected", Tools: 1}, st["a"])
	assert.Equal(t, "failed", st["dead"].State)
	assert.NotEmpty(t, st["dead"].Error)

	a := m.list()[0]
	changed, err := m.Update([]config.MCPServerConfig{{Name: "a", URL: url}, {Name: "c", URL: url}, {Name: "off", URL: url, Disabled: true}})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, []string{"a", "c"}, m.Servers(), "dead removed, off not started")
	assert.Same(t, a, m.list()[0], "a unchanged keeps running")
	st = states(m)
	assert.Equal(t, "connected", st["a"].State)
	assert.Equal(t, "idle", st["c"].State)

	changed, err = m.Update([]config.MCPServerConfig{{Name: "c", URL: url}, {Name: "a", URL: url}})
	require.NoError(t, err)
	assert.False(t, changed, "the same servers")

	changed, err = m.Update([]config.MCPServerConfig{{Name: "a", URL: url, Headers: map[string]string{"X": "1"}}, {Name: "c", URL: url}})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.NotSame(t, a, m.list()[0], "a changed restarts")
	assert.Equal(t, "idle", states(m)["a"].State)

	_, err = m.Update([]config.MCPServerConfig{{Name: "a", URL: url}, {Name: "a", Command: "x"}})
	assert.ErrorContains(t, err, "duplicate")
	assert.Equal(t, []string{"a", "c"}, m.Servers(), "nothing changed")

	var none *MCPManager
	changed, err = none.Update(nil)
	assert.NoError(t, err)
	assert.False(t, changed)
	assert.Nil(t, none.States())
}

// Restart starts one server afresh; an unknown one is refused.
func TestMCPManagerRestart(t *testing.T) {
	url := pingServer(t)
	m, err := NewMCPManager([]config.MCPServerConfig{{Name: "a", URL: url}, {Name: "b", URL: url}}, nil, nil)
	require.NoError(t, err)
	defer m.Close()
	list(m)
	a, b := m.list()[0], m.list()[1]
	require.NoError(t, m.Restart("a"))
	assert.NotSame(t, a, m.list()[0])
	assert.Same(t, b, m.list()[1], "the others keep running")
	assert.Equal(t, "idle", states(m)["a"].State)
	assert.Equal(t, "connected", states(m)["b"].State)
	list(m)
	assert.Equal(t, "connected", states(m)["a"].State, "it connects again")

	assert.ErrorContains(t, m.Restart("nope"), "no MCP server")
	var none *MCPManager
	assert.Error(t, none.Restart("a"))
}
