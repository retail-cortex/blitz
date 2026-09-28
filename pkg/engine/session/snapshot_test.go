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
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adksession "google.golang.org/adk/v2/session"
)

// conversation creates an active session with two messages and a model
// event log (as the engine writes it) in the storage directory.
func conversation(t *testing.T) (*Storage, *PersistentService, *SessionRecord, string) {
	t.Helper()
	st, dir := newStorage(t)
	st.SetWorkspace("/work")
	svc, err := NewPersistentService(dir)
	require.NoError(t, err)
	rec, err := st.CreateSession("", "refactor", "blitz")
	require.NoError(t, err)
	st.AddMessage("user", "remember pineapple")
	st.AddMessage("model", "noted")
	created, err := svc.Create(context.Background(), &adksession.CreateRequest{AppName: "app", UserID: "u", SessionID: rec.ID})
	require.NoError(t, err)
	appendText(t, svc, created.Session, "user", "user", "remember pineapple", false)
	appendText(t, svc, created.Session, "agent", "model", "noted", false)
	return st, svc, rec, dir
}

func TestSnapshotCopiesTheSessionAndLeavesItActive(t *testing.T) {
	st, _, src, dir := conversation(t)
	snap, err := st.Snapshot(src.ID, "before-refactor", false)
	require.NoError(t, err)
	require.NotEqual(t, src.ID, snap.ID, "snapshot = %+v", snap)
	require.Equal(t, "before-refactor", snap.Name, "snapshot = %+v", snap)
	require.Equal(t, src.ID, snap.From, "snapshot = %+v", snap)
	require.Equal(t, "/work", snap.Workspace, "snapshot = %+v", snap)
	require.Equal(t, 2, snap.MessageCount, "snapshot = %+v", snap)
	a := st.Active()
	require.Equal(t, src.ID, a.ID, "active changed to %s", a.ID)
	for _, suffix := range []string{metaSuffix, messagesSuffix, eventsSuffix} {
		info, err := os.Stat(filepath.Join(dir, snap.ID+suffix))
		require.NoError(t, err, "%s: %v %v", suffix, err, info)
		require.Equal(t, fs.FileMode(filePerm), info.Mode().Perm(), "%s: %v %v", suffix, err, info)
	}
	// The source keeps growing on its own.
	st.AddMessage("user", "more")
	found, err := st.FindName("before-refactor")
	require.NoError(t, err, "FindName = %+v,", found)
	require.NotNil(t, found, "FindName = %+v, %v", found, err)
	require.Equal(t, snap.ID, found.ID, "FindName = %+v, %v", found, err)
	require.Equal(t, 2, found.MessageCount, "FindName = %+v, %v", found, err)
}

func TestSnapshotNamesAreUniqueUnlessReplaced(t *testing.T) {
	st, _, src, dir := conversation(t)
	first, err := st.Snapshot(src.ID, "v1", false)
	require.NoError(t, err)
	_, dupErr := st.Snapshot(src.ID, "v1", false)
	require.ErrorIs(t, dupErr, api.ErrSnapshotNameTaken, "a duplicate name")
	st.AddMessage("user", "third")
	second, err := st.Snapshot(src.ID, "v1", true)
	require.NoError(t, err)
	found, _ := st.FindName("v1")
	require.NotNil(t, found, "after replace: %+v", found)
	require.Equal(t, second.ID, found.ID, "after replace: %+v", found)
	require.Equal(t, 3, found.MessageCount, "after replace: %+v", found)
	for _, suffix := range []string{metaSuffix, messagesSuffix, eventsSuffix} {
		_, err := os.Stat(filepath.Join(dir, first.ID+suffix))
		require.ErrorIs(t, err, os.ErrNotExist, "replaced snapshot's %s still there: %v", suffix, err)
	}
}

func TestSnapshotRefusesBadNamesAndEmptySessions(t *testing.T) {
	st, _, src, _ := conversation(t)
	for _, name := range []string{"", "../x", "a b", "session-20260101-000000-abcd", ".hidden"} {
		_, err := st.Snapshot(src.ID, name, false)
		assert.Error(t, err, "name %q accepted", name)
	}
	empty, _ := st.CreateSession("", "empty", "a")
	_, err := st.Snapshot(empty.ID, "nothing", false)
	assert.Error(t, err, "saved an empty session")
	_, err = st.Snapshot("no-such-session", "x", false)
	assert.Error(t, err, "saved a missing session")
}

