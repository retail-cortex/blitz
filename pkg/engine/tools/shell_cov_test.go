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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shell tool needs a workspace.
func TestRunShellCommandToolNeedsWorkspace(t *testing.T) {
	_, err := NewRunShellCommandTool(ShellConfig{})
	assert.ErrorContains(t, err, "requires a workspace")
}

// Commands that can't run say why, without an exit code from the command.
func TestRunShellCommandFailures(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "gone"), 0o755))
	tests := []struct {
		name  string
		cfg   func(t *testing.T) ShellConfig
		input RunShellCommandInput
		want  string
	}{
		{
			name:  "no background processes",
			cfg:   func(*testing.T) ShellConfig { c := shellCfg(ws); c.Processes = nil; return c },
			input: RunShellCommandInput{Command: "true", Background: true},
			want:  "background execution is not available",
		},
		{
			name:  "a bad pattern",
			cfg:   func(*testing.T) ShellConfig { return shellCfg(ws) },
			input: RunShellCommandInput{Command: "true", Background: true, NotifyPattern: "("},
			want:  "notify_pattern",
		},
		{
			name: "credentials that can't be copied",
			cfg: func(t *testing.T) ShellConfig {
				failCredentialsDir(t)
				c := shellCfg(ws)
				c.Exec = &ExecEnv{Credentials: []Credential{signedIn(t, "CRED")}}
				return c
			},
			input: RunShellCommandInput{Command: "true"},
			want:  "failed to prepare command",
		},
		{
			name: "a background process that can't be prepared",
			cfg: func(t *testing.T) ShellConfig {
				failCredentialsDir(t)
				c := shellCfg(ws)
				c.Processes.exec = &ExecEnv{Credentials: []Credential{signedIn(t, "CRED")}}
				t.Cleanup(c.Processes.Shutdown)
				return c
			},
			input: RunShellCommandInput{Command: "true", Background: true},
			want:  "failed to start background command",
		},
		{
			name: "a directory removed",
			cfg: func(t *testing.T) ShellConfig {
				c := shellCfg(ws)
				require.NoError(t, os.Remove(filepath.Join(dir, "gone")))
				return c
			},
			input: RunShellCommandInput{Command: "true", Cwd: "gone"},
			want:  "gone",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runShellCommand(context.Background(), tt.cfg(t), tt.input)
			assert.Contains(t, out.Error, tt.want)
		})
	}
}

// A background command with a pattern reports the lines that match.
func TestRunShellCommandNotifyPattern(t *testing.T) {
	ws, _ := newTestWorkspace(t)
	cfg := shellCfg(ws)
	t.Cleanup(cfg.Processes.Shutdown)
	var mu sync.Mutex
	var notices []string
	cfg.Processes.Notice = func(_, text string) { mu.Lock(); defer mu.Unlock(); notices = append(notices, text) }
	out := runShellCommand(context.Background(), cfg, RunShellCommandInput{Command: "echo listening on 8080", Background: true, NotifyPattern: "listening"})
	require.Empty(t, out.Error)
	assert.Contains(t, out.Output, "you'll be told when it exits")
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(notices) == 2 }, 10*time.Second, 20*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, notices[0], "printed: listening on 8080")
	assert.Contains(t, notices[1], "exited with code 0")
}

// An empty owner session leaves the context as it was.
func TestWithOwnerSessionEmpty(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, ctx, WithOwnerSession(ctx, ""))
}
