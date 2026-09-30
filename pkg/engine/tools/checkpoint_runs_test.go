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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Turns running at once keep their own changes: each change goes to its
// session's latest turn, and a worker run's turn is its own checkpoint,
// out of /checkpoints and /undo (BL-WK-01, BL-WK-02).
func TestWorkerRunCheckpoints(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	c := NewCheckpoints(ws, 0)
	in := func(session string) context.Context { return WithOwnerSession(context.Background(), session) }
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return string(b)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("original"), 0o644))

	c.BeginTurn("chat", 0, "the user's prompt")
	c.BeginWorkerTurn("run-session", "run-1", "the worker's prompt")
	// The chat writes after the worker began: its change is still the chat's.
	require.NoError(t, ws.WriteFileAtomic(in("chat"), "chat.txt", []byte("by chat")))
	require.NoError(t, ws.WriteFileAtomic(in("run-session"), "report.md", []byte("by worker")))
	require.NoError(t, ws.WriteFileAtomic(in("run-session"), "shared.txt", []byte("worker's")))

	list := c.List()
	require.Len(t, list, 1, "the worker's turn is listed: %+v", list)
	assert.Equal(t, []string{"chat.txt"}, list[0].Files)
	assert.Equal(t, []string{"report.md", "shared.txt"}, c.RunFiles("run-1"))
	assert.Empty(t, c.RunFiles("run-2"))

	// /undo reverts the chat's turn only.
	res, err := c.Undo(false)
	require.NoError(t, err)
	assert.Equal(t, []string{"chat.txt"}, res.Restored)
	assert.Equal(t, "worker's", read("shared.txt"))
	_, err = c.Undo(false)
	assert.ErrorIs(t, err, ErrNothingToUndo, "the worker's turn was undone by /undo")

	// A file changed since the run blocks its undo, unless forced.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("edited by hand"), 0o644))
	_, err = c.UndoRun("run-1", false)
	assert.ErrorContains(t, err, "shared.txt")
	res, err = c.UndoRun("run-1", true)
	require.NoError(t, err)
	assert.Equal(t, []string{"report.md", "shared.txt"}, res.Restored)
	assert.Equal(t, "original", read("shared.txt"))
	_, err = os.Stat(filepath.Join(dir, "report.md"))
	assert.True(t, os.IsNotExist(err), "the run's new file is still there")
	_, err = c.UndoRun("run-1", false)
	assert.ErrorIs(t, err, ErrNothingToUndo)
}

// Without a session (or with one that has no turn) a change goes to the
// latest turn, as before.
func TestCheckpointWithoutSession(t *testing.T) {
	ws, _ := newTestWorkspace(t)
	c := NewCheckpoints(ws, 0)
	c.BeginTurn("a", 0, "first")
	c.BeginTurn("b", 0, "second")
	require.NoError(t, ws.WriteFileAtomic(context.Background(), "x.txt", []byte("x")))
	require.NoError(t, ws.WriteFileAtomic(WithOwnerSession(context.Background(), "other"), "y.txt", []byte("y")))
	list := c.List()
	require.Len(t, list, 1)
	assert.Equal(t, "second", list[0].Label)
	assert.Equal(t, []string{"x.txt", "y.txt"}, list[0].Files)
}
