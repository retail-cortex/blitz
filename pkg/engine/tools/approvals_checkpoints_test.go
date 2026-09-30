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
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovalRemembering(t *testing.T) {
	ctx := context.Background()
	store, err := OpenApprovalStore(filepath.Join(t.TempDir(), "approvals.json"))
	require.NoError(t, err)
	h, reqs := decisionHooks(api.DecisionSession)
	h.SetStore(store)
	req := api.ApprovalRequest{Tool: "run_shell_command", Kind: api.ActionCommand, Key: "cmd:ls", KeyLabel: "this exact command"}

	// Session: asked once, then remembered for the same key only.
	for i := 0; i < 3; i++ {
		require.NoError(t, h.Approve(ctx, req))
	}
	assert.Len(t, *reqs, 1, "expected 1 prompt for repeated session-approved action, got %d", len(*reqs))
	other := req
	other.Key = "cmd:ls -la"
	h.Approve(ctx, other)
	assert.Len(t, *reqs, 2, "session approval leaked to a different key")
	assert.False(t, store.Has("cmd:ls"), "session decision must not be persisted")
	assert.True(t, h.RevokeSession("cmd:ls"), "RevokeSession should remove the rule once")
	assert.False(t, h.RevokeSession("cmd:ls"), "RevokeSession should remove the rule once")

	// Always: persisted and honoured by a fresh Hooks with the same store file.
	always, _ := decisionHooks(api.DecisionAlways)
	always.SetStore(store)
	require.NoError(t, always.Approve(ctx, api.ApprovalRequest{Tool: "t", Kind: api.ActionWrite, Key: "write:/ws"}))
	reloaded, err := OpenApprovalStore(store.path)
	require.NoError(t, err, "always rule not persisted")
	require.True(t, reloaded.Has("write:/ws"), "always rule not persisted: %v", err)
	fresh, freshReqs := approverHooks(false)
	fresh.SetStore(reloaded)
	err = fresh.Approve(ctx, api.ApprovalRequest{Tool: "t", Kind: api.ActionWrite, Key: "write:/ws"})
	assert.NoError(t, err, "saved rule not applied without prompting: %v prompts=%d", err, len(*freshReqs))
	assert.Len(t, *freshReqs, 0, "saved rule not applied without prompting: %v prompts=%d", err, len(*freshReqs))
	info, _ := os.Stat(store.path)
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "approvals file mode %v", info.Mode().Perm())
	ok, err := reloaded.Remove("write:/ws")
	assert.True(t, ok, "Remove failed")
	assert.NoError(t, err, "Remove failed")
	assert.False(t, reloaded.Has("write:/ws"), "Remove failed")

	// Negative: unkeyed requests can't be remembered; each one prompts.
	empty, _ := OpenApprovalStore(filepath.Join(t.TempDir(), "empty.json"))
	h2, reqs2 := decisionHooks(api.DecisionAlways)
	h2.SetStore(empty)
	for i := 0; i < 2; i++ {
		h2.Approve(ctx, api.ApprovalRequest{Tool: "t", Kind: api.ActionWrite})
	}
	assert.Len(t, *reqs2, 2, "unkeyed approvals should not be remembered: prompts=%d rules=%v", len(*reqs2), empty.Rules())
	assert.Len(t, empty.Rules(), 0, "unkeyed approvals should not be remembered: prompts=%d rules=%v", len(*reqs2), empty.Rules())

	// Negative: corrupt store file.
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte("{nope"), 0o600)
	_, err = OpenApprovalStore(bad)
	assert.Error(t, err, "expected error for corrupt approvals file")
}

func TestSavedCommandRuleCannotBypassDeny(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	store, _ := OpenApprovalStore(filepath.Join(t.TempDir(), "a.json"))
	store.Add("cmd:"+ws.Dir()+"\x00touch x", "")
	h := NewHooks(Policy{})
	h.SetStore(store)
	policy := mustPolicy(t, CommandPolicyConfig{Deny: []string{"touch *"}})
	out := runShellCommand(context.Background(), ShellConfig{Workspace: ws, Hooks: h, Policy: policy}, RunShellCommandInput{Command: "touch x"})
	assert.Contains(t, out.Error, "blocked", "saved rule bypassed deny: %+v", out)
	_, err := os.Stat(filepath.Join(dir, "x"))
	assert.Error(t, err, "denied command ran")
}

