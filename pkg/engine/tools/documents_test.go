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
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-pdf/fpdf"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pdfOf is a PDF with one page per text.
func pdfOf(t *testing.T, pages ...string) []byte {
	t.Helper()
	f := fpdf.New("P", "mm", "A4", "")
	f.SetFont("Helvetica", "", 12)
	for _, p := range pages {
		f.AddPage()
		f.Cell(0, 10, p)
	}
	var buf bytes.Buffer
	require.NoError(t, f.Output(&buf))
	return buf.Bytes()
}

func TestReadFilePDF(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "paper.pdf"), pdfOf(t, "Abstract", "Method", "Results"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fake.pdf"), []byte("plain text\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.pdf"), []byte("%PDF-1.7 cut"), 0o644))
	rt := toolOf(t)(NewReadFileTool(ws))
	tests := []struct {
		name    string
		args    map[string]any
		want    []string
		notWant []string
		note    string
		wantErr string
	}{
		{name: "the text, paged", args: map[string]any{"path": "paper.pdf"},
			want: []string{"--- Page 1 ---", "Abstract", "--- Page 3 ---", "Results"}, note: "a 3-page PDF"},
		{name: "a range", args: map[string]any{"path": "paper.pdf", "start_line": 4, "end_line": 5},
			want: []string{"Method"}, notWant: []string{"Abstract", "Results"}},
		{name: "not a PDF inside", args: map[string]any{"path": "fake.pdf"}, want: []string{"plain text"}},
		{name: "unreadable", args: map[string]any{"path": "broken.pdf"}, wantErr: "unreadable PDF"},
		{name: "outside", args: map[string]any{"path": "../x.pdf"}, wantErr: "failed to read file"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := runTool(t, rt, tc.args)
			if tc.wantErr != "" {
				assert.Contains(t, errOf(out), tc.wantErr)
				return
			}
			require.Empty(t, errOf(out))
			content, _ := out["content"].(string)
			for _, w := range tc.want {
				assert.Contains(t, content, w)
			}
			for _, w := range tc.notWant {
				assert.NotContains(t, content, w)
			}
			if tc.note != "" {
				assert.Contains(t, out["note"], tc.note)
			}
		})
	}
}

func TestPDFTextCache(t *testing.T) {
	c := &pdfTextCache{docs: map[[32]byte]pdfDoc{}}
	var docs [][]byte
	for i := range pdfTextCacheSize + 2 {
		docs = append(docs, pdfOf(t, fmt.Sprintf("doc %d", i)))
	}
	for _, d := range docs {
		text, pages, err := c.get(context.Background(), d)
		require.NoError(t, err)
		assert.Equal(t, 1, pages)
		assert.Contains(t, text, "doc")
	}
	assert.Len(t, c.docs, pdfTextCacheSize, "the oldest are dropped")
	text, _, err := c.get(context.Background(), docs[len(docs)-1])
	require.NoError(t, err)
	assert.Contains(t, text, fmt.Sprintf("doc %d", len(docs)-1))
	_, _, err = c.get(context.Background(), []byte("%PDF-bad"))
	assert.Error(t, err)
}

func TestViewDocument(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	store, err := images.OpenStore(t.TempDir())
	require.NoError(t, err)
	r := &Registry{workspace: ws, hooks: allowAll(), images: store, imageOpts: images.Options{MaxInput: 20 << 20}}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "paper.pdf"), pdfOf(t, "one", "two"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "shot.png"), pngBytes(t), 0o644))
	rt := toolOf(t)(NewViewDocumentTool(r))

	out := runTool(t, rt, map[string]any{"path": "paper.pdf"})
	require.Empty(t, errOf(out))
	assert.EqualValues(t, 2, out["pages"])
	uri, _ := out[images.ToolResultKey].(string)
	assert.Equal(t, "application/pdf", store.MIME(uri))
	assert.Equal(t, 2, store.Pages(uri))

	for path, want := range map[string]string{"shot.png": "isn't a PDF", "missing.pdf": "cannot view document", "../x.pdf": "cannot view document"} {
		t.Run(path, func(t *testing.T) {
			assert.Contains(t, errOf(runTool(t, rt, map[string]any{"path": path})), want)
		})
	}

	disabled := toolOf(t)(NewViewDocumentTool(&Registry{workspace: ws, hooks: allowAll()}))
	assert.Contains(t, errOf(runTool(t, disabled, map[string]any{"path": "paper.pdf"})), api.ErrImagesDisabled.Error())
}
