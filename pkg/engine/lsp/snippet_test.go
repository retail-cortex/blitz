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
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A Go fragment is made a whole file the way its first line asks.
func TestWrapGo(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		prefix     int
		wrapped    bool
	}{
		{"a whole file", "// A program.\npackage main\n\nfunc main() {}\n", 0, false},
		{"declarations", "func Hello() string { return \"hi\" }\n", 2, false},
		{"an import first", "\nimport \"fmt\"\n\nfunc main() { fmt.Println() }\n", 2, false},
		{"a type after a comment", "/* A point. */\ntype Point struct{ X, Y int }\n", 2, false},
		{"a func after a long comment", "/*\nA long comment.\n*/\nfunc F() {}\n", 2, false},
		{"a comment then code on its line", "/* why */ x := 1\n", 3, true},
		{"line comments", "// first\n// second\nx := 1\n", 3, true},
		{"var declarations", "var answer = 42\n", 2, false},
		{"var then statements", "var x int\nx = 2\nfmt.Println(x)\n", 3, true},
		{"statements", "x := 1\nfmt.Println(x)\n", 3, true},
		{"a call", "fmt.Println(\"hi\")", 3, true},
		{"empty", "", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, w := wrapGo(tc.text)
			assert.Equal(t, wrapping{prefix: tc.prefix, wrapped: tc.wrapped}, w)
			assert.Contains(t, text, tc.text)
			if w.prefix > 0 {
				assert.Equal(t, "package snippet\n", text[:16])
			}
		})
	}
}

// Edits move from the server's text to the block's; those in what was
// added are dropped, and an end past the block stops at its last line.
func TestWrappingShift(t *testing.T) {
	w := wrapping{prefix: 3, wrapped: true}
	e := TextEdit{Start: Position{5, 2}, End: Position{5, 4}}
	assert.True(t, w.shift(&e, 4))
	assert.Equal(t, TextEdit{Start: Position{2, 2}, End: Position{2, 4}}, e)
	e = TextEdit{Start: Position{1, 1}, End: Position{1, 1}} // "package snippet"
	assert.False(t, w.shift(&e, 4))
	e = TextEdit{Start: Position{7, 1}, End: Position{9, 1}}
	assert.True(t, w.shift(&e, 4))
	assert.Equal(t, Position{4, 1}, e.End)
}

// A code block's document: the server gets the whole file, requests and
// answers are in the block's lines, and a definition in the block is "".
func TestSnippetDocument(t *testing.T) {
	var mu sync.Mutex
	var opened string
	var asked wirePosition
	seen := func() (string, wirePosition) {
		mu.Lock()
		defer mu.Unlock()
		return opened, asked
	}
	var src string
	m, f, src := newFakeManager(t, func(_ *fakeServer, method string, params json.RawMessage) (any, string) {
		var p struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
			Position wirePosition `json:"position"`
		}
		_ = json.Unmarshal(params, &p)
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "initialize":
			return map[string]any{"capabilities": map[string]any{}}, ""
		case "textDocument/didOpen":
			opened = p.TextDocument.Text
		case "textDocument/definition":
			asked = p.Position
			at := map[string]any{"start": map[string]any{"line": 3, "character": 0}, "end": map[string]any{"line": 3, "character": 1}}
			return []any{map[string]any{"uri": p.TextDocument.URI, "range": at}, map[string]any{"uri": fileURI(src), "range": at}}, ""
		case "textDocument/completion":
			edit := func(line int) map[string]any {
				return map[string]any{"range": map[string]any{"start": map[string]any{"line": line, "character": 0}, "end": map[string]any{"line": line, "character": 0}}, "newText": "x"}
			}
			return []any{map[string]any{"label": "Println", "textEdit": edit(4), "additionalTextEdits": []any{edit(1)}}}, ""
		}
		return nil, ""
	})
	path := SnippetPath(filepath.Dir(src), "k", ".go")
	info := m.OpenSnippet("w", path, "x := 1\nfmt.Println(x)\n", 1)
	require.NotEmpty(t, info.ID)
	require.Eventually(t, func() bool { o, _ := seen(); return o != "" }, 5*time.Second, 5*time.Millisecond)
	o, _ := seen()
	assert.Equal(t, "package snippet\n\nfunc _() {\nx := 1\nfmt.Println(x)\n\n}\n", o)

	locs, err := m.DocumentDefinition(context.Background(), info.ID, 1, 2, 13)
	require.NoError(t, err)
	_, a := seen()
	assert.Equal(t, wirePosition{Line: 4, Character: 12}, a, "line 2 of the block is line 5 of the file")
	require.Len(t, locs, 2)
	assert.Equal(t, "", locs[0].Path, "in the block")
	assert.Equal(t, 1, locs[0].Line)
	assert.Equal(t, filepath.Base(src), locs[1].Path, "another file, relative to the workspace")
	assert.Equal(t, 4, locs[1].Line)

	list, err := m.Complete(context.Background(), info.ID, 1, 2, 1, "")
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.NotNil(t, list.Items[0].Edit)
	assert.Equal(t, 2, list.Items[0].Edit.Start.Line)
	assert.Empty(t, list.Items[0].AdditionalEdits, "an edit to what was added (an import) can't be made")

	// Problems: in the block's lines; about what was added, dropped; a
	// fragment's unused variable and missing import, hints.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan DocumentDiagnostics, 8)
	go func() { _ = m.WatchDiagnostics(ctx, "w", func(d DocumentDiagnostics) error { got <- d; return nil }) }()
	diag := func(line int, msg string) map[string]any {
		return map[string]any{"range": map[string]any{"start": map[string]any{"line": line, "character": 0}, "end": map[string]any{"line": line, "character": 1}}, "severity": 1, "message": msg}
	}
	f.notify("textDocument/publishDiagnostics", map[string]any{"uri": fileURI(path), "diagnostics": []any{
		diag(0, "in package snippet"), diag(3, "declared and not used: x"), diag(4, "undefined: fmt"), diag(4, "too many arguments"),
	}})
	select {
	case d := <-got:
		assert.Equal(t, info.ID, d.Document)
		var seen []string
		for _, x := range d.Diagnostics {
			seen = append(seen, fmt.Sprintf("%d %s %s", x.Start.Line, x.Severity, x.Message))
		}
		assert.Equal(t, []string{"1 hint declared and not used: x", "2 hint undefined: fmt", "2 error too many arguments"}, seen)
	case <-time.After(5 * time.Second):
		t.Fatal("no diagnostics")
	}
}

// In a fragment, gopls's "undefined: fmt" for fmt.Println is a missing
// import; an undefined variable is still a problem.
func TestUnimported(t *testing.T) {
	text := "x := undefinedName\nfmt.Println(x)\nos.Exit(1)\n"
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{"undefined: fmt", true},
		{"undefined: os", true},
		{"undefined: undefinedName", false},
		{"undefined: Println", false},
		{"undefined: a.b", false},
		{"declared and not used: x", false},
	} {
		t.Run(tc.message, func(t *testing.T) {
			assert.Equal(t, tc.want, unimported(tc.message, text))
		})
	}
}
