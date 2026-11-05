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

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Turns running at once keep their own changes: each change goes to its
// session's latest turn, and a worker run's turn is its own checkpoint,
// out of /checkpoints and /undo (BL-WK-01, BL-WK-02).
func TestWorkerRunCheckpoints(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	c := newTestCheckpoints(ws, 0)
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
	res, err := c.Undo("", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"chat.txt"}, res.Restored)
	assert.Equal(t, "worker's", read("shared.txt"))
	_, err = c.Undo("", false)
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
	c := newTestCheckpoints(ws, 0)
	c.BeginTurn("a", 0, "first")
	c.BeginTurn("b", 0, "second")
	require.NoError(t, ws.WriteFileAtomic(context.Background(), "x.txt", []byte("x")))
	require.NoError(t, ws.WriteFileAtomic(WithOwnerSession(context.Background(), "other"), "y.txt", []byte("y")))
	list := c.List()
	require.Len(t, list, 1)
	assert.Equal(t, "second", list[0].Label)
	assert.Equal(t, []string{"x.txt", "y.txt"}, list[0].Files)
}

// Undo takes the session: its latest turn with changes, counting its
// sub-agents' and tasks' (made under its owner session), and never
// another session's, even a later one. A session with nothing to undo
// says so though others have changes; conflicts work as before (FS-63a).
func TestUndoTakesTheSession(t *testing.T) {
	in := func(session string) context.Context { return WithOwnerSession(context.Background(), session) }
	// Two chats, a and b, each with one turn; b's is the workspace's latest.
	setup := func(t *testing.T) (*Workspace, *Checkpoints, string) {
		ws, dir := newTestWorkspace(t)
		c := newTestCheckpoints(ws, 0)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("original"), 0o644))
		c.BeginTurn("a", 0, "a's prompt")
		require.NoError(t, ws.WriteFileAtomic(in("a"), "a.txt", []byte("by a")))
		c.BeginTurn("b", 0, "b's prompt")
		require.NoError(t, ws.WriteFileAtomic(in("b"), "b.txt", []byte("by b")))
		// a's sub-agent writes after b's turn began: still a's.
		require.NoError(t, ws.WriteFileAtomic(in("a"), "shared.txt", []byte("a's sub-agent")))
		return ws, c, dir
	}
	exists := func(dir, name string) bool { _, err := os.Stat(filepath.Join(dir, name)); return err == nil }

	cases := map[string]struct {
		session    string
		edit       func(t *testing.T, ws *Workspace, dir string)
		force      bool
		label      string
		restored   []string
		err        error
		kept, gone []string
	}{
		"a undoes its own turn, not b's later one": {
			session: "a", label: "a's prompt", restored: []string{"a.txt", "shared.txt"},
			kept: []string{"b.txt"}, gone: []string{"a.txt"},
		},
		"b undoes its own turn, not a's": {
			session: "b", label: "b's prompt", restored: []string{"b.txt"},
			kept: []string{"a.txt"}, gone: []string{"b.txt"},
		},
		"a chat with no changes has nothing to undo": {
			session: "c", err: ErrNothingToUndo, kept: []string{"a.txt", "b.txt"},
		},
		"another chat's later edit is a conflict": {
			session: "a",
			edit: func(t *testing.T, ws *Workspace, _ string) {
				require.NoError(t, ws.WriteFileAtomic(in("b"), "shared.txt", []byte("b's edit")))
			},
			err: api.ErrUndoConflict, kept: []string{"a.txt", "b.txt"},
		},
		"forced past the conflict": {
			session: "a", force: true,
			edit: func(t *testing.T, ws *Workspace, _ string) {
				require.NoError(t, ws.WriteFileAtomic(in("b"), "shared.txt", []byte("b's edit")))
			},
			label: "a's prompt", restored: []string{"a.txt", "shared.txt"}, kept: []string{"b.txt"}, gone: []string{"a.txt"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ws, c, dir := setup(t)
			if tc.edit != nil {
				tc.edit(t, ws, dir)
			}
			res, err := c.Undo(tc.session, tc.force)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.label, res.Turn.Label)
				assert.Equal(t, tc.restored, res.Restored)
			}
			for _, f := range tc.kept {
				assert.True(t, exists(dir, f), "%s was undone", f)
			}
			for _, f := range tc.gone {
				assert.False(t, exists(dir, f), "%s is still there", f)
			}
		})
	}

	t.Run("each chat's latest turn in turn", func(t *testing.T) {
		_, c, dir := setup(t)
		_, err := c.Undo("a", false)
		require.NoError(t, err)
		_, err = c.Undo("a", false)
		assert.ErrorIs(t, err, ErrNothingToUndo, "a's only turn is gone; b's isn't a's")
		assert.True(t, exists(dir, "b.txt"))
		_, err = c.Undo("b", false)
		require.NoError(t, err)
		assert.Empty(t, c.List())
	})
}
