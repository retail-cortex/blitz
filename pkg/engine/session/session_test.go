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
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStorage(t *testing.T) (*Storage, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sessions")
	s, err := NewStorage(dir)
	require.NoError(t, err)
	return s, dir
}

func TestStoragePermissions(t *testing.T) {
	s, dir := newStorage(t)
	rec, err := s.CreateSession("", "t", "blitz")
	require.NoError(t, err)
	require.NoError(t, s.AddMessage("user", "my password is hunter2"))

	info, _ := os.Stat(dir)
	assert.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), "session dir mode = %v, want 0700", info.Mode().Perm())
	for _, suffix := range []string{metaSuffix, messagesSuffix} {
		info, err := os.Stat(filepath.Join(dir, rec.ID+suffix))
		require.NoError(t, err)
		assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm(), "%s mode = %v, want 0600", suffix, info.Mode().Perm())
	}
}

func TestStorageTightensExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "old")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	_, err := NewStorage(dir)
	require.NoError(t, err)
	info, _ := os.Stat(dir)
	assert.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), "existing dir not tightened: %v", info.Mode().Perm())
}

func TestSessionIDValidation(t *testing.T) {
	s, dir := newStorage(t)
	outside := filepath.Join(filepath.Dir(dir), "stolen.json")
	_ = os.WriteFile(outside, []byte(`{"id":"x"}`), 0o600)

	// Negative: traversal and unsafe IDs rejected for both create and load.
	for _, id := range []string{"../stolen", "a/b", ".hidden", "..", "x y", strings.Repeat("a", 200)} {
		_, err := s.CreateSession(id, "t", "a")
		assert.ErrorIs(t, err, ErrInvalidID, "CreateSession(%q) expected ErrInvalidID, got %v", id, err)
		_, err = s.Load(id)
		assert.ErrorIs(t, err, ErrInvalidID, "Load(%q) expected ErrInvalidID, got %v", id, err)
	}
	// Positive: well-formed IDs accepted.
	for _, id := range []string{"session-1", "abc.DEF_9"} {
		_, err := s.CreateSession(id, "t", "a")
		assert.NoError(t, err, "CreateSession(%q)", id)
	}
}

func TestNewSessionIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewSessionID()
		require.NoError(t, ValidateID(id), "generated invalid id %q", id)
		require.False(t, seen[id], "duplicate id %q", id)
		seen[id] = true
	}
	// Default-ID sessions created in the same second no longer collide.
	s, _ := newStorage(t)
	a, _ := s.CreateSession("", "a", "x")
	b, _ := s.CreateSession("", "b", "x")
	assert.NotEqual(t, b.ID, a.ID, "sessions created back-to-back share ID %q", a.ID)
}

func TestAddMessageAppendsAndLoadRoundTrip(t *testing.T) {
	s, dir := newStorage(t)

	// Negative: no active session.
	assert.Error(t, s.AddMessage("user", "hi"), "expected error without active session")

	rec, _ := s.CreateSession("", "Chat", "helios")
	for i := 0; i < 3; i++ {
		require.NoError(t, s.AddMessage("user", "msg"))
	}
	// Messages file is append-only JSONL: one line per message.
	data, _ := os.ReadFile(filepath.Join(dir, rec.ID+messagesSuffix))
	n := strings.Count(string(data), "\n")
	assert.Equal(t, 3, n, "expected 3 JSONL lines, got %d", n)
	// Metadata stays small and does not embed messages.
	meta, _ := os.ReadFile(filepath.Join(dir, rec.ID+metaSuffix))
	assert.NotContains(t, string(meta), `"messages"`, "metadata file should not contain messages")

	s2, _ := NewStorage(dir)
	loaded, err := s2.Load(rec.ID)
	require.NoError(t, err)
	assert.Len(t, loaded.Messages, 3, "unexpected loaded session %+v", loaded)
	assert.Equal(t, 3, loaded.MessageCount, "unexpected loaded session %+v", loaded)
	assert.Equal(t, "helios", loaded.Agent, "unexpected loaded session %+v", loaded)
	assert.Equal(t, rec.ID, s2.Active().ID, "Load should make the session active")

	// Negative: unknown session.
	_, err = s2.Load("nope")
	assert.Error(t, err, "expected not found, got")
	assert.Contains(t, err.Error(), "not found", "expected not found, got %v", err)
}

