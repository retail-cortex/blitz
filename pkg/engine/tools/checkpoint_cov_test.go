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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A nil store (checkpoints disabled) is safe to call: the recorders do
// nothing and the restorers say checkpoints are off.
func TestNilCheckpoints(t *testing.T) {
	var c *Checkpoints
	c.BeginTurn("s", 0, "x")
	c.BeginWorkerTurn("s", "r", "x")
	c.Detach("s", 0)
	c.discard("s", "/a")
	c.after("s", "/a", nil, false)
	assert.False(t, c.before("s", "/a", "a", fileState{}, nil))
	assert.Nil(t, c.List())
	assert.Nil(t, c.RunFiles("r"))
	assert.Empty(t, c.SessionDiff(""))
	restorers := map[string]func() error{
		"undo":     func() error { _, err := c.Undo(false); return err },
		"rewind":   func() error { _, err := c.Rewind("s", 0, false); return err },
		"undo run": func() error { _, err := c.UndoRun("r", false); return err },
	}
	for name, f := range restorers {
		t.Run(name, func(t *testing.T) {
			assert.ErrorContains(t, f(), "disabled")
		})
	}
}

// Beginning a turn while the current one is still empty reuses it rather
// than stacking empty turns.
func TestBeginReusesAnEmptyTurn(t *testing.T) {
	ws, _ := newTestWorkspace(t)
	c := NewCheckpoints(ws, 0)
	c.BeginTurn("s", 0, "first")
	c.BeginTurn("s", 2, "second")
	require.NoError(t, ws.WriteFileAtomic(context.Background(), "a.txt", []byte("a")))
	list := c.List()
	require.Len(t, list, 1)
	assert.Equal(t, "second", list[0].Label)
	assert.Equal(t, 2, list[0].Prompt)
	assert.Equal(t, 1, list[0].ID, "the empty turn's ID was not reused")
}

// A change that fails after its snapshot was taken leaves no entry behind.
func TestFailedChangesAreDiscarded(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	c := NewCheckpoints(ws, 0)
	writeFile(t, filepath.Join(dir, "exists.txt"), "x")
	sub := filepath.Join(dir, "ro")
	writeFile(t, filepath.Join(sub, "f.txt"), "f")
	c.Begin("t")

	err := ws.CreateExclusive(context.Background(), "exists.txt", []byte("y"))
	require.ErrorIs(t, err, fs.ErrExist)
	require.NoError(t, os.Chmod(sub, 0o555))
	t.Cleanup(func() { os.Chmod(sub, 0o755) })
	assert.Error(t, ws.RemoveFile(context.Background(), "ro/f.txt"), "removing from a read-only directory")
	assert.Error(t, ws.WriteFileAtomic(context.Background(), "ro/f.txt", []byte("z")), "writing into a read-only directory")
	assert.Empty(t, c.List(), "failed changes were recorded")
	// Discarding what was never recorded is a no-op.
	c.discard("", filepath.Join(dir, "nothing"))
	empty := NewCheckpoints(ws, 0)
	empty.discard("", filepath.Join(dir, "nothing"))
}

// A file over the snapshot limit can't be restored: undo refuses unless
// forced, then restores the rest, and the session diff says so.
func TestCheckpointTooLargeFiles(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorkspace(dir, 8)
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	c := NewCheckpoints(ws, 0)
	writeFile(t, filepath.Join(dir, "big.txt"), "0123456789abc")
	writeFile(t, filepath.Join(dir, "small.txt"), "s0")
	c.Begin("t")
	require.NoError(t, ws.WriteFileAtomic(context.Background(), "big.txt", []byte("tiny")))
	require.NoError(t, ws.WriteFileAtomic(context.Background(), "small.txt", []byte("s1")))

	assert.Contains(t, c.SessionDiff(""), "big.txt: too large to diff")
	_, err = c.Undo(false)
	require.ErrorContains(t, err, "snapshot limit")
	res, err := c.Undo(true)
	require.NoError(t, err)
	assert.Equal(t, []string{"small.txt"}, res.Restored)
	assert.Equal(t, "tiny", readString(t, filepath.Join(dir, "big.txt")))
	assert.Equal(t, "s0", readString(t, filepath.Join(dir, "small.txt")))
}

