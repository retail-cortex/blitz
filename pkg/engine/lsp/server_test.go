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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// replyThenHangUp is an answer after which the fake server stops reading
// and goes away.
type replyThenHangUp struct{ result any }

// fakeServer plays a language server in the test's own process: answer
// gives each request's result, or an error message; it can send
// notifications with notify.
type fakeServer struct {
	answer func(f *fakeServer, method string, params json.RawMessage) (any, string)

	mu       sync.Mutex
	methods  []string
	launches int
	out      io.Writer
}

func (f *fakeServer) launcher() Launcher {
	return func(context.Context, []string) (Process, error) {
		serverIn, clientOut := io.Pipe()
		clientIn, serverOut := io.Pipe()
		f.mu.Lock()
		f.launches++
		f.mu.Unlock()
		go f.serve(serverIn, serverOut)
		return &pipeProcess{toServer: clientOut, fromServer: clientIn}, nil
	}
}

// send writes one message to the client.
func (f *fakeServer) send(v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(f.out, "Content-Length: %d\r\n\r\n%s", len(b), b)
}

// notify sends a notification.
func (f *fakeServer) notify(method string, params any) {
	f.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (f *fakeServer) serve(in *io.PipeReader, out *io.PipeWriter) {
	defer out.Close()
	f.out = out
	r := bufio.NewReader(in)
	for {
		body, err := readMessage(r)
		if err != nil {
			return
		}
		var m rpcMessage
		json.Unmarshal(body, &m)
		f.mu.Lock()
		f.methods = append(f.methods, m.Method)
		f.mu.Unlock()
		result, errText := any(nil), ""
		if f.answer != nil {
			result, errText = f.answer(f, m.Method, m.Params)
		}
		hangUp := false
		if r, ok := result.(replyThenHangUp); ok {
			result, hangUp = r.result, true
		}
		if len(m.ID) == 0 {
			continue
		}
		if errText != "" {
			f.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32603, "message": errText}})
			continue
		}
		f.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		if hangUp {
			in.Close()
			return
		}
	}
}

// launched is how many servers were started.
func (f *fakeServer) launched() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.launches
}

// newFakeManager is a manager for a .go server played by answer, over a
// temporary workspace with main.go.
func newFakeManager(t *testing.T, answer func(f *fakeServer, method string, params json.RawMessage) (any, string)) (*Manager, *fakeServer, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	src := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(src, []byte("package main\n\nfunc Foo() {}\n"), 0o644))
	f := &fakeServer{answer: answer}
	m := NewManager(dir, []Server{{Language: "go", Command: []string{"fake"}, Extensions: []string{".go"}}}, f.launcher())
	t.Cleanup(m.Close)
	return m, f, src
}

// The connection skips what isn't JSON, fails what it can't send, and
// fails a call waiting when the server goes away.
func TestConnFailures(t *testing.T) {
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	p := &pipeProcess{toServer: clientOut, fromServer: clientIn}
	notes := make(chan string, 1)
	c := newConn(p, func(method string, _ json.RawMessage) { notes <- method })
	defer p.Stop()

	go func() {
		fmt.Fprint(serverOut, "Content-Length: 3\r\n\r\nxyz")
		hello := `{"jsonrpc":"2.0","method":"window/hello"}`
		fmt.Fprintf(serverOut, "Content-Length: %d\r\n\r\n%s", len(hello), hello)
	}()
	select {
	case m := <-notes:
		assert.Equal(t, "window/hello", m, "after a message that isn't JSON")
	case <-time.After(5 * time.Second):
		t.Fatal("no notification")
	}

	ctx := context.Background()
	assert.Error(t, c.call(ctx, "bad", map[string]any{"x": make(chan int)}, nil), "params that aren't JSON")

	waiting := make(chan error, 1)
	go func() { waiting <- c.call(ctx, "slow", nil, nil) }()
	r := bufio.NewReader(serverIn)
	_, err := readMessage(r) // the request
	require.NoError(t, err)
	serverIn.Close() // the server stops reading
	assert.ErrorIs(t, c.send("note", nil), io.ErrClosedPipe)
	serverOut.Close() // and goes away
	select {
	case err := <-waiting:
		assert.ErrorIs(t, err, errClosed)
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting call never ended")
	}
}