// Opening a snapshot by name starts a new session with its history, for the
// transcript and for the model (the event log replays); the snapshot itself
// doesn't change when the new session continues.
func TestOpenByNameBranchesAndKeepsTheSnapshot(t *testing.T) {
	st, svc, src, _ := conversation(t)
	snap, err := st.Snapshot(src.ID, "checkpoint", false)
	require.NoError(t, err)
	st.SetWorkspace("/other")

	rec, branched, err := st.Open("checkpoint")
	require.NoError(t, err, "Open: %v, branched %v", err, branched)
	require.True(t, branched, "Open: %v, branched %v", err, branched)
	require.NotEqual(t, snap.ID, rec.ID, "branch = %+v", rec)
	require.NotEqual(t, src.ID, rec.ID, "branch = %+v", rec)
	require.Equal(t, "", rec.Name, "branch = %+v", rec)
	require.Equal(t, snap.ID, rec.From, "branch = %+v", rec)
	require.Equal(t, "/other", rec.Workspace, "branch = %+v", rec)
	require.Equal(t, rec.ID, st.Active().ID, "active %s, messages %+v", st.Active().ID, rec.Messages)
	require.Len(t, rec.Messages, 2, "active %s, messages %+v", st.Active().ID, rec.Messages)
	require.Equal(t, "remember pineapple", rec.Messages[0].Content, "active %s, messages %+v", st.Active().ID, rec.Messages)
	// A new process's session service replays the copied event log.
	fresh, err := NewPersistentService(st.dir)
	require.NoError(t, err)
	got := eventTexts(t, fresh, rec.ID)
	require.Equal(t, []string{"remember pineapple", "noted"}, got, "replayed events = %v", got)

	// Continue the branch: the snapshot keeps its two messages and events.
	st.AddMessage("user", "new direction")
	resp, _ := svc.Get(context.Background(), &adksession.GetRequest{AppName: "app", UserID: "u", SessionID: rec.ID})
	appendText(t, svc, resp.Session, "user", "user", "new direction", false)
	got = eventTexts(t, fresh, snap.ID)
	require.Len(t, got, 2, "snapshot events changed: %v", got)
	again, _ := st.FindName("checkpoint")
	require.Equal(t, 2, again.MessageCount, "snapshot messages changed: %d", again.MessageCount)

	// Opening it again starts another branch from the same point.
	second, branched, err := st.Open("checkpoint")
	require.NoError(t, err, "second open: %+v %v", second, branched)
	require.True(t, branched, "second open: %+v %v %v", second, branched, err)
	require.NotEqual(t, rec.ID, second.ID, "second open: %+v %v %v", second, branched, err)
	require.Len(t, second.Messages, 2, "second open: %+v %v %v", second, branched, err)
}

// A snapshot opened by its ID is copied too, never continued in place.
func TestOpenASnapshotByIDBranches(t *testing.T) {
	st, _, src, _ := conversation(t)
	snap, _ := st.Snapshot(src.ID, "fixed", false)
	rec, branched, err := st.Open(snap.ID)
	require.NoError(t, err, "Open(snapshot id) = %+v %v", rec, branched)
	require.True(t, branched, "Open(snapshot id) = %+v %v %v", rec, branched, err)
	require.NotEqual(t, snap.ID, rec.ID, "Open(snapshot id) = %+v %v %v", rec, branched, err)
	require.Equal(t, snap.ID, rec.From, "Open(snapshot id) = %+v %v %v", rec, branched, err)
}

func TestOpenByIDResumesInPlace(t *testing.T) {
	st, _, src, _ := conversation(t)
	st.CreateSession("", "other", "a")
	rec, branched, err := st.Open(src.ID)
	require.NoError(t, err, "Open(id) = %+v %v", rec, branched)
	require.False(t, branched, "Open(id) = %+v %v %v", rec, branched, err)
	require.Equal(t, src.ID, rec.ID, "Open(id) = %+v %v %v", rec, branched, err)
	_, _, err = st.Open("no-such-name")
	require.Error(t, err, "opened a missing name")
}

// Sessions in the single-file format of earlier versions can be saved too.
func TestSnapshotOfALegacySession(t *testing.T) {
	st, dir := newStorage(t)
	legacy := SessionRecord{ID: "legacy-1", Title: "legacy", UpdatedAt: time.Unix(0, 0), Messages: []Message{{Role: "user", Content: "x"}}}
	b, _ := json.Marshal(legacy)
	os.WriteFile(filepath.Join(dir, "legacy-1.json"), b, 0o600)
	snap, err := st.Snapshot("legacy-1", "old-one", false)
	require.NoError(t, err)
	rec, branched, err := st.Open("old-one")
	require.NoError(t, err, "%+v %v", rec, branched)
	require.True(t, branched, "%+v %v %v", rec, branched, err)
	require.Len(t, rec.Messages, 1, "%+v %v %v", rec, branched, err)
	require.Equal(t, 1, snap.MessageCount, "%+v %v %v", rec, branched, err)
}