func TestLoadToleratesTornLine(t *testing.T) {
	s, dir := newStorage(t)
	rec, _ := s.CreateSession("", "t", "a")
	_ = s.AddMessage("user", "complete")
	f, _ := os.OpenFile(filepath.Join(dir, rec.ID+messagesSuffix), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"role":"model","content":"trunc`)
	f.Close()

	loaded, err := s.Load(rec.ID)
	require.NoError(t, err)
	assert.Len(t, loaded.Messages, 1, "expected torn line skipped, got %+v", loaded.Messages)
	assert.Equal(t, "complete", loaded.Messages[0].Content, "expected torn line skipped, got %+v", loaded.Messages)
}

func TestListMetadataAndLegacy(t *testing.T) {
	s, dir := newStorage(t)
	old, _ := s.CreateSession("", "old", "a")
	_ = s.AddMessage("user", "1")
	time.Sleep(10 * time.Millisecond)
	newer, _ := s.CreateSession("", "new", "a")
	_ = s.AddMessage("user", "1")
	_ = s.AddMessage("user", "2")

	legacy := SessionRecord{ID: "legacy-1", Title: "legacy", UpdatedAt: time.Unix(0, 0), Messages: []Message{{Role: "user", Content: "x"}}}
	b, _ := json.Marshal(legacy)
	_ = os.WriteFile(filepath.Join(dir, "legacy-1.json"), b, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "garbage.meta.json"), []byte("{not json"), 0o600)

	list, err := s.List()
	require.NoError(t, err)
	require.Len(t, list, 3, "expected 3 sessions (corrupt skipped), got %d", len(list))
	assert.Equal(t, newer.ID, list[0].ID, "unexpected order: %s, %s, %s", list[0].ID, list[1].ID, list[2].ID)
	assert.Equal(t, old.ID, list[1].ID, "unexpected order: %s, %s, %s", list[0].ID, list[1].ID, list[2].ID)
	assert.Equal(t, "legacy-1", list[2].ID, "unexpected order: %s, %s, %s", list[0].ID, list[1].ID, list[2].ID)
	assert.Equal(t, 2, list[0].MessageCount, "unexpected message counts %d, %d", list[0].MessageCount, list[2].MessageCount)
	assert.Equal(t, 1, list[2].MessageCount, "unexpected message counts %d, %d", list[0].MessageCount, list[2].MessageCount)
	for _, r := range list {
		assert.Len(t, r.Messages, 0, "List should not load message bodies for %s", r.ID)
	}

	// Legacy sessions still load.
	rec, err := s.Load("legacy-1")
	assert.NoError(t, err, "legacy load failed: %v %+v", err, rec)
	assert.Len(t, rec.Messages, 1, "legacy load failed: %v %+v", err, rec)
}

func TestActiveReturnsSnapshot(t *testing.T) {
	s, _ := newStorage(t)
	assert.Nil(t, s.Active(), "expected nil active session initially")
	_, _ = s.CreateSession("", "t", "a")
	snap := s.Active()
	snap.Title = "mutated"
	_ = s.AddMessage("user", "x")
	assert.NotEqual(t, "mutated", s.Active().Title, "Active should return an independent copy")
	assert.Len(t, snap.Messages, 0, "Active should return an independent copy")
}

func TestWorkspaceScopedSessions(t *testing.T) {
	s, dir := newStorage(t)
	s.SetWorkspace("/work/a")
	a1, _ := s.CreateSession("", "a1", "x")
	time.Sleep(5 * time.Millisecond)
	s.SetWorkspace("/work/b")
	b1, _ := s.CreateSession("", "b1", "x")
	time.Sleep(5 * time.Millisecond)
	s.SetWorkspace("/work/a")
	a2, _ := s.CreateSession("", "a2", "x")

	assert.Equal(t, "/work/a", a1.Workspace, "workspace not recorded: %q %q", a1.Workspace, b1.Workspace)
	assert.Equal(t, "/work/b", b1.Workspace, "workspace not recorded: %q %q", a1.Workspace, b1.Workspace)
	listA, _ := s.ListWorkspace("/work/a")
	assert.Len(t, listA, 2, "workspace a sessions: %+v", listA)
	assert.Equal(t, a2.ID, listA[0].ID, "workspace a sessions: %+v", listA)
	assert.Equal(t, a1.ID, listA[1].ID, "workspace a sessions: %+v", listA)
	listB, _ := s.ListWorkspace("/work/b")
	assert.Len(t, listB, 1, "workspace b sessions: %+v", listB)
	assert.Equal(t, b1.ID, listB[0].ID, "workspace b sessions: %+v", listB)
	all, _ := s.List()
	assert.Len(t, all, 3, "List should include every workspace, got %d", len(all))
	none, _ := s.ListWorkspace("/work/c")
	assert.Len(t, none, 0, "unknown workspace should be empty: %+v", none)

	// Workspace survives restarts (it's in the metadata file).
	s2, _ := NewStorage(dir)
	l, _ := s2.ListWorkspace("/work/b")
	assert.Len(t, l, 1, "workspace not persisted")
}