// A snapshot that went missing shows in the session diff, and a forced
// undo reports the file it couldn't restore while restoring the others.
func TestCheckpointMissingSnapshot(t *testing.T) {
	dir, store := t.TempDir(), filepath.Join(t.TempDir(), "cp")
	writeFile(t, filepath.Join(dir, "a.txt"), "a0")
	writeFile(t, filepath.Join(dir, "b.txt"), "b0")
	ws, c := reopen(t, dir, store)
	c.Begin("t")
	require.NoError(t, ws.WriteFileAtomic(context.Background(), "a.txt", []byte("a1")))
	require.NoError(t, ws.WriteFileAtomic(context.Background(), "b.txt", []byte("b1")))
	require.NoError(t, os.Remove(filepath.Join(store, "blobs", hashOf([]byte("a0")))))

	assert.Contains(t, c.SessionDiff(""), "a.txt: snapshot missing")
	res, err := c.Undo(false)
	require.ErrorContains(t, err, "a.txt: snapshot")
	assert.Equal(t, []string{"b.txt"}, res.Restored)
	assert.Equal(t, "b0", readString(t, filepath.Join(dir, "b.txt")))
	assert.Empty(t, c.List(), "the turn was kept after restoring")
}

// A file that can't be put back is reported; the others still are.
func TestCheckpointRestoreFailure(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	c := NewCheckpoints(ws, 0)
	c.Begin("t")
	require.NoError(t, ws.CreateExclusive(context.Background(), "made.txt", []byte("m")))
	// Replaced by a non-empty directory, which removing can't undo.
	require.NoError(t, os.Remove(filepath.Join(dir, "made.txt")))
	writeFile(t, filepath.Join(dir, "made.txt", "inner"), "i")

	res, err := c.Undo(true)
	assert.ErrorContains(t, err, "made.txt")
	assert.Empty(t, res.Restored)
}

// The same content snapshotted twice is stored once; a damaged blob is
// detected rather than restored.
func TestDiskBlobs(t *testing.T) {
	d := diskBlobs(t.TempDir())
	h1, err := d.put([]byte("same"))
	require.NoError(t, err)
	h2, err := d.put([]byte("same"))
	require.NoError(t, err)
	assert.Equal(t, h1, h2)
	assert.True(t, d.has(h1))

	cases := map[string]string{"short": "abc", "not hex": strings.Repeat("z", 64)}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			assert.False(t, d.has(h))
			_, err := d.get(h)
			assert.ErrorContains(t, err, "bad snapshot hash")
		})
	}

	require.NoError(t, os.WriteFile(filepath.Join(string(d), h1), []byte("changed"), 0o600))
	_, err = d.get(h1)
	assert.ErrorContains(t, err, "damaged")

	// keep on a missing directory does nothing.
	diskBlobs(filepath.Join(string(d), "missing")).keep(nil)
	d.keep(map[string]bool{})
	assert.False(t, d.has(h1), "an unreferenced blob was kept")
}

