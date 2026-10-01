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
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/breaker"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// failingToolset fails while broken is set and counts how often it is asked.
type failingToolset struct {
	inner  tool.Toolset
	broken atomic.Bool
	calls  atomic.Int32
}

func (f *failingToolset) Name() string { return "failing" }
func (f *failingToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	f.calls.Add(1)
	if f.broken.Load() {
		return nil, errors.New("connection refused")
	}
	return f.inner.Tools(ctx)
}

func TestUnhealthyServerIsSkippedUntilCooldown(t *testing.T) {
	fts := &failingToolset{inner: inMemoryMCP(t, "create_issue")}
	fts.broken.Store(true)
	m := NewMCPManagerFromToolsets([]MCPToolset{{Config: config.MCPServerConfig{Name: "gh"}, Toolset: fts}}, nil)
	var warnings []string
	m.Warn = func(s string) { warnings = append(warnings, s) }
	clk := &fakeClock{t: time.Unix(0, 0)}
	m.servers[0].health.SetClock(clk.now)
	ts := m.Toolsets()[0]

	for range 5 { // model calls while the server is down
		tl, err := ts.Tools(createTestToolContext())
		require.NoError(t, err, "down server: %v", tl)
		require.Len(t, tl, 0, "down server: %v %v", tl, err)
	}
	n := fts.calls.Load()
	require.Equal(t, int32(mcpFailThreshold), n, "server contacted %d times while down; want %d then paused", n, mcpFailThreshold)
	require.Len(t, warnings, 2, "warnings = %q", warnings)
	require.Contains(t, warnings[0], "connection refused", "warnings = %q", warnings)
	require.Contains(t, warnings[1], "paused", "warnings = %q", warnings)

	fts.broken.Store(false)
	clk.advance(breaker.InitialCooldown)
	tl, err := ts.Tools(createTestToolContext())
	require.NoError(t, err, "recovered server: %v", tl)
	require.Len(t, tl, 1, "recovered server: %v %v", tl, err)
	last := warnings[len(warnings)-1]
	require.Contains(t, last, "available again", "no recovery notice: %q", warnings)
}

// oneShard is the environment for running this test binary again as a
// helper process: under Bazel's sharding (shard_count) the child would
// otherwise inherit its parent's shard and skip the helper test unless
// that test happens to fall in the same shard.
var oneShard = []string{"TEST_TOTAL_SHARDS=1", "TEST_SHARD_INDEX=0"}

// TestMCPHelperServer is not a test: run with BLITZ_MCP_HELPER=1 it is
// a stdio MCP server for the tests below, so they exercise real processes.
func TestMCPHelperServer(t *testing.T) {
	if os.Getenv("BLITZ_MCP_HELPER") != "1" {
		t.Skip("helper process")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "helper", Version: "1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "echo"}, func(ctx context.Context, req *mcp.CallToolRequest, in echoArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%s from %d", in.Text, os.Getpid())}}}, nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "crash"}, func(ctx context.Context, req *mcp.CallToolRequest, in echoArgs) (*mcp.CallToolResult, any, error) {
		os.Exit(3)
		return nil, nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "slow"}, func(ctx context.Context, req *mcp.CallToolRequest, in echoArgs) (*mcp.CallToolResult, any, error) {
		time.Sleep(10 * time.Second)
		return &mcp.CallToolResult{}, nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "fail"}, func(ctx context.Context, req *mcp.CallToolRequest, in echoArgs) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "bad input"}}}, nil, nil
	})
	_ = server.Run(context.Background(), &mcp.StdioTransport{})
	os.Exit(0)
}

func helperServer(t *testing.T, timeoutSeconds int) (*MCPManager, map[string]runnerTool) {
	t.Helper()
	m, err := NewMCPManager([]config.MCPServerConfig{{
		Name:           "helper",
		Command:        os.Args[0],
		Args:           []string{"-test.run=^TestMCPHelperServer$"},
		Env:            map[string]string{"BLITZ_MCP_HELPER": "1", "TEST_TOTAL_SHARDS": "1", "TEST_SHARD_INDEX": "0"}, // as oneShard
		TimeoutSeconds: timeoutSeconds,
	}}, nil, nil)
	require.NoError(t, err)
	t.Cleanup(m.Close)
	tl, err := m.Toolsets()[0].Tools(createTestToolContext())
	require.NoError(t, err, "helper tools: %v", toolNames(tl))
	require.Len(t, tl, 4, "helper tools: %v %v", toolNames(tl), err)
	tools := map[string]runnerTool{}
	for _, x := range tl {
		tools[x.Name()] = x.(runnerTool)
	}
	return m, tools
}

func echoText(t *testing.T, rt runnerTool) (string, error) {
	t.Helper()
	res, err := rt.Run(createTestToolContext(), map[string]any{"text": "hi"})
	return fmt.Sprint(res), err
}

func TestCrashedStdioServerIsRestarted(t *testing.T) {
	m, tools := helperServer(t, 0)
	first, err := echoText(t, tools["echo"])
	require.NoError(t, err)
	_, crashErr := tools["crash"].Run(createTestToolContext(), map[string]any{"text": "x"})
	require.Error(t, crashErr, "the crash tool reports an error")
	second, err := echoText(t, tools["echo"])
	require.NoError(t, err, "server not restarted after a crash")
	require.NotEqual(t, second, first, "same process answered before and after the crash: %s", first)
	ok, _ := m.servers[0].health.Allow()
	require.True(t, ok, "breaker open after a successful call")
}

func TestSlowMCPCallTimesOutAndToolErrorsKeepServerHealthy(t *testing.T) {
	m, tools := helperServer(t, 1)
	start := time.Now()
	_, err := tools["slow"].Run(createTestToolContext(), map[string]any{"text": "x"})
	require.Error(t, err, "slow call")
	require.Contains(t, err.Error(), "timed out", "slow call: %v", err)
	d := time.Since(start)
	require.LessOrEqual(t, d, 5*time.Second, "timeout took %v", d)
	require.Equal(t, 1, m.servers[0].health.Failures(), "timeout not counted: %d failures", m.servers[0].health.Failures())

	// A tool-level error means the server is working: it resets the streak.
	_, err = tools["fail"].Run(createTestToolContext(), map[string]any{"text": "x"})
	require.Error(t, err, "fail tool")
	require.Contains(t, err.Error(), "bad input", "fail tool: %v", err)
	require.Equal(t, 0, m.servers[0].health.Failures(), "a tool error was counted as a server failure")
}
