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
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func appendText(t *testing.T, svc adksession.Service, s adksession.Session, author, role, text string, partial bool) {
	t.Helper()
	ev := adksession.NewEvent(context.Background(), "inv-1")
	ev.Author = author
	ev.LLMResponse = model.LLMResponse{Content: genai.NewContentFromText(text, genai.Role(role)), Partial: partial}
	require.NoError(t, svc.AppendEvent(context.Background(), s, ev))
}

func eventTexts(t *testing.T, svc adksession.Service, id string) []string {
	t.Helper()
	resp, err := svc.Get(context.Background(), &adksession.GetRequest{AppName: "app", UserID: "u", SessionID: id})
	require.NoError(t, err, "Get(%s)", id)
	var out []string
	for ev := range resp.Session.Events().All() {
		if ev.Content != nil {
			for _, p := range ev.Content.Parts {
				if p.Text != "" {
					out = append(out, p.Text)
				}
				if p.FunctionCall != nil {
					out = append(out, "call:"+p.FunctionCall.Name)
				}
			}
		}
	}
	return out
}

func TestPersistentServiceResume(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	svc, err := NewPersistentService(dir)
	require.NoError(t, err)
	created, err := svc.Create(ctx, &adksession.CreateRequest{AppName: "app", UserID: "u", SessionID: "sess-1"})
	require.NoError(t, err)
	s := created.Session
	appendText(t, svc, s, "user", "user", "remember pineapple", false)
	appendText(t, svc, s, "agent", "model", "streaming chunk", true) // partial: not persisted
	call := adksession.NewEvent(context.Background(), "inv-1")
	call.Author = "agent"
	call.LLMResponse = model.LLMResponse{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "read_file", Args: map[string]any{"path": "x"}}}}}}
	require.NoError(t, svc.AppendEvent(ctx, s, call))
	appendText(t, svc, s, "agent", "model", "noted", false)

	info, err := os.Stat(filepath.Join(dir, "sess-1"+eventsSuffix))
	require.NoError(t, err, "event log missing or wrong mode: %v", info)
	require.Equal(t, fs.FileMode(filePerm), info.Mode().Perm(), "event log missing or wrong mode: %v %v", info, err)

	// A new process (new service, same dir) sees the full history.
	svc2, _ := NewPersistentService(dir)
	got := eventTexts(t, svc2, "sess-1")
	want := []string{"remember pineapple", "call:read_file", "noted"}
	require.Len(t, got, len(want), "replayed %v, want %v", got, want)
	for i := range want {
		assert.Equal(t, want[i], got[i], "event %d = %q, want %q", i, got[i], want[i])
	}

	// Create with a previously used ID also replays (runner auto-create path).
	svc3, _ := NewPersistentService(dir)
	c3, err := svc3.Create(ctx, &adksession.CreateRequest{AppName: "app", UserID: "u", SessionID: "sess-1"})
	assert.NoError(t, err, "create-replay: %v len=%d", err, c3.Session.Events().Len())
	assert.Equal(t, 3, c3.Session.Events().Len(), "create-replay: %v len=%d", err, c3.Session.Events().Len())

	// Delete removes the stored log.
	require.NoError(t, svc2.Delete(ctx, &adksession.DeleteRequest{AppName: "app", UserID: "u", SessionID: "sess-1"}))
	assert.False(t, svc2.HasEvents("sess-1"), "event log not deleted")
}

func TestPersistentServiceEdgeCases(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	svc, _ := NewPersistentService(dir)

	// Unknown sessions are still not found.
	_, err := svc.Get(ctx, &adksession.GetRequest{AppName: "app", UserID: "u", SessionID: "missing"})
	assert.Error(t, err, "expected not found")
	// Unsafe IDs are never used as file names.
	assert.False(t, svc.HasEvents("../escape"), "HasEvents accepted traversal id")
	// A torn trailing line is skipped.
	os.WriteFile(filepath.Join(dir, "torn"+eventsSuffix), []byte(`{"id":"e1","author":"user","content":{"role":"user","parts":[{"text":"ok"}]}}`+"\n"+`{"id":"e2","auth`), filePerm)
	got := eventTexts(t, svc, "torn")
	assert.Len(t, got, 1, "torn log replay: %v", got)
	assert.Equal(t, "ok", got[0], "torn log replay: %v", got)
}

