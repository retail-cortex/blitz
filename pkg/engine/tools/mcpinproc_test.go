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
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/config"
)

// An in-process server is served through its registered transport; its
// read-only tools run unasked, the rest ask; an unregistered one is an
// error.
func TestMCPInProcess(t *testing.T) {
	RegisterInProcess("test-inproc", func(c config.MCPServerConfig) mcp.Transport {
		server := mcp.NewServer(&mcp.Implementation{Name: "inproc", Version: "1"}, nil)
		for _, name := range []string{"count", "wipe"} {
			mcp.AddTool(server, &mcp.Tool{Name: name}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
			})
		}
		client, srv := mcp.NewInMemoryTransports()
		go func() { _ = server.Run(context.Background(), srv) }()
		return client
	})

	m, err := NewMCPManager([]config.MCPServerConfig{{Name: "dbs", InProcess: "test-inproc", Prefix: "db", ReadOnlyTools: []string{"count"}}}, nil, nil)
	require.NoError(t, err)
	t.Cleanup(m.Close)
	list(m)
	assert.Equal(t, MCPState{Name: "dbs", State: "connected", Tools: 2}, states(m)["dbs"])
	for _, tc := range []struct {
		tool string
		auto bool
	}{
		{"db__count", true},
		{"db__wipe", false},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			server, auto, ok := m.Lookup(tc.tool)
			require.True(t, ok)
			assert.Equal(t, "dbs", server)
			assert.Equal(t, tc.auto, auto)
		})
	}

	_, err = NewMCPManager([]config.MCPServerConfig{{Name: "x", InProcess: "nothing-here"}}, nil, nil)
	assert.ErrorContains(t, err, `no in-process server "nothing-here"`)
}
