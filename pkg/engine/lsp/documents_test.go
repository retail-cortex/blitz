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

package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// textSync records the text a fake server was sent for each URI, as
// didOpen, didChange and didClose leave it ("" once closed).
type textSync struct {
	mu    sync.Mutex
	texts map[string]string
	opens map[string]int
}

func (ts *textSync) record(method string, params json.RawMessage) {
	var p struct {
		TextDocument struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
	}
	_ = json.Unmarshal(params, &p)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	switch method {
	case "textDocument/didOpen":
		ts.texts[p.TextDocument.URI] = p.TextDocument.Text
		ts.opens[p.TextDocument.URI]++
	case "textDocument/didChange":
		ts.texts[p.TextDocument.URI] = p.ContentChanges[0].Text
	case "textDocument/didClose":
		delete(ts.texts, p.TextDocument.URI)
	}
}

func (ts *textSync) text(uri string) (string, bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	t, ok := ts.texts[uri]
	return t, ok
}

// await waits for the server to have text for uri (closed: open false):
// notifications arrive after the call that sent them returns.
func (ts *textSync) await(t *testing.T, uri, text string, open bool) {
	t.Helper()
	require.Eventually(t, func() bool {
		got, ok := ts.text(uri)
		return ok == open && (!open || got == text)
	}, 5*time.Second, 5*time.Millisecond, "the server's text for %s: want %q (open %v)", uri, text, open)
}

// documentIDs are a client's open documents, without renewing its lease.
func documentIDs(m *Manager, client string) []string {
	m.docs.mu.Lock()
	defer m.docs.mu.Unlock()
	var ids []string
	for id, doc := range m.docs.byID {
		if doc.Client == client {
			ids = append(ids, id)
		}
	}
	return ids
}

// newDocManager is a fake .go server that records text sync and answers
// with answer (initialize is answered for it).
func newDocManager(t *testing.T, answer func(f *fakeServer, method string, params json.RawMessage) (any, string)) (*Manager, *fakeServer, *textSync, string) {
	t.Helper()
	ts := &textSync{texts: map[string]string{}, opens: map[string]int{}}
	m, f, src := newFakeManager(t, func(f *fakeServer, method string, params json.RawMessage) (any, string) {
		ts.record(method, params)
		if method == "initialize" {
			return map[string]any{"capabilities": map[string]any{}}, ""
		}
		if answer != nil {
			return answer(f, method, params)
		}
		return nil, ""
	})
	return m, f, ts, src
}

// openReady opens src with text and waits for it to reach the server.
func openReady(t *testing.T, m *Manager, ts *textSync, src, text string, version int64) string {
	t.Helper()
	info := m.OpenDocument("window-1", src, text, version)
	require.NotEmpty(t, info.ID)
	require.Equal(t, "go", info.Language)
	require.Eventually(t, func() bool { got, ok := ts.text(fileURI(src)); return ok && got == text }, 5*time.Second, 5*time.Millisecond)
	return info.ID
}

// The editor's text, not the disk's, is the server's copy while it's open;
// the agent's syncs leave it alone; closing it goes back to the disk.
func TestDocumentsHoldTheEditorsText(t *testing.T) {
	m, _, ts, src := newDocManager(t, nil)
	uri := fileURI(src)
	disk, err := os.ReadFile(src)
	require.NoError(t, err)

	id := openReady(t, m, ts, src, "package main // unsaved\n", 1)
	_, err = m.Diagnostics(context.Background(), src, time.Millisecond) // the agent asks
	require.NoError(t, err)
	got, _ := ts.text(uri)
	assert.Equal(t, "package main // unsaved\n", got, "the agent's sync didn't put the disk's text back")

	require.NoError(t, m.ChangeDocument(context.Background(), id, 2, "package main // more\n"))
	ts.await(t, uri, "package main // more\n", true)
	assert.ErrorIs(t, m.ChangeDocument(context.Background(), id, 1, "older"), ErrStale)

	require.NoError(t, m.CloseDocument(id))
	ts.await(t, uri, "", false)
	assert.ErrorIs(t, m.CloseDocument(id), ErrUnknownDocument)
	_, err = m.Diagnostics(context.Background(), src, time.Millisecond)
	require.NoError(t, err)
	ts.await(t, uri, string(disk), true) // the agent's next sync is from disk
}