// TestPersistentServiceTruncate checks that Truncate keeps the first
// events on disk and in memory, skipping lines replay would skip, and
// refuses to keep more events than there are.
func TestPersistentServiceTruncate(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	svc, err := NewPersistentService(dir)
	require.NoError(t, err)
	created, err := svc.Create(ctx, &adksession.CreateRequest{AppName: "app", UserID: "u", SessionID: "s1"})
	require.NoError(t, err)
	for _, text := range []string{"one", "two", "three"} {
		appendText(t, svc, created.Session, "user", "user", text, false)
	}
	path := filepath.Join(dir, "s1"+eventsSuffix)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, filePerm)
	require.NoError(t, err)
	_, err = f.WriteString("\nnot json\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	assert.ErrorContains(t, svc.Truncate(ctx, "app", "u", "s1", 4), "fewer than 4")
	require.NoError(t, svc.Truncate(ctx, "app", "u", "s1", 2))
	assert.Equal(t, []string{"one", "two"}, eventTexts(t, svc, "s1"), "in memory")
	svc2, err := NewPersistentService(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"one", "two"}, eventTexts(t, svc2, "s1"), "on disk")

	require.NoError(t, svc.Truncate(ctx, "app", "u", "new", 0), "a session with no log yet")
	assert.Error(t, svc.Truncate(ctx, "app", "u", "../x", 0), "an unsafe ID")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "d"+eventsSuffix), dirPerm))
	assert.Error(t, svc.Truncate(ctx, "app", "u", "d", 0), "a log that can't be read")
}

// TestPersistentServiceErrors checks the service's failures: a directory
// that can't be created, unsafe IDs, logs that can't be read or written.
func TestPersistentServiceErrors(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	file := filepath.Join(base, "file")
	require.NoError(t, os.WriteFile(file, nil, filePerm))
	_, err := NewPersistentService(filepath.Join(file, "sub"))
	assert.Error(t, err, "the directory can't be created under a file")

	dir := t.TempDir()
	svc, err := NewPersistentService(dir)
	require.NoError(t, err)

	created, err := svc.Create(ctx, &adksession.CreateRequest{AppName: "app", UserID: "u", SessionID: "s1"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, &adksession.CreateRequest{AppName: "app", UserID: "u", SessionID: "s1"})
	assert.Error(t, err, "the ID is in use")

	list, err := svc.List(ctx, &adksession.ListRequest{AppName: "app", UserID: "u"})
	require.NoError(t, err)
	assert.Len(t, list.Sessions, 1)

	require.NoError(t, os.Mkdir(filepath.Join(dir, "s1"+eventsSuffix), dirPerm))
	ev := adksession.NewEvent(ctx, "inv-1")
	ev.Author = "user"
	assert.Error(t, svc.AppendEvent(ctx, created.Session, ev), "the log can't be opened")

	require.NoError(t, os.Mkdir(filepath.Join(dir, "s2"+eventsSuffix), dirPerm))
	_, err = svc.Create(ctx, &adksession.CreateRequest{AppName: "app", UserID: "u", SessionID: "s2"})
	assert.Error(t, err, "the log can't be replayed")
	_, err = svc.Get(ctx, &adksession.GetRequest{AppName: "app", UserID: "u", SessionID: "s2"})
	assert.Error(t, err, "a failed Create leaves no session without its history")
	fresh, err := NewPersistentService(dir)
	require.NoError(t, err)
	for i := range 2 {
		_, err = fresh.Get(ctx, &adksession.GetRequest{AppName: "app", UserID: "u", SessionID: "s2"})
		assert.Error(t, err, "Get %d: the log can't be replayed", i)
	}

	bad, err := svc.Create(ctx, &adksession.CreateRequest{AppName: "app", UserID: "u", SessionID: "../bad"})
	if err == nil {
		assert.Error(t, svc.AppendEvent(ctx, bad.Session, ev), "an unsafe ID isn't a file name")
	}
}

// TestReadEvents checks reading a stored log for export.
func TestReadEvents(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	svc, err := NewPersistentService(dir)
	require.NoError(t, err)
	created, err := svc.Create(ctx, &adksession.CreateRequest{AppName: "app", UserID: "u", SessionID: "s1"})
	require.NoError(t, err)
	appendText(t, svc, created.Session, "user", "user", "hello", false)

	evs, err := ReadEvents(dir, "s1")
	require.NoError(t, err)
	require.Len(t, evs, 1)
	assert.Equal(t, "hello", evs[0].Content.Parts[0].Text)

	evs, err = ReadEvents(dir, "none")
	require.NoError(t, err)
	assert.Nil(t, evs, "no log")
	_, err = ReadEvents(dir, "../x")
	assert.Error(t, err, "an unsafe ID")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "torn"+eventsSuffix), []byte("{\"id\":\"e1\"}\n{bad"), filePerm))
	evs, err = ReadEvents(dir, "torn")
	assert.Error(t, err)
	assert.Len(t, evs, 1, "what was read before the bad line")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "locked"+eventsSuffix), nil, 0o000))
	if _, err := os.ReadFile(filepath.Join(dir, "locked"+eventsSuffix)); err == nil {
		t.Skip("running with privileges that ignore file modes")
	}
	_, err = ReadEvents(dir, "locked")
	assert.Error(t, err, "a log that can't be opened")
}