// The server's requests are answered without holding up the reader: a
// server that doesn't read its stdin until it's done writing is still
// heard, and answered once it reads.
func TestConnRepliesOffReader(t *testing.T) {
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	p := &pipeProcess{toServer: clientOut, fromServer: clientIn}
	notes := make(chan string, 1)
	newConn(p, func(method string, _ json.RawMessage) { notes <- method })
	defer p.Stop()

	go func() {
		for _, m := range []string{
			`{"jsonrpc":"2.0","id":7,"method":"workspace/configuration","params":{"items":[{"section":"python"},{"section":"pyright"}]}}`,
			`{"jsonrpc":"2.0","method":"window/hello"}`,
		} {
			fmt.Fprintf(serverOut, "Content-Length: %d\r\n\r\n%s", len(m), m)
		}
	}()
	select {
	case m := <-notes:
		assert.Equal(t, "window/hello", m)
	case <-time.After(5 * time.Second):
		t.Fatal("the reader is stuck answering")
	}
	body, err := readMessage(bufio.NewReader(serverIn))
	require.NoError(t, err)
	assert.JSONEq(t, `{"jsonrpc":"2.0","id":7,"result":[null,null]}`, string(body), "the defaults, one for each item")
}

// A server that can't start is reported, and not started again for a
// while; one that died is started again.
func TestManagerStarting(t *testing.T) {
	t.Run("no command", func(t *testing.T) {
		m := NewManager(t.TempDir(), []Server{{Language: "go", Extensions: []string{".go"}}}, nil)
		_, err := m.For(context.Background(), "a.go")
		assert.ErrorContains(t, err, "starting the go language server (): no command")
	})
	for _, tt := range []struct {
		name    string
		answer  func(f *fakeServer, method string, params json.RawMessage) (any, string)
		wantErr string
	}{
		{"initialize fails", func(*fakeServer, string, json.RawMessage) (any, string) { return nil, "not today" }, "initialize: not today"},
		{"deaf after initialize", func(*fakeServer, string, json.RawMessage) (any, string) { return replyThenHangUp{map[string]any{}}, "" }, "closed pipe"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, f, src := newFakeManager(t, tt.answer)
			_, err := m.For(context.Background(), src)
			assert.ErrorContains(t, err, tt.wantErr)
			_, again := m.For(context.Background(), src)
			assert.Equal(t, err, again, "the same error, without starting again")
			assert.Equal(t, 1, f.launched())
		})
	}
	t.Run("caller gave up", func(t *testing.T) {
		gate := make(chan struct{})
		m, f, src := newFakeManager(t, func(f *fakeServer, method string, _ json.RawMessage) (any, string) {
			if method == "initialize" && f.launched() == 1 {
				<-gate
			}
			return map[string]any{}, ""
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		time.AfterFunc(50*time.Millisecond, func() { close(gate) })
		_, err := m.For(ctx, src)
		assert.ErrorIs(t, err, context.Canceled)
		_, err = m.For(context.Background(), src)
		require.NoError(t, err, "the start carried on for the next caller")
		assert.Equal(t, 1, f.launched())
	})
	t.Run("tried again after a while", func(t *testing.T) {
		old := failedRetry
		failedRetry = 50 * time.Millisecond
		t.Cleanup(func() { failedRetry = old })
		m, f, src := newFakeManager(t, func(f *fakeServer, _ string, _ json.RawMessage) (any, string) {
			if f.launched() == 1 {
				return nil, "not today"
			}
			return map[string]any{}, ""
		})
		_, err := m.For(context.Background(), src)
		require.ErrorContains(t, err, "not today")
		_, again := m.For(context.Background(), src)
		assert.Equal(t, err, again, "remembered at first")
		require.Eventually(t, func() bool {
			_, err := m.For(context.Background(), src)
			return err == nil
		}, 5*time.Second, 20*time.Millisecond)
		assert.Equal(t, 2, f.launched())
	})
	t.Run("died", func(t *testing.T) {
		m, f, src := newFakeManager(t, nil)
		s, err := m.For(context.Background(), src)
		require.NoError(t, err)
		s.c.proc.Stop()
		<-s.c.done
		s2, err := m.For(context.Background(), src)
		require.NoError(t, err)
		assert.NotSame(t, s, s2)
		assert.Equal(t, 2, f.launched())
	})
	t.Run("closed", func(t *testing.T) {
		m, _, src := newFakeManager(t, nil)
		m.Close()
		_, err := m.For(context.Background(), src)
		assert.ErrorIs(t, err, errClosed)
	})
}

// A server stuck in initialize holds up only its own language: another
// starts meanwhile, a caller for it gives up with its own context, and
// Close ends the start without waiting out its timeout.
func TestManagerSlowStart(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	stuck := &fakeServer{answer: func(_ *fakeServer, method string, _ json.RawMessage) (any, string) {
		if method == "initialize" {
			<-gate
		}
		return map[string]any{}, ""
	}}
	quick := &fakeServer{}
	launchStuck, launchQuick := stuck.launcher(), quick.launcher()
	m := NewManager(dir, []Server{
		{Language: "go", Command: []string{"stuck"}, Extensions: []string{".go"}},
		{Language: "python", Command: []string{"quick"}, Extensions: []string{".py"}},
	}, func(ctx context.Context, argv []string) (Process, error) {
		if argv[0] == "stuck" {
			return launchStuck(ctx, argv)
		}
		return launchQuick(ctx, argv)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = m.For(ctx, filepath.Join(dir, "a.go"))
	assert.ErrorIs(t, err, context.DeadlineExceeded, "the caller gave up")

	py, err := m.For(context.Background(), filepath.Join(dir, "a.py"))
	require.NoError(t, err, "another language isn't held up")
	assert.NotNil(t, py)

	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close waited for the stuck start")
	}
	_, err = m.For(context.Background(), filepath.Join(dir, "a.go"))
	assert.ErrorIs(t, err, errClosed)
}

// Each request reports the server's error, and a file that can't be read.
func TestManagerRequestErrors(t *testing.T) {
	ctx := context.Background()
	m, _, src := newFakeManager(t, func(_ *fakeServer, method string, _ json.RawMessage) (any, string) {
		if strings.Contains(method, "/") {
			return nil, "broken"
		}
		return map[string]any{}, ""
	})
	missing := filepath.Join(filepath.Dir(src), "gone.go")
	for _, tt := range []struct {
		name    string
		run     func() error
		wantErr string
	}{
		{"definition", func() error { _, err := m.Definition(ctx, src, 1, 1); return err }, "textDocument/definition: broken"},
		{"references", func() error { _, err := m.References(ctx, src, 1, 1); return err }, "textDocument/references: broken"},
		{"references of a missing file", func() error { _, err := m.References(ctx, missing, 1, 1); return err }, "no such file"},
		{"hover", func() error { _, err := m.Hover(ctx, src, 1, 1); return err }, "textDocument/hover: broken"},
		{"hover of a missing file", func() error { _, err := m.Hover(ctx, missing, 1, 1); return err }, "no such file"},
		{"symbols", func() error { _, err := m.Symbols(ctx, src, "x"); return err }, "workspace/symbol: broken"},
		{"symbols of no server", func() error { _, err := m.Symbols(ctx, "a.txt", "x"); return err }, ErrNoServer.Error()},
		{"diagnostics of a missing file", func() error { _, err := m.Diagnostics(ctx, missing, time.Second); return err }, "no such file"},
		{"diagnostics of no server", func() error { _, err := m.Diagnostics(ctx, "a.txt", time.Second); return err }, ErrNoServer.Error()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorContains(t, tt.run(), tt.wantErr)
		})
	}
}

// What servers answer, read: a single location, nothing, too many; hover
// it can't read; symbols of unknown kinds; diagnostics that never come.
func TestManagerAnswers(t *testing.T) {
	ctx := context.Background()
	var answers sync.Map // method → result
	m, _, src := newFakeManager(t, func(f *fakeServer, method string, _ json.RawMessage) (any, string) {
		if method == "textDocument/didOpen" {
			f.notify("window/logMessage", map[string]any{"message": "hi"})
			f.notify("textDocument/publishDiagnostics", "not diagnostics")
		}
		if v, ok := answers.Load(method); ok {
			return v, ""
		}
		return map[string]any{}, ""
	})
	uri := fileURI(src)
	at := func(line, char int) map[string]any {
		return map[string]any{"uri": uri, "range": map[string]any{"start": map[string]any{"line": line, "character": char}}}
	}
	many := make([]any, maxResults+5)
	for i := range many {
		many[i] = at(2, 5)
	}

	for _, tt := range []struct {
		name   string
		answer any
		want   []Location
	}{
		{"one", at(2, 5), []Location{{Path: "main.go", Line: 3, Column: 6, Text: "func Foo() {}"}}},
		{"none", nil, nil},
		{"not a location", map[string]any{"x": 1}, nil},
		{"elsewhere", map[string]any{"uri": "untitled:x", "range": map[string]any{}}, []Location{{Path: "untitled:x", Line: 1, Column: 1}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			answers.Store("textDocument/definition", tt.answer)
			got, err := m.Definition(ctx, src, 3, 0)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	answers.Store("textDocument/references", many)
	refs, err := m.References(ctx, src, 3, 6)
	require.NoError(t, err)
	assert.Len(t, refs, maxResults)

	answers.Store("textDocument/hover", map[string]any{"contents": 42})
	hover, err := m.Hover(ctx, src, 3, 6)
	require.NoError(t, err)
	assert.Empty(t, hover)

	syms := make([]any, maxResults+5)
	for i := range syms {
		syms[i] = map[string]any{"name": "Foo", "kind": 99, "location": at(2, 5)}
	}
	answers.Store("workspace/symbol", syms)
	got, err := m.Symbols(ctx, src, "Foo")
	require.NoError(t, err)
	require.Len(t, got, maxResults)
	assert.Empty(t, got[0].Kind, "a kind it doesn't know")

	diags, err := m.Diagnostics(ctx, src, 50*time.Millisecond)
	require.NoError(t, err)
	assert.Nil(t, diags, "nothing reported in time")
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = m.Diagnostics(cctx, src, time.Minute)
	assert.ErrorIs(t, err, context.Canceled)
}

// Diagnostics coming faster than they're read: the latest are kept.
func TestServerNotified(t *testing.T) {
	s := &server{text: map[string]string{"file:///a.go": "package a"}, diags: map[string]publishedDiags{}, changed: make(chan struct{}, 1)}
	for _, uri := range []string{"file:///a.go", "file:///a.go", "file:///other.go"} {
		params, _ := json.Marshal(map[string]any{"uri": uri, "diagnostics": []any{map[string]any{"message": "about " + uri}}})
		s.notified("textDocument/publishDiagnostics", params)
	}
	assert.Equal(t, "about file:///a.go", s.diags["file:///a.go"].items[0].Message)
	assert.NotContains(t, s.diags, "file:///other.go", "only open documents' diagnostics are kept")
	assert.Len(t, s.changed, 1)
}

// Only the documents synced last stay open: the others are closed, and
// their text and diagnostics forgotten.
func TestServerKeepsFewDocumentsOpen(t *testing.T) {
	old := maxOpen
	maxOpen = 2
	t.Cleanup(func() { maxOpen = old })
	m, f, src := newFakeManager(t, func(_ *fakeServer, method string, _ json.RawMessage) (any, string) {
		if method == "initialize" {
			return map[string]any{"capabilities": map[string]any{}}, ""
		}
		return nil, ""
	})
	dir := filepath.Dir(src)
	files := []string{src}
	for _, name := range []string{"b.go", "c.go", "d.go"} {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte("package main\n"), 0o644))
		files = append(files, p)
	}
	ctx := context.Background()
	for _, p := range files {
		_, err := m.Diagnostics(ctx, p, time.Millisecond)
		require.NoError(t, err)
	}
	m.mu.Lock()
	s := m.running["go"]
	m.mu.Unlock()
	s.mu.Lock()
	assert.Equal(t, []string{fileURI(files[2]), fileURI(files[3])}, s.opened)
	assert.Len(t, s.text, 2)
	assert.Len(t, s.version, 2)
	s.mu.Unlock()
	require.Eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		closed := slices.DeleteFunc(slices.Clone(f.methods), func(m string) bool { return m != "textDocument/didClose" })
		return len(closed) == 2
	}, 5*time.Second, 10*time.Millisecond, "the two oldest closed")

	// One closed is opened again when asked about.
	_, err := m.Diagnostics(ctx, src, time.Millisecond)
	require.NoError(t, err)
	s.mu.Lock()
	assert.Equal(t, []string{fileURI(files[3]), fileURI(src)}, s.opened)
	s.mu.Unlock()
}

