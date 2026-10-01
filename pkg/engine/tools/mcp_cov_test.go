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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

// plainTool is a tool the ADK can't call as a function (no Run).
type plainTool struct{ name string }

func (p plainTool) Name() string        { return p.name }
func (p plainTool) Description() string { return "plain" }
func (p plainTool) IsLongRunning() bool { return false }

// declarationless is a function tool without a declaration.
type declarationless struct{ plainTool }

func (declarationless) Declaration() *genai.FunctionDeclaration { return nil }
func (declarationless) Run(agent.Context, any) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

// staticToolset serves fixed tools.
type staticToolset struct{ tools []tool.Tool }

func (s staticToolset) Name() string                                     { return "static" }
func (s staticToolset) Tools(agent.ReadonlyContext) ([]tool.Tool, error) { return s.tools, nil }

// A stdio server's command is checked when the manager is made, and that
// check leaves nothing behind: no copy of the sign-in's credentials (it
// used to stay until Blitz exited, with the guard pipe). A failure then
// is the manager's error; a failure on a later connect only empties the
// server's tools.
func TestNewMCPManagerStdioCommand(t *testing.T) {
	src := filepath.Join(t.TempDir(), "adc.json")
	require.NoError(t, os.WriteFile(src, []byte(`{}`), 0o600))
	root := useCredentialsDir(t)
	env := &ExecEnv{Credentials: []Credential{{Env: "GOOGLE_APPLICATION_CREDENTIALS", Path: func() string { return src }}}}
	cfg := config.MCPServerConfig{Name: "local", Command: "true"}

	m, err := NewMCPManager([]config.MCPServerConfig{cfg}, env, nil)
	require.NoError(t, err)
	left, _ := os.ReadDir(root)
	assert.Empty(t, left, "checking the command left a credentials copy")

	failing := errors.New("no cache directory")
	credentialsDir = func() (string, error) { return "", failing }
	tl, err := m.Toolsets()[0].Tools(createTestToolContext())
	require.NoError(t, err, "a server that can't start is skipped")
	assert.Empty(t, tl)
	m.Close()

	_, err = NewMCPManager([]config.MCPServerConfig{cfg}, env, nil)
	assert.ErrorIs(t, err, failing)
	assert.ErrorContains(t, err, `mcp server "local"`)
}

// A server with sandbox = false runs with the workspace's scrubbed
// environment and directory only.
func TestNewMCPManagerUnsandboxedServer(t *testing.T) {
	off := false
	dir := t.TempDir()
	env := &ExecEnv{Dir: dir, ScrubEnv: []string{"SECRET_*"}}
	m, err := NewMCPManager([]config.MCPServerConfig{{Name: "local", Command: "true", Sandbox: &off}}, env, nil)
	require.NoError(t, err)
	t.Cleanup(m.Close)
	cmd, err := m.servers[0].transport.build()
	require.NoError(t, err)
	defer cmd.Release()
	if cmd.childEnd != nil {
		defer cmd.childEnd.Close()
	}
	assert.Equal(t, dir, cmd.Dir)
}

// A server's tools allow-list keeps the others from the model, whether
// the manager made the server or was given its toolset.
func TestMCPToolsAllowList(t *testing.T) {
	m := NewMCPManagerFromToolsets([]MCPToolset{{
		Config:  config.MCPServerConfig{Name: "gh", Tools: []string{"create_issue"}},
		Toolset: inMemoryMCP(t, "create_issue", "delete_repo"),
	}}, nil)
	tl, err := m.Toolsets()[0].Tools(createTestToolContext())
	require.NoError(t, err)
	assert.Equal(t, []string{"create_issue"}, toolNames(tl))

	made, err := NewMCPManager([]config.MCPServerConfig{{Name: "remote", URL: "http://127.0.0.1:1/mcp", Tools: []string{"a", "b"}}}, nil, nil)
	require.NoError(t, err)
	t.Cleanup(made.Close)
	assert.Equal(t, map[string]bool{"a": true, "b": true}, made.servers[0].allowed)
}

// Tools that aren't function tools can't be renamed for a prefix and are
// skipped; a function tool without a declaration has none under its new
// name either; the wrapper passes on the inner tool's description.
func TestMCPPrefixedToolShapes(t *testing.T) {
	var warnings []string
	m := NewMCPManagerFromToolsets([]MCPToolset{{
		Config:  config.MCPServerConfig{Name: "odd", Prefix: "o"},
		Toolset: staticToolset{tools: []tool.Tool{plainTool{"plain"}, declarationless{plainTool{"bare"}}}},
	}}, nil)
	m.Warn = func(s string) { warnings = append(warnings, s) }
	tl, err := m.Toolsets()[0].Tools(createTestToolContext())
	require.NoError(t, err)
	require.Equal(t, []string{"o__bare"}, toolNames(tl))
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], `tool "plain" can't be renamed`)

	mt := tl[0].(*managedTool)
	assert.Nil(t, mt.Declaration())
	assert.Equal(t, "plain", mt.Description())
	assert.False(t, mt.IsLongRunning())
}

// A paused server's tool fails at once with when it will be tried again.
func TestManagedToolPausedServer(t *testing.T) {
	m := NewMCPManagerFromToolsets([]MCPToolset{{
		Config:  config.MCPServerConfig{Name: "odd", Prefix: "o"},
		Toolset: staticToolset{tools: []tool.Tool{declarationless{plainTool{"bare"}}}},
	}}, nil)
	tl, err := m.Toolsets()[0].Tools(createTestToolContext())
	require.NoError(t, err)
	srv := m.servers[0]
	for range mcpFailThreshold {
		m.recordFailure(context.Background(), srv, errors.New("down"))
	}
	_, err = tl[0].(runnerTool).Run(createTestToolContext(), map[string]any{})
	assert.ErrorContains(t, err, "unavailable after repeated failures")
}

// A nil manager (no MCP configured) serves and owns nothing.
func TestNilMCPManager(t *testing.T) {
	var m *MCPManager
	assert.Nil(t, m.Servers())
	_, _, ok := m.Lookup("anything")
	assert.False(t, ok)
	assert.NotPanics(t, m.Close)
}

// An auto-approved server's tools run without asking, unless a rule says
// to ask.
func TestApproveMCPAutoApproved(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	reg, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { reg.Close() })
	reg.mcp = newMCPManagerWithToolsets(map[string]tool.Toolset{"gh": inMemoryMCP(t, "create_issue")}, map[string]bool{"gh": true}, nil)
	_, err = reg.mcp.Toolsets()[0].Tools(createTestToolContext())
	require.NoError(t, err)
	h, reqs := approverHooks(false)
	reg.hooks = h
	assert.NoError(t, reg.ApproveMCP(context.Background(), "create_issue", nil))
	assert.Empty(t, *reqs, "an auto-approved server's tool asked")
}

// The derived contexts take their deadline and values from the context
// they carry.
func TestDerivedContexts(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), key{}, "v"), time.Minute)
	defer cancel()
	want, _ := ctx.Deadline()
	for name, c := range map[string]interface {
		Deadline() (time.Time, bool)
		Value(any) any
	}{
		"readonly": readonlyWith{createTestToolContext(), ctx},
		"tool":     toolWith{createTestToolContext(), ctx},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := c.Deadline()
			assert.True(t, ok)
			assert.Equal(t, want, got)
			assert.Equal(t, "v", c.Value(key{}))
		})
	}
}
