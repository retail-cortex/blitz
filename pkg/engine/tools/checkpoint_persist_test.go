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
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reopen opens the workspace and its persisted checkpoints again, as a
// later process would.
func reopen(t *testing.T, dir, store string) (*Workspace, *Checkpoints) {
	t.Helper()
	ws, err := NewWorkspace(dir, 0)
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	cp, err := OpenCheckpoints(ws, CheckpointOptions{Dir: store})
	require.NoError(t, err)
	return ws, cp
}

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func TestCheckpointsOutliveTheProcess(t *testing.T) {
	dir, store := t.TempDir(), filepath.Join(t.TempDir(), "cp")
	writeFile(t, filepath.Join(dir, "f.txt"), "v1\n")
	ws, cp := reopen(t, dir, store)
	cp.BeginTurn("s1", 0, "first")
	ws.WriteFileAtomic("f.txt", []byte("v2\n"))
	cp.BeginTurn("s1", 2, "second")
	ws.CreateExclusive("new.txt", []byte("n\n"))

	for _, p := range []string{store, filepath.Join(store, "index.json"), filepath.Join(store, "blobs")} {
		t.Run(p, func(t *testing.T) {
			info, err := os.Stat(p)
			require.NoError(t, err)
			assert.Equal(t, fs.FileMode(0), info.Mode().Perm()&0o077, "%s is %v, not owner-only", p, info.Mode().Perm())
		})
	}
	blobs, _ := os.ReadDir(filepath.Join(store, "blobs"))
	require.Len(t, blobs, 1, "blobs %v", blobs)
	info, _ := blobs[0].Info()
	assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "blob mode %v", info.Mode().Perm())

	// A later process sees both turns, and undoes them from the stored snapshots.
	ws.Close()
	_, cp2 := reopen(t, dir, store)
	l := cp2.List()
	require.Len(t, l, 2, "after reopening: %+v", l)
	require.Equal(t, "second", l[0].Label, "after reopening: %+v", l)
	require.Equal(t, "s1", l[0].Session, "after reopening: %+v", l)
	require.Equal(t, 2, l[0].Prompt, "after reopening: %+v", l)
	require.Equal(t, 0, l[1].Prompt, "after reopening: %+v", l)
	d := cp2.SessionDiff("s1")
	assert.Contains(t, d, "-v1", "session diff:\n%s", d)
	assert.Contains(t, d, "+v2", "session diff:\n%s", d)
	assert.Contains(t, d, "+n", "session diff:\n%s", d)
	d = cp2.SessionDiff("other")
	assert.Equal(t, "", d, "another session's diff: %q", d)
	for range 2 {
		_, err := cp2.Undo(false)
		require.NoError(t, err)
	}
	got := readString(t, filepath.Join(dir, "f.txt"))
	assert.Equal(t, "v1\n", got, "f.txt %q", got)
	_, err := os.Stat(filepath.Join(dir, "new.txt"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "new.txt not removed")
	// Undone turns' snapshots go.
	blobs, _ = os.ReadDir(filepath.Join(store, "blobs"))
	assert.Len(t, blobs, 0, "blobs left: %v", blobs)
	_, cp3 := reopen(t, dir, store)
	l = cp3.List()
	assert.Len(t, l, 0, "undone turns came back: %+v", l)
}