// Two windows on one file: the server closes it when neither holds it.
func TestDocumentsSharedByTwoWindows(t *testing.T) {
	m, _, ts, src := newDocManager(t, nil)
	a := openReady(t, m, ts, src, "package main\n", 1)
	b := m.OpenDocument("window-2", src, "package main\n", 1)
	require.Eventually(t, func() bool {
		m.docs.mu.Lock()
		defer m.docs.mu.Unlock()
		return m.docs.byID[b.ID].server != nil
	}, 5*time.Second, 5*time.Millisecond)
	require.NoError(t, m.CloseDocument(a))
	time.Sleep(20 * time.Millisecond) // a didClose would have arrived
	_, open := ts.text(fileURI(src))
	assert.True(t, open, "the other window still holds it")
	require.NoError(t, m.CloseDocument(b.ID))
	ts.await(t, fileURI(src), "", false)
}

// A file without a server has no document; a missing server says what to
// install.
func TestOpenDocumentStates(t *testing.T) {
	m, _, _, src := newDocManager(t, nil)
	info := m.OpenDocument("w", filepath.Join(filepath.Dir(src), "notes.txt"), "", 1)
	assert.Equal(t, DocumentInfo{State: StateNone}, info)

	missing := NewManager(t.TempDir(), []Server{{Language: "go", Command: []string{"gopls"}, Extensions: []string{".go"}}},
		func(context.Context, []string) (Process, error) { return nil, &NotInstalledError{Command: "gopls"} })
	t.Cleanup(missing.Close)
	_, err := missing.For(context.Background(), "a.go")
	require.Error(t, err)
	info = missing.OpenDocument("w", "a.go", "", 1)
	assert.Equal(t, StateMissing, info.State)
	assert.Contains(t, info.Detail, "gopls isn't installed")
	assert.Equal(t, "go install golang.org/x/tools/gopls@latest", info.Install)
	status := missing.Status()
	require.Len(t, status, 1)
	assert.Equal(t, StateMissing, status[0].State)
	assert.Equal(t, info.Install, status[0].Install)
	assert.False(t, status[0].Since.IsZero())
}

// Completion: the server's items in characters (its edits in UTF-16
// offsets), with their kinds, snippets and extra edits.
func TestComplete(t *testing.T) {
	var asked json.RawMessage
	m, _, ts, src := newDocManager(t, func(_ *fakeServer, method string, params json.RawMessage) (any, string) {
		if method != "textDocument/completion" {
			return nil, ""
		}
		asked = params
		edit := func(line, from, to int, text string) map[string]any {
			return map[string]any{"range": map[string]any{"start": map[string]any{"line": line, "character": from}, "end": map[string]any{"line": line, "character": to}}, "newText": text}
		}
		return map[string]any{"isIncomplete": true, "items": []any{
			map[string]any{"label": "Println", "kind": 3, "detail": "func(a ...any)", "documentation": map[string]any{"kind": "markdown", "value": "Prints."},
				"insertTextFormat": 2, "textEdit": edit(1, 7, 9, "Println(${1:})"),
				"additionalTextEdits": []any{edit(0, 12, 12, "\nimport \"fmt\"")}},
			map[string]any{"label": "x", "kind": 6},
		}}, ""
	})
	// "é" and "🌍" take 1 and 2 UTF-16 units before the edit.
	id := openReady(t, m, ts, src, "package main\né🌍 fmt.Pr\n", 3)
	list, err := m.Complete(context.Background(), id, 3, 2, 10, ".")
	require.NoError(t, err)
	assert.True(t, list.Incomplete)
	require.Len(t, list.Items, 2)
	p := list.Items[0]
	assert.Equal(t, "function", p.Kind)
	assert.Equal(t, "Prints.", p.Documentation)
	assert.True(t, p.Snippet)
	require.NotNil(t, p.Edit)
	assert.Equal(t, TextEdit{Start: Position{2, 7}, End: Position{2, 9}, Text: "Println(${1:})"}, *p.Edit, "UTF-16 offset 7 is after é, 🌍, space, f, m, t")
	assert.Equal(t, []TextEdit{{Start: Position{1, 13}, End: Position{1, 13}, Text: "\nimport \"fmt\""}}, p.AdditionalEdits)
	assert.Equal(t, Completion{Label: "x", Kind: "variable", InsertText: "x"}, list.Items[1])
	var ctx struct {
		Position wirePosition `json:"position"`
		Context  struct {
			TriggerKind      int    `json:"triggerKind"`
			TriggerCharacter string `json:"triggerCharacter"`
		} `json:"context"`
	}
	require.NoError(t, json.Unmarshal(asked, &ctx))
	assert.Equal(t, wirePosition{Line: 1, Character: 10}, ctx.Position, "column 10 is after 9 characters, 10 UTF-16 units")
	assert.Equal(t, 2, ctx.Context.TriggerKind)
	assert.Equal(t, ".", ctx.Context.TriggerCharacter)

	_, err = m.Complete(context.Background(), id, 2, 1, 1, "")
	assert.ErrorIs(t, err, ErrStale, "a request about another version")
	_, err = m.Complete(context.Background(), "doc-404", 3, 1, 1, "")
	assert.ErrorIs(t, err, ErrUnknownDocument)
}