func TestWriteToolsSendDiffAndKey(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "one\ntwo\n")
	h, reqs := approverHooks(true)
	runTool(t, toolOf(t)(NewReplaceInFileTool(ws, h)), map[string]any{"path": "a.txt", "target_content": "two", "replacement_content": "TWO"})
	runTool(t, toolOf(t)(NewCreateFileTool(ws, h)), map[string]any{"path": "b.txt", "content": "new\n"})
	runTool(t, toolOf(t)(NewDeleteFileTool(ws, h)), map[string]any{"path": "b.txt"})
	require.Len(t, *reqs, 3, "expected 3 approvals, got %d", len(*reqs))
	r := *reqs
	assert.Contains(t, r[0].Diff, "-two", "replace approval: diff=%q key=%q", r[0].Diff, r[0].Key)
	assert.Contains(t, r[0].Diff, "+TWO", "replace approval: diff=%q key=%q", r[0].Diff, r[0].Key)
	assert.Equal(t, "write:"+ws.Dir(), r[0].Key, "replace approval: diff=%q key=%q", r[0].Diff, r[0].Key)
	assert.Contains(t, r[1].Diff, "/dev/null", "create approval diff: %q", r[1].Diff)
	assert.Contains(t, r[1].Diff, "+new", "create approval diff: %q", r[1].Diff)
	assert.Contains(t, r[2].Diff, "-new", "delete approval: diff=%q key=%q", r[2].Diff, r[2].Key)
	assert.Equal(t, "delete:"+ws.Dir(), r[2].Key, "delete approval: diff=%q key=%q", r[2].Diff, r[2].Key)
}

func TestCheckpointUndo(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "v1\n")
	os.Chmod(path, 0o755)

	cp.Begin("turn 1")
	ws.WriteFileAtomic(context.Background(), "f.txt", []byte("v2\n"))
	ws.WriteFileAtomic(context.Background(), "f.txt", []byte("v3\n")) // same turn: original snapshot kept
	ws.CreateExclusive(context.Background(), "new.txt", []byte("n\n"))
	cp.Begin("turn 2")
	ws.RemoveFile(context.Background(), "new.txt")

	l := cp.List()
	require.Len(t, l, 2, "unexpected checkpoints %+v", l)
	require.Equal(t, "turn 2", l[0].Label, "unexpected checkpoints %+v", l)
	require.Len(t, l[1].Files, 2, "unexpected checkpoints %+v", l)
	d := cp.SessionDiff("")
	assert.Contains(t, d, "-v1", "session diff missing change:\n%s", d)
	assert.Contains(t, d, "+v3", "session diff missing change:\n%s", d)

	// Undo turn 2: deleted file comes back.
	_, err := cp.Undo(false)
	require.NoError(t, err)
	b, _ := os.ReadFile(filepath.Join(dir, "new.txt"))
	assert.Equal(t, "n\n", string(b), "deleted file not restored: %q", b)
	// Undo turn 1: original content and mode restored, created file removed.
	res, err := cp.Undo(false)
	require.NoError(t, err)
	b, _ = os.ReadFile(path)
	assert.Equal(t, "v1\n", string(b), "content not restored: %q", b)
	info, _ := os.Stat(path)
	assert.Equal(t, fs.FileMode(0o755), info.Mode().Perm(), "mode not restored: %v", info.Mode().Perm())
	_, err = os.Stat(filepath.Join(dir, "new.txt"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "created file not removed by undo")
	assert.Len(t, res.Restored, 2, "restored %v", res.Restored)
	// Negative: nothing left.
	_, err = cp.Undo(false)
	assert.Error(t, err, "expected nothing to undo")
}

