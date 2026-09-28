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

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHooksApprovePolicy(t *testing.T) {
	ctx := context.Background()
	cmd := api.ApprovalRequest{Tool: "run_shell_command", Kind: api.ActionCommand, Detail: "ls"}
	write := api.ApprovalRequest{Tool: "create_file", Kind: api.ActionWrite, Detail: "f"}

	// Positive: bypass mode covers everything.
	all := NewHooks(Policy{Mode: api.ModeBypass})
	assert.NoError(t, all.Approve(ctx, cmd), "auto-approve-all denied command")
	assert.NoError(t, all.Approve(ctx, write), "auto-approve-all denied write")

	// AutoApproveCommands covers commands only; writes still fail closed.
	cmds := NewHooks(Policy{AutoApproveCommands: true})
	assert.NoError(t, cmds.Approve(ctx, cmd), "auto-approve-commands denied command")
	err := cmds.Approve(ctx, write)
	assert.ErrorIs(t, err, ErrNotApproved, "expected write to need approval, got %v", err)

	// Negative: no approver configured => denied (fail closed), incl. nil hooks.
	err = NewHooks(Policy{}).Approve(ctx, cmd)
	assert.ErrorIs(t, err, ErrNotApproved, "expected fail-closed denial, got %v", err)
	var nilHooks *Hooks
	err = nilHooks.Approve(ctx, cmd)
	assert.ErrorIs(t, err, ErrNotApproved, "expected nil hooks to deny, got %v", err)

	// Approver decisions and errors.
	yes, reqs := approverHooks(true)
	assert.NoError(t, yes.Approve(ctx, cmd), "approver said yes but got")
	assert.Len(t, *reqs, 1, "approver did not receive request: %v", *reqs)
	assert.Equal(t, "ls", (*reqs)[0].Detail, "approver did not receive request: %v", *reqs)
	no, _ := approverHooks(false)
	err = no.Approve(ctx, cmd)
	assert.ErrorIs(t, err, ErrNotApproved, "approver said no but got %v", err)
	failing := NewHooks(Policy{})
	failing.SetApprover(func(context.Context, api.ApprovalRequest) (api.Decision, error) {
		return api.DecisionOnce, errors.New("tty gone")
	})
	err = failing.Approve(ctx, cmd)
	assert.ErrorIs(t, err, ErrNotApproved, "approver error should deny, got %v", err)
}

func TestMutatingToolsRespectApproval(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "existing.txt"), "original")

	type call struct {
		name  string
		build func(*Hooks) runnerTool
		args  map[string]any
		check func(t *testing.T, applied bool)
	}
	exists := func(p string) bool { _, err := os.Stat(filepath.Join(dir, p)); return err == nil }
	content := func(p string) string { b, _ := os.ReadFile(filepath.Join(dir, p)); return string(b) }

	calls := []call{
		{"create_file", func(h *Hooks) runnerTool { return toolOf(t)(NewCreateFileTool(ws, h)) },
			map[string]any{"path": "created.txt", "content": "x"},
			func(t *testing.T, applied bool) {
				assert.Equal(t, applied, exists("created.txt"), "create_file applied=%v but file exists=%v", applied, exists("created.txt"))
			}},
		{"replace_in_file", func(h *Hooks) runnerTool { return toolOf(t)(NewReplaceInFileTool(ws, h)) },
			map[string]any{"path": "existing.txt", "target_content": "original", "replacement_content": "edited"},
			func(t *testing.T, applied bool) {
				assert.Equal(t, applied, (content("existing.txt") == "edited"), "replace_in_file applied=%v content=%q", applied, content("existing.txt"))
			}},
		{"delete_snippet", func(h *Hooks) runnerTool { return toolOf(t)(NewDeleteSnippetTool(ws, h)) },
			map[string]any{"path": "existing.txt", "snippet": "ited"},
			func(t *testing.T, applied bool) {
				assert.Equal(t, applied, (content("existing.txt") == "ed"), "delete_snippet applied=%v content=%q", applied, content("existing.txt"))
			}},
		{"run_shell_command", func(h *Hooks) runnerTool {
			return toolOf(t)(NewRunShellCommandTool(ShellConfig{Workspace: ws, Hooks: h}))
		},
			map[string]any{"command": "touch ran.marker"},
			func(t *testing.T, applied bool) {
				assert.Equal(t, applied, exists("ran.marker"), "shell applied=%v marker exists=%v", applied, exists("ran.marker"))
			}},
		{"delete_file", func(h *Hooks) runnerTool { return toolOf(t)(NewDeleteFileTool(ws, h)) },
			map[string]any{"path": "existing.txt"},
			func(t *testing.T, applied bool) {
				assert.NotEqual(t, applied, exists("existing.txt"), "delete_file applied=%v but file exists=%v", applied, exists("existing.txt"))
			}},
	}

	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			// Negative first: denial leaves the workspace untouched and reports why.
			denied, deniedReqs := approverHooks(false)
			out := runTool(t, c.build(denied), c.args)
			assert.Contains(t, errOf(out), "not approved", "expected not-approved error, got %v", out)
			assert.Len(t, *deniedReqs, 1, "expected exactly one approval request, got %d", len(*deniedReqs))
			c.check(t, false)

			// No approver at all: fail closed.
			out = runTool(t, c.build(NewHooks(Policy{})), c.args)
			assert.Contains(t, errOf(out), "not approved", "expected fail-closed denial, got %v", out)
			c.check(t, false)

			// Positive: approval applies the change.
			approved, reqs := approverHooks(true)
			out = runTool(t, c.build(approved), c.args)
			require.Equal(t, "", errOf(out), "approved call failed: %v", out)
			assert.Len(t, *reqs, 1, "unexpected approval requests %v", *reqs)
			assert.Equal(t, c.name, (*reqs)[0].Tool, "unexpected approval requests %v", *reqs)
			c.check(t, true)
		})
	}
}

// Approval requests name their targets, which policies such as a worker's
// permissions match against.
func TestApprovalRequestsNameTheirTargets(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	var got [][]string
	h := NewHooks(Policy{})
	h.SetApprover(func(_ context.Context, req api.ApprovalRequest) (api.Decision, error) {
		got = append(got, req.Targets)
		return api.DecisionOnce, nil
	})
	create, err := NewCreateFileTool(ws, h)
	require.NoError(t, err)
	runTool(t, create.(runnerTool), map[string]any{"path": "reports/deps.md", "content": "x\n"})
	cfg := ShellConfig{Workspace: ws, Hooks: h, Exec: &ExecEnv{Dir: dir}}
	runShellCommand(context.Background(), cfg, RunShellCommandInput{Command: "echo hi"})
	assert.Len(t, got, 2, "targets %q", got)
	assert.Equal(t, []string{"reports/deps.md"}, got[0], "targets %q", got)
	assert.Equal(t, []string{"echo hi"}, got[1], "targets %q", got)
}