func TestRewindRestoresEveryChangeFromAPrompt(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	writeFile(t, filepath.Join(dir, "a.txt"), "a0\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "b0\n")
	cp.BeginTurn("s", 0, "p0")
	ws.WriteFileAtomic("a.txt", []byte("a1\n"))
	cp.BeginTurn("s", 2, "p1")
	ws.WriteFileAtomic("a.txt", []byte("a2\n"))
	ws.CreateExclusive("c.txt", []byte("c\n"))
	cp.BeginTurn("other", 0, "elsewhere")
	ws.WriteFileAtomic("b.txt", []byte("b1\n"))
	cp.BeginTurn("s", 4, "p2")
	ws.WriteFileAtomic("a.txt", []byte("a3\n"))

	res, err := cp.Rewind("s", 2, false)
	require.NoError(t, err)
	got := readString(t, filepath.Join(dir, "a.txt"))
	assert.Equal(t, "a1\n", got, "a.txt %q, want the state before prompt 2", got)
	_, err = os.Stat(filepath.Join(dir, "c.txt"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "c.txt, created from prompt 2 on, is still there")
	got = readString(t, filepath.Join(dir, "b.txt"))
	assert.Equal(t, "b1\n", got, "another session's change was undone: %q", got)
	assert.Equal(t, "a.txt,c.txt", strings.Join(res.Restored, ","), "restored %v", res.Restored)
	var left []string
	for _, s := range cp.List() {
		left = append(left, s.Label)
	}
	assert.Equal(t, "elsewhere,p0", strings.Join(left, ","), "turns left %v", left)
	_, err = cp.Rewind("s", 2, false)
	assert.ErrorIs(t, err, ErrNothingToUndo, "nothing left to rewind: %v", err)
}

func TestRewindChecksEveryFileFirst(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	writeFile(t, filepath.Join(dir, "a.txt"), "a0\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "b0\n")
	cp.BeginTurn("s", 0, "p0")
	ws.WriteFileAtomic("a.txt", []byte("a1\n"))
	cp.BeginTurn("s", 2, "p1")
	ws.WriteFileAtomic("b.txt", []byte("b1\n"))
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("mine\n"), 0o644)

	_, err := cp.Rewind("s", 0, false)
	require.ErrorIs(t, err, api.ErrUndoConflict, "conflict: %v", err)
	require.Contains(t, err.Error(), "b.txt", "conflict: %v", err)
	got := readString(t, filepath.Join(dir, "a.txt"))
	assert.Equal(t, "a1\n", got, "a file was restored before the conflict was found")
	_, err = cp.Rewind("s", 0, true)
	require.NoError(t, err)
	assert.Equal(t, "a0\n", readString(t, filepath.Join(dir, "a.txt")), "forced rewind didn't restore both")
	assert.Equal(t, "b0\n", readString(t, filepath.Join(dir, "b.txt")), "forced rewind didn't restore both")
}

// After the conversation is rewound without the files, the changes made
// from that prompt on belong before the next prompt.
func TestDetachAfterAConversationRewind(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	writeFile(t, filepath.Join(dir, "a.txt"), "a0\n")
	cp.BeginTurn("s", 0, "p0")
	ws.WriteFileAtomic("a.txt", []byte("a1\n"))
	cp.BeginTurn("s", 2, "p1")
	ws.WriteFileAtomic("a.txt", []byte("a2\n"))
	cp.Detach("s", 2)
	cp.BeginTurn("s", 2, "new p1")
	ws.WriteFileAtomic("a.txt", []byte("a3\n"))

	_, err := cp.Rewind("s", 2, false)
	require.NoError(t, err)
	got := readString(t, filepath.Join(dir, "a.txt"))
	require.Equal(t, "a2\n", got, "rewinding the new prompt: %q (the detached change must stay)", got)
	_, err = cp.Rewind("s", 0, false)
	require.NoError(t, err)
	got = readString(t, filepath.Join(dir, "a.txt"))
	require.Equal(t, "a0\n", got, "rewinding the first prompt: %q", got)
}

func TestPersistedCheckpointsAgeOutAndSurviveDamage(t *testing.T) {
	dir, store := t.TempDir(), filepath.Join(t.TempDir(), "cp")
	writeFile(t, filepath.Join(dir, "a.txt"), "a0\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "b0\n")
	ws, cp := reopen(t, dir, store)
	cp.BeginTurn("s", 0, "old")
	ws.WriteFileAtomic("a.txt", []byte("a1\n"))
	cp.BeginTurn("s", 2, "new")
	ws.WriteFileAtomic("b.txt", []byte("b1\n"))
	ws.Close()

	// Age the first turn past the limit.
	path := filepath.Join(store, "index.json")
	var idx checkpointIndex
	json.Unmarshal([]byte(readString(t, path)), &idx)
	idx.Turns[0].Started = time.Now().Add(-40 * 24 * time.Hour)
	data, _ := json.Marshal(idx)
	os.WriteFile(path, data, 0o600)

	_, cp2 := reopen(t, dir, store)
	l := cp2.List()
	require.Len(t, l, 1, "aged turn kept: %+v", l)
	require.Equal(t, "new", l[0].Label, "aged turn kept: %+v", l)
	blobs, _ := os.ReadDir(filepath.Join(store, "blobs"))
	assert.Len(t, blobs, 1, "the aged turn's snapshot wasn't removed: %d blobs", len(blobs))

	// A missing snapshot can't be restored.
	blobs, _ = os.ReadDir(filepath.Join(store, "blobs"))
	os.Remove(filepath.Join(store, "blobs", blobs[0].Name()))
	_, cp3 := reopen(t, dir, store)
	_, err := cp3.Undo(false)
	assert.Error(t, err, "undo without its snapshot")
	assert.Contains(t, err.Error(), "snapshot limit", "undo without its snapshot: %v", err)

	// A damaged index is set aside; the store starts empty.
	os.WriteFile(path, []byte("{nope"), 0o600)
	ws4, err := NewWorkspace(dir, 0)
	require.NoError(t, err)
	defer ws4.Close()
	cp4, err := OpenCheckpoints(ws4, CheckpointOptions{Dir: store})
	require.Error(t, err, "damaged index: %v %+v", err, cp4.List())
	require.Len(t, cp4.List(), 0, "damaged index: %v %+v", err, cp4.List())
	_, err = os.Stat(path + ".damaged")
	assert.NoError(t, err, "the damaged index wasn't kept aside")
}