// A bare array of items, and the cap on how many come back.
func TestCompleteBareArrayAndCap(t *testing.T) {
	m, _, ts, src := newDocManager(t, func(_ *fakeServer, method string, _ json.RawMessage) (any, string) {
		if method != "textDocument/completion" {
			return nil, ""
		}
		var items []any
		for i := range maxCompletions + 5 {
			items = append(items, map[string]any{"label": fmt.Sprintf("item%d", i)})
		}
		return items, ""
	})
	id := openReady(t, m, ts, src, "package main\n", 1)
	list, err := m.Complete(context.Background(), id, 1, 1, 1, "")
	require.NoError(t, err)
	assert.Len(t, list.Items, maxCompletions)
	assert.True(t, list.Incomplete, "cut short")
}

// Hover, definition and references are about the editor's text.
func TestDocumentRequests(t *testing.T) {
	var uri string // the file's, once made
	m, _, ts, src := newDocManager(t, func(_ *fakeServer, method string, _ json.RawMessage) (any, string) {
		switch method {
		case "textDocument/hover":
			return map[string]any{"contents": map[string]any{"kind": "markdown", "value": "func Foo()"}}, ""
		case "textDocument/definition", "textDocument/references":
			return []any{map[string]any{"uri": uri, "range": map[string]any{"start": map[string]any{"line": 2, "character": 5}, "end": map[string]any{"line": 2, "character": 8}}}}, ""
		}
		return nil, ""
	})
	uri = fileURI(src)
	id := openReady(t, m, ts, src, "package main\n\nfunc Foo() {}\n", 1)
	ctx := context.Background()
	h, err := m.DocumentHover(ctx, id, 1, 3, 6)
	require.NoError(t, err)
	assert.Equal(t, "func Foo()", h)
	for _, f := range []func(context.Context, string, int64, int, int) ([]Location, error){m.DocumentDefinition, m.DocumentReferences} {
		locs, err := f(ctx, id, 1, 3, 6)
		require.NoError(t, err)
		require.Len(t, locs, 1)
		assert.Equal(t, Location{Path: "main.go", Line: 3, Column: 6, Text: "func Foo() {}"}, locs[0])
	}
	_, err = m.DocumentHover(ctx, id, 1, 99, 1)
	assert.ErrorContains(t, err, "line 99")
}

// Watching sends each document's diagnostics as they come, in characters,
// with codes; diagnostics about an older text are skipped.
func TestWatchDiagnostics(t *testing.T) {
	m, f, ts, src := newDocManager(t, nil)
	id := openReady(t, m, ts, src, "package main\nvar é🌍x int\n", 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan DocumentDiagnostics, 8)
	done := make(chan error, 1)
	go func() {
		done <- m.WatchDiagnostics(ctx, "window-1", func(d DocumentDiagnostics) error { got <- d; return nil })
	}()
	publish := func(version int, msg string, code any) {
		f.notify("textDocument/publishDiagnostics", map[string]any{"uri": fileURI(src), "version": version, "diagnostics": []any{
			map[string]any{"range": map[string]any{"start": map[string]any{"line": 1, "character": 7}, "end": map[string]any{"line": 1, "character": 8}},
				"severity": 2, "message": msg, "source": "compiler", "code": code},
		}})
	}
	publish(0, "old text", nil) // the server's version 1 is current: unsaid counts as current
	publish(7, "a version the server never had", "X")
	publish(1, "unused", "UnusedVar")
	want := DocumentDiagnostics{Document: id, Version: 4, Diagnostics: []DocumentDiagnostic{{
		Start: Position{2, 7}, End: Position{2, 8}, Severity: "warning", Message: "unused", Source: "compiler", Code: "UnusedVar"}}}
	require.Eventually(t, func() bool {
		for {
			select {
			case d := <-got:
				if len(d.Diagnostics) == 1 && d.Diagnostics[0].Message == "unused" {
					assert.Equal(t, want, d)
					return true
				}
				assert.NotEqual(t, "a version the server never had", d.Diagnostics[0].Message)
			default:
				return false
			}
		}
	}, 5*time.Second, 5*time.Millisecond)
	publish(1, "numbered", 12)
	select {
	case d := <-got:
		assert.Equal(t, "12", d.Diagnostics[0].Code)
	case <-time.After(5 * time.Second):
		t.Fatal("no diagnostics")
	}
	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)

	failing := errors.New("the stream broke")
	err := m.WatchDiagnostics(context.Background(), "window-1", func(DocumentDiagnostics) error { return failing })
	assert.ErrorIs(t, err, failing, "the latest are sent first, and a send error ends the watch")
}

