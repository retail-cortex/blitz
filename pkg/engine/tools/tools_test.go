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
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
)

type runnerTool interface {
	Run(ctx agent.Context, args any) (map[string]any, error)
}

type mockIC struct {
	agent.InvocationContext
	ctx context.Context
}

func (m *mockIC) Deadline() (deadline time.Time, ok bool) { return m.ctx.Deadline() }
func (m *mockIC) Done() <-chan struct{}                   { return m.ctx.Done() }
func (m *mockIC) Err() error                              { return m.ctx.Err() }
func (m *mockIC) Value(key any) any                       { return m.ctx.Value(key) }
func (m *mockIC) Artifacts() agent.Artifacts              { return nil }

func createTestToolContext() agent.Context {
	return createTestToolContextWith(context.Background())
}

func createTestToolContextWith(ctx context.Context) agent.Context {
	return agent.NewToolContext(&mockIC{ctx: ctx}, "test_call", &session.EventActions{}, nil)
}

// --- shared helpers ---

func newTestWorkspace(t *testing.T) (*Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	ws, err := NewWorkspace(dir, 0)
	require.NoError(t, err, "NewWorkspace")
	t.Cleanup(func() { ws.Close() })
	return ws, dir
}

// toolOf adapts a (tool.Tool, error) constructor result: toolOf(t)(NewX(...)).
func toolOf(t *testing.T) func(tool.Tool, error) runnerTool {
	return func(tl tool.Tool, err error) runnerTool {
		t.Helper()
		require.NoError(t, err, "failed to create tool")
		rt, ok := tl.(runnerTool)
		require.True(t, ok, "%T does not implement runnerTool", tl)
		return rt
	}
}

func runTool(t *testing.T, rt runnerTool, args map[string]any) map[string]any {
	t.Helper()
	out, err := rt.Run(createTestToolContext(), args)
	require.NoError(t, err, "tool run returned error")
	return out
}

func errOf(out map[string]any) string {
	s, _ := out["error"].(string)
	return s
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// allowAll auto-approves every action.
func allowAll() *Hooks { return NewHooks(Policy{Mode: api.ModeBypass}) }

// approverHooks records requests and approves once (true) or denies (false).
func approverHooks(approve bool) (*Hooks, *[]api.ApprovalRequest) {
	d := api.DecisionDeny
	if approve {
		d = api.DecisionOnce
	}
	return decisionHooks(d)
}

// decisionHooks records requests and answers with d.
func decisionHooks(d api.Decision) (*Hooks, *[]api.ApprovalRequest) {
	var reqs []api.ApprovalRequest
	h := NewHooks(Policy{})
	h.SetApprover(func(ctx context.Context, req api.ApprovalRequest) (api.Decision, error) {
		reqs = append(reqs, req)
		return d, nil
	})
	return h, &reqs
}

// --- registry ---

func TestToolsSuite(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = tmpDir
	cfg.Tools.UCToolsDir = filepath.Join(tmpDir, "uc")
	cfg.Tools.AutoApproveCommands = true
	configtest.RunTools(cfg)

	agentReg, err := agents.NewRegistry()
	require.NoError(t, err, "failed to create agent registry")
	skillProv, err := skills.NewProvider()
	require.NoError(t, err, "failed to create skill provider")

	toolReg, err := NewRegistry(cfg, agentReg, skillProv)
	require.NoError(t, err, "failed to create tool registry")
	defer toolReg.Close()

	all := toolReg.GetAllTools()
	assert.GreaterOrEqual(t, len(all), 10, "expected at least 10 tools, got %d", len(all))
	seen := map[string]bool{}
	for _, tl := range all {
		assert.NotEqual(t, "", tl.Name(), "tool missing name: %T", tl)
		assert.NotEqual(t, "", tl.Description(), "tool %s missing description", tl.Name())
		assert.False(t, seen[tl.Name()], "GetAllTools returned duplicate %s", tl.Name())
		seen[tl.Name()] = true
	}
	assert.True(t, seen["manage_background_process"], "expected manage_background_process to be registered")

	// UC tools dir is created lazily, not at registry construction.
	_, err = os.Stat(cfg.Tools.UCToolsDir)
	assert.ErrorIs(t, err, fs.ErrNotExist, "expected uc tools dir to not be created eagerly, stat err=%v", err)

	writeFile(t, filepath.Join(tmpDir, "test.txt"), "line 1\nline 2 target\nline 3\n")
	ws := toolReg.Workspace()
	hooks := toolReg.Hooks()

	readOut := runTool(t, toolOf(t)(NewReadFileTool(ws)), map[string]any{"path": "test.txt"})
	content, _ := readOut["content"].(string)
	assert.Contains(t, content, "line 2 target", "expected read_file to contain 'line 2 target', got: %v", content)

	runTool(t, toolOf(t)(NewReplaceInFileTool(ws, hooks)), map[string]any{
		"path":                "test.txt",
		"target_content":      "line 2 target",
		"replacement_content": "line 2 replaced",
	})
	b, _ := os.ReadFile(filepath.Join(tmpDir, "test.txt"))
	assert.Contains(t, string(b), "line 2 replaced", "expected updated content, got %s", b)

	grepOut := runTool(t, toolOf(t)(NewGrepTool(ws)), map[string]any{"query": "line 2 replaced"})
	matches, _ := grepOut["matches"].([]any)
	assert.NotEqual(t, 0, len(matches), "expected grep to find matches, got 0")

	shellOut := runTool(t, toolOf(t)(NewRunShellCommandTool(ShellConfig{Workspace: ws, Hooks: hooks})),
		map[string]any{"command": "echo 'puppy power'"})
	output, _ := shellOut["output"].(string)
	assert.Contains(t, output, "puppy power", "expected shell output 'puppy power', got: %s", output)

	got := toolReg.GetToolsForAgent([]string{"read_file", "list_files", "run_shell_command"})
	assert.Len(t, got, 3, "expected 3 tools for agent, got %d", len(got))
	// Aliases resolve to the same tool and are de-duplicated.
	got = toolReg.GetToolsForAgent([]string{"edit", "replace_in_file", "missing_tool"})
	assert.Len(t, got, 1, "expected alias de-duplication and unknown names skipped, got %d tools", len(got))
}

func TestRegistryRejectsMissingWorkspace(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = filepath.Join(t.TempDir(), "does-not-exist")
	_, err := NewRegistry(cfg, nil, nil)
	require.Error(t, err, "expected error for missing workspace directory")
}

func TestRegistryCloseKillsBackgroundProcesses(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	reg, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	bp, err := reg.Processes().Start("", "sleep 30", reg.Workspace().Dir())
	require.NoError(t, err)
	reg.Close()
	select {
	case <-bp.done:
	case <-time.After(5 * time.Second):
		t.Fatal("background process survived registry Close")
	}
}