func TestLegacySessionAdoptedOnResume(t *testing.T) {
	s, dir := newStorage(t)
	legacy, _ := s.CreateSession("", "old", "x") // no workspace set: legacy record
	require.Equal(t, "", legacy.Workspace, "expected legacy session without workspace")
	s2, _ := NewStorage(dir)
	s2.SetWorkspace("/work/a")
	l, _ := s2.ListWorkspace("/work/a")
	assert.Len(t, l, 0, "legacy sessions must not appear in a workspace listing before being resumed")
	rec, err := s2.Load(legacy.ID)
	require.NoError(t, err, "legacy session not adopted: %+v", rec)
	require.Equal(t, "/work/a", rec.Workspace, "legacy session not adopted: %+v %v", rec, err)
	s3, _ := NewStorage(dir)
	l, _ = s3.ListWorkspace("/work/a")
	assert.Len(t, l, 1, "adoption not persisted")
	// Sessions that already have a workspace keep it when loaded elsewhere.
	s3.SetWorkspace("/work/b")
	rec, _ = s3.Load(legacy.ID)
	assert.Equal(t, "/work/a", rec.Workspace, "owned session re-assigned to %q", rec.Workspace)
}

func TestLastTurnPersistsForActiveAndOtherSessions(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStorage(dir)
	require.NoError(t, err)
	a, _ := s.CreateSession("a", "A", "blitz")
	b, _ := s.CreateSession("b", "B", "blitz") // b is now active

	tp, n := s.LastTurn(a.ID)
	require.Equal(t, "", tp, "new session has a last turn: %q %d", tp, n)
	require.Equal(t, 0, n, "new session has a last turn: %q %d", tp, n)
	const tpA, tpB = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	require.NoError(t, s.SetLastTurn(a.ID, tpA, 3))
	require.NoError(t, s.SetLastTurn(b.ID, tpB, 1))
	require.NoError(t, s.AddMessage("user", "hi"))

	s2, _ := NewStorage(dir) // a later process, e.g. --resume
	for id, want := range map[string]string{a.ID: tpA, b.ID: tpB} {
		tp, _ := s2.LastTurn(id)
		assert.Equal(t, want, tp, "%s: LastTurn = %q, want %q", id, tp, want)
	}
	_, n = s2.LastTurn(a.ID)
	assert.Equal(t, 3, n, "index = %d, want 3", n)
	tp, _ = s2.LastTurn("../escape")
	assert.Equal(t, "", tp, "invalid id accepted")
	assert.Error(t, s2.SetLastTurn("missing", tpA, 1), "SetLastTurn on a missing session should fail")
}

func TestMessageKindsAndTruncate(t *testing.T) {
	s, _ := newStorage(t)
	rec, err := s.CreateSession("", "", "blitz")
	require.NoError(t, err)
	three := 3
	for _, m := range []Message{
		{Role: "user", Content: "first", Events: new(int)},
		{Role: "model", Content: "reply"},
		{Role: "user", Content: "mid-turn", Kind: KindSteer},
		{Role: "user", Content: "second", Events: &three},
		{Role: "model", Content: "reply 2"},
	} {
		require.NoError(t, s.Append(m))
	}
	got, err := s.Load(rec.ID)
	require.NoError(t, err)
	var prompts []string
	for _, m := range got.Messages {
		if m.IsPrompt() {
			prompts = append(prompts, m.Content)
		}
	}
	require.Equal(t, "first,second", strings.Join(prompts, ","), "round trip: %+v", got.Messages)
	require.NotNil(t, got.Messages[3].Events, "round trip: %+v", got.Messages)
	require.Equal(t, 3, *got.Messages[3].Events, "round trip: %+v", got.Messages)
	require.Equal(t, KindSteer, got.Messages[2].Kind, "round trip: %+v", got.Messages)

	require.NoError(t, s.Truncate(3))
	assert.Error(t, s.Truncate(9), "truncating past the end")
	got, _ = s.Load(rec.ID)
	require.Equal(t, 3, got.MessageCount, "after truncating: %d %+v", got.MessageCount, got.Messages)
	require.Len(t, got.Messages, 3, "after truncating: %d %+v", got.MessageCount, got.Messages)
	require.Equal(t, "mid-turn", got.Messages[2].Content, "after truncating: %d %+v", got.MessageCount, got.Messages)
	require.NoError(t, s.AddMessage("user", "third"))
	got, _ = s.Load(rec.ID)
	assert.Equal(t, 4, got.MessageCount, "appending after truncating")
	if assert.Len(t, got.Messages, 4, "appending after truncating") {
	assert.Equal(t, "third", got.Messages[3].Content)
	}
}
