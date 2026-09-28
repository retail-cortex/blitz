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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func shellCfg(ws *Workspace) ShellConfig {
	return ShellConfig{Workspace: ws, Hooks: allowAll(), Processes: NewProcessManager(0, 0), DefaultTimeout: 10 * time.Second}
}

func TestShellBasicAndExitCodes(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	cfg := shellCfg(ws)
	ctx := context.Background()

	// Positive: stdout and stderr both captured.
	out := runShellCommand(ctx, cfg, RunShellCommandInput{Command: "echo out; echo err >&2"})
	assert.Equal(t, 0, out.ExitCode, "unexpected result %+v", out)
	assert.Contains(t, out.Output, "out", "unexpected result %+v", out)
	assert.Contains(t, out.Output, "err", "unexpected result %+v", out)
	// Positive: cwd inside workspace.
	out = runShellCommand(ctx, cfg, RunShellCommandInput{Command: "basename \"$PWD\"", Cwd: "sub"})
	assert.Equal(t, "sub", strings.TrimSpace(out.Output), "expected cwd sub, got %q", out.Output)
	// Non-zero exit code reported without Error.
	out = runShellCommand(ctx, cfg, RunShellCommandInput{Command: "exit 3"})
	assert.Equal(t, 3, out.ExitCode, "expected exit code 3, got %+v", out)
	assert.Equal(t, "", out.Error, "expected exit code 3, got %+v", out)

	// Negative: empty command, cwd outside workspace, missing cwd.
	out = runShellCommand(ctx, cfg, RunShellCommandInput{})
	assert.NotEqual(t, "", out.Error, "expected error for empty command")
	out = runShellCommand(ctx, cfg, RunShellCommandInput{Command: "pwd", Cwd: "/"})
	assert.NotEqual(t, "", out.Error, "expected error for cwd outside workspace")
	out = runShellCommand(ctx, cfg, RunShellCommandInput{Command: "pwd", Cwd: "missing"})
	assert.NotEqual(t, "", out.Error, "expected error for missing cwd")
}

func TestShellOutputCapped(t *testing.T) {
	ws, _ := newTestWorkspace(t)
	// 5MB of output must not be buffered in full.
	out := runShellCommand(context.Background(), shellCfg(ws), RunShellCommandInput{Command: "head -c 5000000 /dev/zero | tr '\\0' 'a'"})
	assert.True(t, out.Truncated, "expected truncated=true")
	assert.LessOrEqual(t, len(out.Output), shellOutputLimit+200, "output not capped: %d bytes", len(out.Output))
	assert.Contains(t, out.Output, "output truncated", "expected truncation notice")

	// Negative: small output is not marked truncated.
	out = runShellCommand(context.Background(), shellCfg(ws), RunShellCommandInput{Command: "echo small"})
	assert.False(t, out.Truncated, "small output marked truncated")
}

func TestShellTimeoutKillsProcessGroup(t *testing.T) {
	ws, _ := newTestWorkspace(t)
	start := time.Now()
	// The backgrounded sleep inherits the output pipe; without a process-group
	// kill and WaitDelay, Run would block for the full 30s.
	out := runShellCommand(context.Background(), shellCfg(ws), RunShellCommandInput{
		Command:        "sleep 30 & echo started; sleep 30",
		TimeoutSeconds: 1,
	})
	elapsed := time.Since(start)
	require.LessOrEqual(t, elapsed, 10*time.Second, "timeout did not kill descendants promptly: took %s", elapsed)
	assert.Contains(t, out.Error, "timed out", "expected timeout error, got %+v", out)
	assert.Equal(t, -1, out.ExitCode, "expected timeout error, got %+v", out)
	assert.Contains(t, out.Output, "started", "expected partial output preserved, got %q", out.Output)
}

