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

package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readOnly makes dir read-only for the rest of the test, skipping it when
// file modes aren't enforced (running as root).
func readOnly(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, dirPerm) })
	if f, err := os.CreateTemp(dir, "probe-*"); err == nil {
		f.Close()
		os.Remove(f.Name())
		t.Skip("running with privileges that ignore file modes")
	}
}

// savedSession creates a session in s with one message, so it is on disk.
func savedSession(t *testing.T, s *Storage) *SessionRecord {
	t.Helper()
	rec, err := s.CreateSession("", "", "blitz")
	require.NoError(t, err)
	require.NoError(t, s.AddMessage("user", "hello"))
	return rec
}

// TestNewStorageResolvesHome checks that "" and "~/…" are under the home
// directory, and that a missing home or an unusable path is an error.
func TestNewStorageResolvesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, err := NewStorage("")
	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(home, ".blitz", "sessions"))
	assert.Equal(t, filepath.Join(home, ".blitz", "sessions"), s.dir)
	s, err = NewStorage("~/elsewhere")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "elsewhere"), s.dir)

	file := filepath.Join(home, "file")
	require.NoError(t, os.WriteFile(file, nil, filePerm))
	_, err = NewStorage(filepath.Join(file, "sub"))
	assert.Error(t, err, "a directory under a file")

	t.Setenv("HOME", "")
	_, err = NewStorage("~")
	assert.ErrorContains(t, err, "home directory")
}

// TestStorageInvalidIDsAndMissingState checks the answers for invalid IDs
// and for calls that need an active session.
func TestStorageInvalidIDsAndMissingState(t *testing.T) {
	s, _ := newStorage(t)
	s.SetWorkspace("/ws")
	assert.Equal(t, "/ws", s.Workspace())

	tp, idx := s.LastTurn("../x")
	assert.Empty(t, tp)
	assert.Zero(t, idx)
	assert.Error(t, s.SetLastTurn("../x", "tp", 1))
	_, ok := s.Usage("../x")
	assert.False(t, ok)
	assert.Empty(t, s.TranscriptPath("../x"))
	assert.Equal(t, filepath.Join(s.dir, "abc"+messagesSuffix), s.TranscriptPath("abc"))
	assert.ErrorContains(t, s.Rename("x"), "no active session")
	assert.ErrorContains(t, s.Truncate(0), "no active session")
}