// The in-memory blobs report what they don't have.
func TestMemBlobs(t *testing.T) {
	b := &memBlobs{m: map[string][]byte{}}
	h, err := b.put([]byte("x"))
	require.NoError(t, err)
	assert.True(t, b.has(h))
	assert.False(t, b.has("nope"))
	_, err = b.get("nope")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// Opening a store fails cleanly when its directory or index can't be read,
// skips null turns, and treats a turn whose snapshot hash is bad as not
// restorable.
func TestOpenCheckpointsProblems(t *testing.T) {
	ws, dir := newTestWorkspace(t)

	t.Run("directory under a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		writeFile(t, file, "x")
		c, err := OpenCheckpoints(ws, CheckpointOptions{Dir: filepath.Join(file, "cp")})
		assert.Error(t, err)
		assert.NotNil(t, c, "an in-memory store is still returned")
	})
	t.Run("index is a directory", func(t *testing.T) {
		store := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(store, "index.json"), 0o700))
		_, err := OpenCheckpoints(ws, CheckpointOptions{Dir: store})
		assert.ErrorContains(t, err, "checkpoints")
	})
	t.Run("null turn and bad hash", func(t *testing.T) {
		store := t.TempDir()
		abs := filepath.Join(dir, "a.txt")
		writeFile(t, abs, "now")
		idx := `{"version":1,"next_id":3,"turns":[null,{"id":3,"prompt":-1,"label":"t","started":"2999-01-01T00:00:00Z","changes":[{"abs":"` + abs + `","display":"a.txt","before":{"exists":true,"hash":"bad","size":1},"after_exists":true,"after_hash":"` + hashOf([]byte("now")) + `"}]}]}`
		writeFile(t, filepath.Join(store, "index.json"), idx)
		c, err := OpenCheckpoints(ws, CheckpointOptions{Dir: store})
		require.NoError(t, err)
		require.Len(t, c.List(), 1)
		_, err = c.Undo(false)
		assert.ErrorContains(t, err, "snapshot limit", "a bad hash is not restorable")
	})
}

// When the store can't be written, checkpoints keep working in memory and
// the failure is logged once; a snapshot that can't be stored marks the
// change as not undoable.
func TestCheckpointStoreNotWritable(t *testing.T) {
	dir, store := t.TempDir(), filepath.Join(t.TempDir(), "cp")
	writeFile(t, filepath.Join(dir, "a.txt"), "a0")
	ws, c := reopen(t, dir, store)
	require.NoError(t, os.Chmod(store, 0o500))
	require.NoError(t, os.Chmod(filepath.Join(store, "blobs"), 0o500))
	t.Cleanup(func() {
		os.Chmod(store, 0o700)
		os.Chmod(filepath.Join(store, "blobs"), 0o700)
	})

	c.Begin("t")
	require.NoError(t, ws.WriteFileAtomic(context.Background(), "a.txt", []byte("a1")))
	require.NoError(t, ws.WriteFileAtomic(context.Background(), "b.txt", []byte("b1")))
	assert.True(t, c.warned, "the failed save wasn't noted")
	list := c.List()
	require.Len(t, list, 1)
	assert.Equal(t, []string{"a.txt", "b.txt"}, list[0].Files)
	_, err := c.Undo(false)
	assert.ErrorContains(t, err, "a.txt", "a change without its snapshot isn't undoable")
}

// An undo that restores some files but not others keeps the changes it
// couldn't undo, so a later undo can.
func TestCheckpointsPartialUndoKeepsTheFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	ws, dir := newTestWorkspace(t)
	cp := NewCheckpoints(ws, 0)
	ro := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(ro, 0o755))
	writeFile(t, filepath.Join(ro, "a.txt"), "a1")
	writeFile(t, filepath.Join(dir, "b.txt"), "b1")
	cp.Begin("t")
	ctx := context.Background()
	require.NoError(t, ws.WriteFileAtomic(ctx, "ro/a.txt", []byte("a2")))
	require.NoError(t, ws.WriteFileAtomic(ctx, "b.txt", []byte("b2")))
	require.NoError(t, os.Chmod(ro, 0o500)) // a.txt can't be replaced
	t.Cleanup(func() { os.Chmod(ro, 0o755) })

	res, err := cp.Undo(false)
	require.Error(t, err)
	assert.Equal(t, []string{"b.txt"}, res.Restored)
	l := cp.List()
	require.Len(t, l, 1, "the turn is kept for what failed")
	assert.Equal(t, []string{filepath.Join("ro", "a.txt")}, l[0].Files)

	require.NoError(t, os.Chmod(ro, 0o755))
	res, err = cp.Undo(false)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join("ro", "a.txt")}, res.Restored)
	assert.Equal(t, "a1", readString(t, filepath.Join(ro, "a.txt")))
	assert.Empty(t, cp.List())
}
