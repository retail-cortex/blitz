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
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/pdftext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 8))))
	return buf.Bytes()
}

func TestExportPDF(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		args    map[string]any
		blocked []string
		wantOut string // the PDF written, workspace-relative
		wantErr string
		images  int
	}{
		{name: "beside the file", files: map[string]string{"notes/week1.md": "# Week 1\n\nGradients."},
			args: map[string]any{"path": "notes/week1.md"}, wantOut: "notes/week1.pdf"},
		{name: "to an output", files: map[string]string{"a.markdown": "# A"},
			args: map[string]any{"path": "a.markdown", "output": "out/a.pdf"}, wantOut: "out/a.pdf"},
		{name: "workspace image", files: map[string]string{"n.md": "![chart](img/c.png)", "img/c.png": "PNG"},
			args: map[string]any{"path": "n.md"}, wantOut: "n.pdf", images: 1},
		{name: "image outside the workspace", files: map[string]string{"n.md": "![x](../../etc/x.png)"},
			args: map[string]any{"path": "n.md"}, wantOut: "n.pdf", images: 0},
		{name: "blocked image", files: map[string]string{"n.md": "![k](secret/k.png)", "secret/k.png": "PNG"},
			blocked: []string{"secret/**"}, args: map[string]any{"path": "n.md"}, wantOut: "n.pdf", images: 0},
		{name: "not Markdown", files: map[string]string{"a.txt": "x"},
			args: map[string]any{"path": "a.txt"}, wantErr: "takes a Markdown file"},
		{name: "output not a PDF", files: map[string]string{"a.md": "x"},
			args: map[string]any{"path": "a.md", "output": "a.html"}, wantErr: "must end in .pdf"},
		{name: "binary", files: map[string]string{"a.md": "\x00\x01"},
			args: map[string]any{"path": "a.md"}, wantErr: "not a text file"},
		{name: "missing", args: map[string]any{"path": "nope.md"}, wantErr: "failed to read"},
		{name: "exists", files: map[string]string{"a.md": "x", "a.pdf": "old"},
			args: map[string]any{"path": "a.md"}, wantErr: "already exists"},
		{name: "overwrite", files: map[string]string{"a.md": "x", "a.pdf": "old"},
			args: map[string]any{"path": "a.md", "overwrite": true}, wantOut: "a.pdf"},
		{name: "a file outside the workspace", args: map[string]any{"path": "../a.md"}, wantErr: "outside"},
		{name: "outside the workspace", files: map[string]string{"a.md": "x"},
			args: map[string]any{"path": "a.md", "output": "../a.pdf"}, wantErr: "outside"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tc.files {
				data := []byte(content)
				if content == "PNG" {
					data = pngBytes(t)
				}
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o644))
			}
			ws, err := OpenWorkspace(WorkspaceOptions{Dir: dir, BlockedPaths: tc.blocked})
			require.NoError(t, err)
			t.Cleanup(func() { ws.Close() })

			out := runTool(t, toolOf(t)(NewExportPDFTool(ws, allowAll(), "a4")), tc.args)
			if tc.wantErr != "" {
				assert.Contains(t, errOf(out), tc.wantErr)
				assert.Equal(t, false, out["success"])
				return
			}
			require.Empty(t, errOf(out))
			assert.Equal(t, tc.wantOut, out["output"])
			data, err := os.ReadFile(filepath.Join(dir, tc.wantOut))
			require.NoError(t, err)
			require.True(t, pdftext.IsPDF(data))
			assert.EqualValues(t, len(data), out["bytes"])
			assert.EqualValues(t, 1, out["pages"])
			assert.Equal(t, tc.images, bytes.Count(data, []byte("/Subtype /Image")))
		})
	}
}

func TestExportPDFApprovalAndUndo(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "notes.md"), "# Notes\n\nText.")
	cp := NewCheckpoints(ws, 0)

	// Denied: nothing is written.
	h, reqs := approverHooks(false)
	out := runTool(t, toolOf(t)(NewExportPDFTool(ws, h, "")), map[string]any{"path": "notes.md"})
	assert.NotEmpty(t, errOf(out))
	require.Len(t, *reqs, 1)
	req := (*reqs)[0]
	assert.Equal(t, "export_pdf", req.Tool)
	assert.Equal(t, api.ActionWrite, req.Kind)
	assert.Equal(t, "write:"+ws.Dir(), req.Key)
	assert.Contains(t, req.Detail, "Create notes.pdf from notes.md (1 pages")
	assert.Empty(t, req.Diff)
	assert.NoFileExists(t, filepath.Join(dir, "notes.pdf"))

	// Approved, in a turn: written, and undone with the turn.
	cp.Begin("turn")
	h, _ = approverHooks(true)
	out = runTool(t, toolOf(t)(NewExportPDFTool(ws, h, "")), map[string]any{"path": "notes.md"})
	require.Empty(t, errOf(out))
	assert.FileExists(t, filepath.Join(dir, "notes.pdf"))
	_, err := cp.Undo(false)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "notes.pdf"))
}

func TestExportPDFOutputPath(t *testing.T) {
	tests := []struct{ path, output, want string }{
		{"notes/a.md", "", "notes/a.pdf"},
		{"a.markdown", "", "a.pdf"},
		{"a.md", "x/b.pdf", "x/b.pdf"},
		{"README", "", "README.pdf"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			assert.Equal(t, tc.want, ExportPDFOutputPath(tc.path, tc.output))
		})
	}
}

func TestHumanSize(t *testing.T) {
	assert.Equal(t, "512 B", humanSize(512))
	assert.Equal(t, "2 KB", humanSize(2048))
	assert.Equal(t, "1.5 MB", humanSize(3<<19))
}