func TestShellParentCancellation(t *testing.T) {
	ws, _ := newTestWorkspace(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	out := runShellCommand(ctx, shellCfg(ws), RunShellCommandInput{Command: "sleep 30"})
	assert.Equal(t, "command cancelled", out.Error, "expected cancellation, got %+v", out)
}

func TestShellTimeoutIsClamped(t *testing.T) {
	ws, _ := newTestWorkspace(t)
	// A huge requested timeout must not overflow or exceed the maximum.
	out := runShellCommand(context.Background(), shellCfg(ws), RunShellCommandInput{Command: "true", TimeoutSeconds: 1 << 40})
	assert.Equal(t, "", out.Error, "unexpected result %+v", out)
	assert.Equal(t, 0, out.ExitCode, "unexpected result %+v", out)
}

func TestBackgroundProcessLifecycle(t *testing.T) {
	ws, _ := newTestWorkspace(t)
	cfg := shellCfg(ws)
	t.Cleanup(cfg.Processes.Shutdown)

	out := runShellCommand(context.Background(), cfg, RunShellCommandInput{Command: "echo ready; sleep 30", Background: true})
	require.Equal(t, "", out.Error, "failed to start background process: %+v", out)
	require.True(t, out.IsBackground, "failed to start background process: %+v", out)
	require.NotEqual(t, 0, out.ProcessID, "failed to start background process: %+v", out)
	id := out.ProcessID

	mgr := toolOf(t)(NewManageBackgroundTool(cfg.Processes))

	// Output eventually contains the echo while still running.
	deadline := time.Now().Add(5 * time.Second)
	for {
		res := runTool(t, mgr, map[string]any{"action": "output", "process_id": id})
		if s, _ := res["output"].(string); strings.Contains(s, "ready") {
			assert.Equal(t, true, res["process"].(map[string]any)["running"], "expected process to still be running")
			break
		}
		require.False(t, time.Now().After(deadline), "background output never appeared")
		time.Sleep(50 * time.Millisecond)
	}

	list := runTool(t, mgr, map[string]any{"action": "list"})
	procs, _ := list["processes"].([]any)
	assert.Len(t, procs, 1, "expected 1 listed process, got %v", list)

	res := runTool(t, mgr, map[string]any{"action": "kill", "process_id": id})
	assert.Equal(t, "", errOf(res), "kill failed: %v", res)
	assert.Equal(t, false, res["process"].(map[string]any)["running"], "kill failed: %v", res)

	// Negative: unknown ID and unknown action.
	res = runTool(t, mgr, map[string]any{"action": "output", "process_id": 999})
	assert.NotEqual(t, "", errOf(res), "expected error for unknown process id")
	res = runTool(t, mgr, map[string]any{"action": "explode"})
	assert.NotEqual(t, "", errOf(res), "expected error for unknown action")
}

func TestProcessManagerLimits(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(1, 500*time.Millisecond)
	t.Cleanup(pm.Shutdown)

	bp, err := pm.Start("sleep 30", dir)
	require.NoError(t, err)
	// Negative: concurrency limit enforced.
	_, limitErr := pm.Start("sleep 30", dir)
	assert.ErrorContains(t, limitErr, "too many", "the concurrency limit is enforced")
	// Lifetime limit kills the process.
	select {
	case <-bp.done:
	case <-time.After(5 * time.Second):
		t.Fatal("process outlived its max lifetime")
	}
	// Positive: slot is free again.
	_, againErr := pm.Start("true", dir)
	assert.NoError(t, againErr, "a start succeeds once the slot is free")

	// Output is capped.
	pm2 := NewProcessManager(0, 0)
	t.Cleanup(pm2.Shutdown)
	big, err := pm2.Start("head -c 2000000 /dev/zero | tr '\\0' 'b'", dir)
	require.NoError(t, err)
	<-big.done
	out, info, _ := pm2.Output(big.ID)
	assert.LessOrEqual(t, len(out), backgroundOutputLimit+200, "background output not capped (%d bytes) or still running", len(out))
	assert.False(t, info.Running, "background output not capped (%d bytes) or still running", len(out))

	// After shutdown new processes are refused.
	pm2.Shutdown()
	_, err = pm2.Start("true", dir)
	assert.Error(t, err, "expected start after shutdown to fail")
}

func TestProcessManagerPrunesFinished(t *testing.T) {
	dir := t.TempDir()
	pm := NewProcessManager(0, 0)
	t.Cleanup(pm.Shutdown)
	for i := 0; i < maxFinishedProcsRetained+5; i++ {
		bp, err := pm.Start("true", dir)
		require.NoError(t, err)
		<-bp.done
	}
	n := len(pm.List())
	assert.LessOrEqual(t, n, maxFinishedProcsRetained+1, "finished processes not pruned: %d retained", n)
}

func TestCappedBuffer(t *testing.T) {
	b := newCappedBuffer(4)
	n, err := b.Write([]byte("ab"))
	require.Equal(t, 2, n, "unexpected write result %d %v", n, err)
	require.NoError(t, err, "unexpected write result %d", n)
	require.False(t, b.Truncated(), "unexpected write result %d %v", n, err)
	// Writes always report full length so producers aren't failed.
	n, _ = b.Write([]byte("cdef"))
	require.Equal(t, 4, n, "expected truncation, n=%d", n)
	require.True(t, b.Truncated(), "expected truncation, n=%d", n)
	s := b.String()
	assert.True(t, strings.HasPrefix(s, "abcd"), "unexpected String %q", s)
	assert.Contains(t, s, "2 bytes", "unexpected String %q", s)
	// Partial runes at the cut are trimmed.
	u := newCappedBuffer(2)
	u.Write([]byte("é!")) // é is 2 bytes; cut after it is fine
	u2 := newCappedBuffer(3)
	u2.Write([]byte("a🐶"))
	s = u2.String()
	assert.True(t, strings.HasPrefix(s, "a\n"), "expected partial rune trimmed, got %q", s)
}