// A long line is cut in a location's text; a column before the line starts
// is its first.
func TestReadableLongLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.go")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", 300)), 0o644))
	m := NewManager(dir, nil, nil)
	loc := m.readable(fileURI(path), wirePosition{})
	assert.Equal(t, strings.Repeat("x", 200)+"…", loc.Text)
	pos, err := position("abc", 1, 0)
	require.NoError(t, err)
	assert.Equal(t, wirePosition{}, pos)
}

func TestLanguageID(t *testing.T) {
	for file, want := range map[string]string{
		"a.ts": "typescript", "a.tsx": "typescriptreact", "a.mjs": "javascript", "a.jsx": "javascriptreact",
		"a.py": "python", "a.rs": "rust", "a.go": "go", "a.rb": "ruby",
	} {
		t.Run(file, func(t *testing.T) {
			assert.Equal(t, want, languageID("ruby", file))
		})
	}
}

// After initialized, the client says its configuration changed: pyright
// answers nothing until it hears it.
func TestManagerSendsConfiguration(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	m, _, src := newFakeManager(t, func(_ *fakeServer, method string, _ json.RawMessage) (any, string) {
		mu.Lock()
		methods = append(methods, method)
		mu.Unlock()
		if method == "initialize" {
			return map[string]any{"capabilities": map[string]any{}}, ""
		}
		return nil, ""
	})
	_, err := m.For(context.Background(), src)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(methods) >= 3
	}, 5*time.Second, 5*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"initialize", "initialized", "workspace/didChangeConfiguration"}, methods[:3])
}