func TestCheckpointUndoConflict(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "orig\n")
	cp.Begin("edit")
	ws.WriteFileAtomic(context.Background(), "f.txt", []byte("tool edit\n"))
	os.WriteFile(path, []byte("user edit after\n"), 0o644) // changed outside the tools

	_, err := cp.Undo(false)
	require.ErrorIs(t, err, api.ErrUndoConflict, "expected conflict, got %v", err)
	b, _ := os.ReadFile(path)
	assert.Equal(t, "user edit after\n", string(b), "conflicting undo modified the file")
	_, err = cp.Undo(true)
	require.NoError(t, err, "forced undo")
	b, _ = os.ReadFile(path)
	assert.Equal(t, "orig\n", string(b), "forced undo did not restore: %q", b)
}

func TestCheckpointFailedWriteNotRecorded(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	os.Mkdir(filepath.Join(dir, "adir"), 0o755)
	cp.Begin("t")
	require.Error(t, ws.WriteFileAtomic(context.Background(), "adir", []byte("x")), "expected write over directory to fail")
	require.Error(t, ws.CreateExclusive(context.Background(), "adir", []byte("x")), "expected create over directory to fail")
	l := cp.List()
	assert.Len(t, l, 0, "failed writes were recorded: %+v", l)
	_, err := cp.Undo(false)
	assert.Error(t, err, "undo should have nothing to do")
	_, err = os.Stat(filepath.Join(dir, "adir"))
	assert.NoError(t, err, "directory was removed")
}

func TestCheckpointMemoryBudget(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 100)
	for i, name := range []string{"a", "b", "c"} {
		writeFile(t, filepath.Join(dir, name), strings.Repeat("x", 60))
		cp.Begin(name)
		ws.WriteFileAtomic(context.Background(), name, []byte{byte('0' + i)})
	}
	l := cp.List()
	assert.NotEqual(t, 0, len(l), "expected oldest turns dropped to fit budget, have %d", len(l))
	assert.NotEqual(t, 3, len(l), "expected oldest turns dropped to fit budget, have %d", len(l))
	assert.Equal(t, "c", l[0].Label, "most recent turn must be kept")
}

func TestCommandApprovalScopedToWorkspace(t *testing.T) {
	wsA, _ := newTestWorkspace(t)
	wsB, _ := newTestWorkspace(t)
	store, _ := OpenApprovalStore(filepath.Join(t.TempDir(), "a.json"))
	always, _ := decisionHooks(api.DecisionAlways)
	always.SetStore(store)
	runShellCommand(context.Background(), ShellConfig{Workspace: wsA, Hooks: always}, RunShellCommandInput{Command: "true"})

	// Same command, other workspace: must prompt again.
	h, reqs := approverHooks(false)
	h.SetStore(store)
	out := runShellCommand(context.Background(), ShellConfig{Workspace: wsB, Hooks: h}, RunShellCommandInput{Command: "true"})
	assert.Len(t, *reqs, 1, "saved rule leaked across workspaces: prompts=%d out=%+v", len(*reqs), out)
	assert.Contains(t, out.Error, "not approved", "saved rule leaked across workspaces: prompts=%d out=%+v", len(*reqs), out)
	// Same workspace: remembered.
	h2, reqs2 := approverHooks(false)
	h2.SetStore(store)
	out = runShellCommand(context.Background(), ShellConfig{Workspace: wsA, Hooks: h2}, RunShellCommandInput{Command: "true"})
	assert.Equal(t, "", out.Error, "saved rule not applied in its workspace: %+v", out)
	assert.Len(t, *reqs2, 0, "saved rule not applied in its workspace: %+v", out)
}

func TestHooksRunOutsideSandbox(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Tools.ApprovalsFile = ""
	outside := filepath.Join(t.TempDir(), "hook-log.txt") // not writable by sandboxed commands
	cfg.Hooks.PostTool = []config.HookConfig{{Command: "echo logged >> " + outside}}
	reg, err := NewRegistry(cfg, nil, nil)
	require.NoError(t, err)
	defer reg.Close()
	reg.ScriptHooks().PostTool(context.Background(), "s", "grep", nil, nil, nil)
	reg.ScriptHooks().flush(context.Background()) // post_tool hooks run in the background
	b, _ := os.ReadFile(outside)
	assert.Contains(t, string(b), "logged", "hook could not write outside the workspace; hooks should not be sandboxed")
}
