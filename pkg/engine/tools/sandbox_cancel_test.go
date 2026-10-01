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
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A scan for blocked paths stops when its context ends, and what it read
// stays cached: the next scan finds the blocked file.
func TestBlockedScanStopsWhenCancelled(t *testing.T) {
	ws := t.TempDir()
	for i := range 600 { // past cancelCheck entries
		writeFile(t, filepath.Join(ws, fmt.Sprintf("d%d", i/50), fmt.Sprintf("f%d", i)), "x")
	}
	writeFile(t, filepath.Join(ws, "d0", ".env"), "x")
	m, err := NewPathMatcher([]string{".env"}, []string{ws})
	require.NoError(t, err)
	scan := newBlockedScan(OSSandboxSpec{WritableDirs: []string{ws}, Blocked: m})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = scan.expandCtx(cancelled)
	assert.ErrorIs(t, err, context.Canceled)

	files, dirs, err := scan.expandCtx(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(ws, "d0", ".env")}, files)
	assert.Empty(t, dirs)
}

// slowSandbox is an active sandbox that's never ready: it waits for the
// command's context to end, as a long cold scan would.
func slowSandbox(t *testing.T) *OSSandbox {
	t.Helper()
	orig := platformSandbox
	t.Cleanup(func() { platformSandbox = orig })
	platformSandbox = func(OSSandboxSpec) (sandboxWrapper, error) {
		return func(ctx context.Context, argv []string) ([]string, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, nil
	}
	box, err := NewOSSandbox(OSSandboxSpec{Mode: SandboxRequired})
	require.NoError(t, err)
	return box
}

// A command whose context ends while the sandbox gets ready never starts.
func TestCommandNotStartedWhileTheSandboxGetsReady(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	cmd, err := (&ExecEnv{Sandbox: slowSandbox(t)}).command(ctx, []string{"touch", marker})
	assert.Nil(t, cmd)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.NoFileExists(t, marker)
}

// The shell tool's timeout holds while the sandbox gets ready, and says
// so as it does for a command it stopped; a cancelled turn says that.
func TestShellTimeoutWhileTheSandboxGetsReady(t *testing.T) {
	ws, err := OpenWorkspace(WorkspaceOptions{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	cfg := ShellConfig{Workspace: ws, Hooks: allowAll(), Exec: &ExecEnv{Sandbox: slowSandbox(t)}}

	start := time.Now()
	out := runShellCommand(context.Background(), cfg, RunShellCommandInput{Command: "true", TimeoutSeconds: 1})
	assert.Equal(t, -1, out.ExitCode)
	assert.Equal(t, "command timed out after 1s", out.Error)
	assert.Less(t, time.Since(start), 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	out = runShellCommand(ctx, cfg, RunShellCommandInput{Command: "true"})
	assert.Equal(t, -1, out.ExitCode)
	assert.Equal(t, "command cancelled", out.Error)
}
