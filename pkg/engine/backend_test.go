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

package engine

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/config/configtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A client sees and stops only the background processes its own sessions
// started; another session's are unknown to it.
func TestProcessesAreScopedToSessions(t *testing.T) {
	w, _ := openTestWith(t, configtest.RunTools)
	bp, err := w.Tools().Processes().Start("mine", "echo hello; sleep 30", w.Dir())
	require.NoError(t, err)
	assert.Same(t, w.Tools().Processes(), w.Processes())

	assert.Len(t, w.ListProcesses([]string{"mine"}), 1)
	assert.Empty(t, w.ListProcesses([]string{"theirs"}))
	for _, sessions := range [][]string{{"theirs"}, nil} {
		_, _, err = w.ProcessOutput(sessions, bp.ID)
		assert.ErrorIs(t, err, api.ErrUnknownProcess, "output for %v", sessions)
		_, err = w.KillProcess(sessions, bp.ID)
		assert.ErrorIs(t, err, api.ErrUnknownProcess, "kill for %v", sessions)
	}
	_, _, err = w.ProcessOutput([]string{"mine"}, 999)
	assert.ErrorIs(t, err, api.ErrUnknownProcess, "a process that never ran")

	require.Eventually(t, func() bool {
		out, _, err := w.ProcessOutput([]string{"mine"}, bp.ID)
		return err == nil && out == "hello\n"
	}, 10*time.Second, 20*time.Millisecond, "the process's output")
	info, err := w.KillProcess([]string{"mine"}, bp.ID)
	require.NoError(t, err)
	assert.Equal(t, bp.ID, info.ID)
}

// SetUI routes approvals and questions to the front end; nil leaves the
// current one in place.
func TestSetUI(t *testing.T) {
	w, _ := openTestWith(t, nil, toolCall("create_file", map[string]any{"path": "a.txt", "content": "a"}), text("done"))
	asked := 0
	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) {
		asked++
		return api.DecisionOnce, nil
	}, func(context.Context, string, []string) (string, error) { return "", nil })
	w.SetUI(nil, nil)
	_, err := w.Run(context.Background(), newSession(t, w).ID, api.Turn{Text: "write"}, ignore)
	require.NoError(t, err)
	assert.Equal(t, 1, asked, "the approver set first still answers")
	assert.FileExists(t, filepath.Join(w.Dir(), "a.txt"))
}

// Commands the user ran directly go to the audit log, with why they
// couldn't start.
func TestAuditShell(t *testing.T) {
	dir := t.TempDir()
	w, _ := openTestWith(t, func(c *config.Config) { c.Audit.Enabled, c.Audit.Dir = true, dir })
	w.AuditShell("make test", 2, nil)
	w.AuditShell("nope", -1, errors.New("not found"))
	require.NoError(t, w.Close())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	var all string
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		all += string(b)
	}
	assert.Contains(t, all, "make test")
	assert.Contains(t, all, "exit 2")
	assert.Contains(t, all, "not found")
}

// Images can be attached only when enabled.
func TestImagesEnabled(t *testing.T) {
	for _, on := range []bool{true, false} {
		t.Run(map[bool]string{true: "on", false: "off"}[on], func(t *testing.T) {
			w, _ := openTestWith(t, func(c *config.Config) { c.Images.Enabled = on })
			assert.Equal(t, on, w.ImagesEnabled())
			_, err := w.AddImage("x.png", []byte("not an image"))
			if on {
				assert.Error(t, err, "not an image")
			} else {
				assert.ErrorIs(t, err, api.ErrImagesDisabled)
			}
		})
	}
}

// With no background tasks, listing, waiting on and stopping one, and
// answering a request, say there's nothing there.
func TestTasksWithoutAny(t *testing.T) {
	w := openTest(t)
	assert.Empty(t, w.ListTasks())
	assert.Empty(t, w.ListTasksIn(nil))
	assert.Empty(t, w.PendingTaskRequests())
	assert.Empty(t, w.PendingTaskRequestsIn(nil))
	_, _, err := w.Task("t1")
	assert.ErrorIs(t, err, api.ErrUnknownTask)
	_, err = w.StopTask("t1")
	assert.ErrorIs(t, err, api.ErrUnknownTask)
	assert.Error(t, w.AnswerTaskRequest("r1", api.DecisionOnce, ""))
	ctx, cancel := context.WithCancel(context.Background())
	ch := w.WatchTasks(ctx, nil)
	cancel()
	for range ch { // closed once ctx ends
	}
}

// Diagnostics start whatever the configuration allows; a log that can't
// be opened only warns.
func TestStartObservability(t *testing.T) {
	cfg := isolatedConfig(t)
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o644))
	cfg.Log.Level, cfg.Log.Dir = "debug", filepath.Join(blocker, "logs") // under a file: can't be created
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var warnings []string
	stop := StartObservability(context.Background(), cfg, "test", func(s string) { warnings = append(warnings, s) })
	stop()
	assert.NotEmpty(t, warnings, "the log couldn't be opened")
}