// A client's documents last while it calls or watches, and close when it
// stops; the oldest go beyond the cap.
func TestDocumentLeases(t *testing.T) {
	oldTTL, oldCheck, oldMax := leaseTTL, leaseCheck, maxDocuments
	leaseTTL, leaseCheck, maxDocuments = 50*time.Millisecond, 10*time.Millisecond, 2
	t.Cleanup(func() { leaseTTL, leaseCheck, maxDocuments = oldTTL, oldCheck, oldMax })
	m, _, ts, src := newDocManager(t, nil)

	openReady(t, m, ts, src, "package main\n", 1)
	require.Eventually(t, func() bool { return len(documentIDs(m, "window-1")) == 0 }, 5*time.Second, 5*time.Millisecond, "closed once its client went quiet")
	ts.await(t, fileURI(src), "", false)

	// Watching keeps them.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	kept := m.OpenDocument("window-2", src, "package main\n", 1)
	go func() { _ = m.WatchDiagnostics(ctx, "window-2", func(DocumentDiagnostics) error { return nil }) }()
	time.Sleep(4 * 50 * time.Millisecond)
	assert.Equal(t, []string{kept.ID}, documentIDs(m, "window-2"))

	// Beyond the cap the least recently seen close.
	dir := filepath.Dir(src)
	var ids []string
	for _, name := range []string{"b.go", "c.go"} {
		ids = append(ids, m.OpenDocument("window-2", filepath.Join(dir, name), "package main\n", 1).ID)
		time.Sleep(time.Millisecond)
	}
	assert.ElementsMatch(t, ids, documentIDs(m, "window-2"))
	assert.ElementsMatch(t, ids, m.KeepDocuments("window-2"), "KeepDocuments lists them too")
}

// A server that doesn't answer three requests in a row is stopped, and the
// next request starts it again with the editor's documents.
func TestStuckServerRestarts(t *testing.T) {
	old := requestTimeout
	requestTimeout = 20 * time.Millisecond
	t.Cleanup(func() { requestTimeout = old })
	var mu sync.Mutex
	deaf := true
	m, f, ts, src := newDocManager(t, func(_ *fakeServer, method string, _ json.RawMessage) (any, string) {
		mu.Lock()
		defer mu.Unlock()
		if method == "textDocument/hover" && deaf {
			time.Sleep(100 * time.Millisecond)
		}
		return map[string]any{"contents": "ok"}, ""
	})
	id := openReady(t, m, ts, src, "package main\n", 1)
	for range 3 {
		_, err := m.DocumentHover(context.Background(), id, 1, 1, 1)
		assert.Error(t, err)
	}
	require.Eventually(t, func() bool { s, _ := m.stateOf("go"); return s != StateReady }, 5*time.Second, 5*time.Millisecond, "stopped")
	mu.Lock()
	deaf = false
	mu.Unlock()
	h, err := m.DocumentHover(context.Background(), id, 1, 1, 1)
	require.NoError(t, err)
	assert.Equal(t, "ok", h)
	assert.Equal(t, 2, f.launched(), "started again")
	ts.mu.Lock()
	assert.Equal(t, 2, ts.opens[fileURI(src)], "the document opened in the new server")
	ts.mu.Unlock()
}

// Restart stops a running server; an unknown language is an error.
func TestRestart(t *testing.T) {
	m, f, ts, src := newDocManager(t, nil)
	openReady(t, m, ts, src, "package main\n", 1)
	require.NoError(t, m.Restart("go"))
	s, _ := m.stateOf("go")
	assert.Equal(t, StateIdle, s)
	assert.ErrorIs(t, m.Restart("cobol"), ErrNoServer)
	require.NoError(t, m.Restart("go"), "not running is fine")
	_, err := m.For(context.Background(), src)
	require.NoError(t, err)
	assert.Equal(t, 2, f.launched())
}
