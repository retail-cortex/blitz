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

package runtime

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/tool/mcptoolset"
)

func inMemoryServer(t *testing.T, name string) *mcptoolset.Config {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: name}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
	})
	ct, st := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ss, err := server.Connect(ctx, st, nil)
	require.NoError(t, err)
	t.Cleanup(func() { ss.Close() }) // ends the client session over the pipe too
	return &mcptoolset.Config{Transport: ct}
}

func TestMCPToolsReachChosenSubAgentOnly(t *testing.T) {
	f := newEngineWith(t, fixtureOpts{}, textContent("root reply"), textContent("kitten reply"))
	ts, err := mcptoolset.New(*inMemoryServer(t, "run_e2e_tests"))
	require.NoError(t, err)
	f.tools.SetMCP(tools.NewMCPManagerFromToolsets([]tools.MCPToolset{{
		Config: config.MCPServerConfig{Name: "qa", Agents: []string{"qa"}, Prefix: "qa"}, Toolset: ts,
	}}, nil))
	require.NoError(t, f.eng.Rebuild(context.Background()))

	_, err = collect(t, f.eng, "s", "hello")
	require.NoError(t, err)
	_, ok := f.llm.Requests[0].Tools["qa__run_e2e_tests"]
	assert.False(t, ok, "primary agent was offered a server scoped to qa")
	_, err = f.eng.InvokeSubagent(context.Background(), "qa", "run the tests")
	require.NoError(t, err)
	_, ok = f.llm.Requests[1].Tools["qa__run_e2e_tests"]
	assert.True(t, ok, "qa not offered its MCP tool; tools: %v", keys(f.llm.Requests[1].Tools))
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
