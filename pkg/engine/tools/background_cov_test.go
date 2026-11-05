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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Processes are listed per session, by ID; Running leaves out the ones
// that exited, and SessionOf names who started each.
func TestProcessManagerQueries(t *testing.T) {
	pm := NewProcessManager(0, 0)
	t.Cleanup(pm.Shutdown)
	dir := t.TempDir()
	done, err := pm.Start("s1", "true", dir)
	require.NoError(t, err)
	<-done.done
	running, err := pm.Start("s2", "sleep 30", dir)
	require.NoError(t, err)
	_, err = pm.Start("", "sleep 30", dir)
	require.NoError(t, err)

	var got []int
	for _, p := range pm.ListIn([]string{"s1", "s2"}) {
		got = append(got, p.ID)
	}
	assert.Equal(t, []int{done.ID, running.ID}, got, "the sessions' processes, in order")
	assert.Empty(t, pm.ListIn([]string{"nobody"}))

	got = nil
	for _, p := range pm.Running() {
		got = append(got, p.ID)
	}
	assert.Equal(t, []int{running.ID, running.ID + 1}, got, "only the running ones")

	session, err := pm.SessionOf(running.ID)
	require.NoError(t, err)
	assert.Equal(t, "s2", session)
	_, err = pm.SessionOf(99)
	assert.ErrorContains(t, err, "no background process with ID 99")
	_, err = pm.Kill(99)
	assert.ErrorContains(t, err, "no background process with ID 99")
}

// WaitAll returns once every process has exited, or when its context ends.
func TestProcessManagerWaitAll(t *testing.T) {
	pm := NewProcessManager(0, 0)
	t.Cleanup(pm.Shutdown)
	dir := t.TempDir()
	_, err := pm.Start("", "true", dir)
	require.NoError(t, err)
	require.NoError(t, pm.WaitAll(context.Background()), "a quick process")

	_, err = pm.Start("", "sleep 30", dir)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, pm.WaitAll(ctx), context.DeadlineExceeded, "a process still running")
}

// A process that can't start, or whose command can't be prepared, isn't
// tracked.
func TestProcessManagerStartFailures(t *testing.T) {
	pm := NewProcessManager(0, 0)
	t.Cleanup(pm.Shutdown)
	_, err := pm.Start("", "true", filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err, "a directory that isn't there")

	failCredentialsDir(t)
	pm.exec = &ExecEnv{Credentials: []Credential{signedIn(t, "CRED")}}
	_, err = pm.Start("", "true", t.TempDir())
	assert.ErrorContains(t, err, "copying the sign-in's credentials")
	assert.Empty(t, pm.List(), "nothing tracked")
}

// A watched process's exit notice carries only the end of a long output.
func TestWatchedProcessLongOutput(t *testing.T) {
	pm := NewProcessManager(0, 0)
	t.Cleanup(pm.Shutdown)
	var mu sync.Mutex
	var got string
	pm.Notice = func(_, text string) { mu.Lock(); defer mu.Unlock(); got = text }
	_, err := pm.StartWatched("", `head -c 3000 /dev/zero | tr '\0' a; printf END`, t.TempDir(), &Watch{Exit: true})
	require.NoError(t, err)
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return got != "" }, 10*time.Second, 20*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, got, "Its last output:\n…a")
	assert.True(t, strings.HasSuffix(got, "aEND"), "the end is kept")
	assert.Less(t, len(got), 1700, "the start is dropped")
}

// Killing an unknown process through the tool reports the error.
func TestManageBackgroundKillUnknown(t *testing.T) {
	pm := NewProcessManager(0, 0)
	t.Cleanup(pm.Shutdown)
	mgr := toolOf(t)(NewManageBackgroundTool(pm))
	res := runTool(t, mgr, map[string]any{"action": "kill", "process_id": 7})
	assert.Contains(t, errOf(res), "no background process with ID 7")
}

// The agent's tool sees and stops only its own chat's processes (N26
// engine-tools-1#9).
func TestManageBackgroundOwnSession(t *testing.T) {
	pm := NewProcessManager(0, 0)
	t.Cleanup(pm.Shutdown)
	ws := t.TempDir()
	mine, err := pm.Start("a", "sleep 5", ws)
	require.NoError(t, err)
	theirs, err := pm.Start("b", "sleep 5", ws)
	require.NoError(t, err)
	mgr := toolOf(t)(NewManageBackgroundTool(pm))
	ctx := WithOwnerSession(context.Background(), "a")
	for _, tc := range []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"output of another chat's", map[string]any{"action": "output", "process_id": theirs.ID}, "belongs to another chat"},
		{"kill another chat's", map[string]any{"action": "kill", "process_id": theirs.ID}, "belongs to another chat"},
		{"output of its own", map[string]any{"action": "output", "process_id": mine.ID}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := runToolCtx(t, ctx, mgr, tc.args)
			if tc.wantErr == "" {
				assert.Empty(t, errOf(res))
			} else {
				assert.Contains(t, errOf(res), tc.wantErr)
			}
		})
	}
	res := runToolCtx(t, ctx, mgr, map[string]any{"action": "list"})
	procs, _ := res["processes"].([]any)
	require.Len(t, procs, 1, "only its own: %v", res)
	_, err = pm.Kill(mine.ID)
	require.NoError(t, err)
	_, err = pm.Kill(theirs.ID)
	require.NoError(t, err)
}

// A process its lifetime stopped says so, not just its exit code (N26
// engine-tools-1#10).
func TestBackgroundLifetimeNotice(t *testing.T) {
	pm := NewProcessManager(0, 200*time.Millisecond)
	t.Cleanup(pm.Shutdown)
	notices := make(chan string, 1)
	pm.Notice = func(_, text string) { notices <- text }
	bp, err := pm.StartWatched("s", "sleep 5", t.TempDir(), &Watch{Exit: true})
	require.NoError(t, err)
	select {
	case n := <-notices:
		assert.Contains(t, n, "was stopped after 200ms, the fixed lifetime")
	case <-time.After(3 * time.Second):
		t.Fatal("no notice")
	}
	<-bp.done
}