// TestStorageCorruptFiles checks that corrupt metadata and legacy files
// are reported, and listing skips them.
func TestStorageCorruptFiles(t *testing.T) {
	s, dir := newStorage(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad"+metaSuffix), []byte("{"), filePerm))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "old"+legacySuffix), []byte("{"), filePerm))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "in valid"+legacySuffix), []byte("{}"), filePerm))

	_, err := s.Load("bad")
	assert.ErrorContains(t, err, "corrupt session metadata")
	_, err = s.Load("old")
	assert.ErrorContains(t, err, "corrupt session file")
	assert.Error(t, s.AppendTo("bad", Message{Role: "user", Content: "x"}))
	assert.Error(t, s.SetUsage("bad", api.Usage{}))
	list, err := s.List()
	require.NoError(t, err)
	assert.Empty(t, list)
}

// TestStorageUnreadableTranscript checks that a transcript that can't be
// read or written fails the load or the append.
func TestStorageUnreadableTranscript(t *testing.T) {
	s, dir := newStorage(t)
	rec := savedSession(t, s)
	path := filepath.Join(dir, rec.ID+messagesSuffix)
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, filePerm) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("running with privileges that ignore file modes")
	}
	other, err := NewStorage(dir)
	require.NoError(t, err)
	_, err = other.Load(rec.ID)
	assert.Error(t, err, "the transcript can't be read")
	assert.Error(t, s.AddMessage("user", "again"), "the transcript can't be written")
}

// TestStorageReadOnlyDirectory checks the writes that fail when the
// session directory is read-only, leaving what's in memory unchanged.
func TestStorageReadOnlyDirectory(t *testing.T) {
	s, dir := newStorage(t)
	s.SetWorkspace("/a")
	rec := savedSession(t, s)
	require.NoError(t, s.AddMessage("model", "hi"))

	plain, err := NewStorage(dir) // no workspace: its sessions are adopted on load
	require.NoError(t, err)
	orphan := savedSession(t, plain)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty"+metaSuffix), []byte(`{"id":"empty"}`), filePerm))

	b, err := NewStorage(dir) // before the directory is read-only: NewStorage resets its mode
	require.NoError(t, err)
	b.SetWorkspace("/b")
	readOnly(t, dir)

	assert.Error(t, s.Rename("new name"))
	assert.NotEqual(t, "new name", s.Active().Title, "the title is restored")
	assert.Error(t, s.Truncate(1))
	assert.Len(t, s.Active().Messages, 2, "messages are kept when the file can't be replaced")

	_, err = b.Move(rec.ID)
	assert.Error(t, err, "the move can't be saved")
	assert.Nil(t, b.Active(), "made active though not moved")
	_, err = b.Load(orphan.ID)
	assert.Error(t, err, "the adoption can't be saved")

	_, err = plain.RemoveEmpty()
	assert.Error(t, err, "the empty chat can't be removed")
}

// TestStorageMissingDirectory checks listing when the directory is gone.
func TestStorageMissingDirectory(t *testing.T) {
	s, dir := newStorage(t)
	require.NoError(t, os.RemoveAll(dir))
	_, err := s.List()
	assert.Error(t, err)
	_, err = s.ListWorkspace("/ws")
	assert.Error(t, err)
	_, err = s.RemoveEmpty()
	assert.Error(t, err)
}

// TestSnapshotAndBranchFailures checks snapshots and branches of sessions
// that are missing, empty, or whose files can't be copied, and that a
// failed copy leaves nothing behind.
func TestSnapshotAndBranchFailures(t *testing.T) {
	st, _, rec, dir := conversation(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "blank"+metaSuffix), []byte(`{"id":"blank"}`), filePerm))
	_, err := st.Snapshot("blank", "snap-blank", false)
	assert.ErrorIs(t, err, errNothingToSave, "metadata without messages or events")

	_, err = st.Fork("missing")
	assert.ErrorContains(t, err, "not found")
	_, _, err = st.Open("not a ref")
	assert.Error(t, err, "neither a name nor an ID")

	// An event log that's a directory can't be copied.
	events := filepath.Join(dir, rec.ID+eventsSuffix)
	require.NoError(t, os.Rename(events, events+".bak"))
	require.NoError(t, os.Mkdir(events, dirPerm))
	before, err := os.ReadDir(dir)
	require.NoError(t, err)
	_, err = st.Snapshot(rec.ID, "snap", false)
	assert.Error(t, err, "the event log can't be copied")
	_, err = st.Fork(rec.ID)
	assert.Error(t, err, "the event log can't be copied")
	after, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, after, len(before), "a failed copy is removed")
	require.NoError(t, os.Remove(events))
	require.NoError(t, os.Rename(events+".bak", events))

	// An event log that can't be opened.
	require.NoError(t, os.Chmod(events, 0o000))
	if _, err := os.ReadFile(events); err == nil {
		t.Skip("running with privileges that ignore file modes")
	}
	_, err = st.Snapshot(rec.ID, "snap", false)
	assert.Error(t, err, "the event log can't be opened")
	require.NoError(t, os.Chmod(events, filePerm))

	// A read-only directory takes no copies.
	readOnly(t, dir)
	_, err = st.Snapshot(rec.ID, "snap", false)
	assert.Error(t, err)
	_, err = st.Fork(rec.ID)
	assert.Error(t, err)
}

// TestSnapshotLookupsFailWithoutDirectory checks that snapshot lookups
// report a directory that can't be listed.
func TestSnapshotLookupsFailWithoutDirectory(t *testing.T) {
	st, dir := newStorage(t)
	require.NoError(t, os.RemoveAll(dir))
	_, err := st.FindName("snap")
	assert.Error(t, err)
	_, err = st.Snapshot("session-x", "snap", false)
	assert.Error(t, err)
	_, _, err = st.Open("snap")
	assert.Error(t, err)
}
