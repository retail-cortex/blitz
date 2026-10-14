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
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// export_docx shares export_pdf's checks (TestExportPDF covers them all);
// these are the Word document's own.
func TestExportDocx(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		args    map[string]any
		wantOut string
		wantErr string
		images  int
	}{
		{name: "beside the file", files: map[string]string{"notes/week1.md": "# Week 1\n\nGradients."},
			args: map[string]any{"path": "notes/week1.md"}, wantOut: "notes/week1.docx"},
		{name: "workspace image", files: map[string]string{"n.md": "![chart](img/c.png)", "img/c.png": "PNG"},
			args: map[string]any{"path": "n.md"}, wantOut: "n.docx", images: 1},
		{name: "markdown given", args: map[string]any{"markdown": "# Summary", "output": "out/summary.docx"}, wantOut: "out/summary.docx"},
		{name: "output not a Word document", files: map[string]string{"a.md": "x"},
			args: map[string]any{"path": "a.md", "output": "a.pdf"}, wantErr: "must end in .docx"},
		{name: "markdown without output", args: map[string]any{"markdown": "x"}, wantErr: "where to write the Word document"},
		{name: "not Markdown", files: map[string]string{"a.txt": "x"},
			args: map[string]any{"path": "a.txt"}, wantErr: "export_docx takes a Markdown file"},
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
			ws, err := OpenWorkspace(WorkspaceOptions{Dir: dir})
			require.NoError(t, err)
			t.Cleanup(func() { ws.Close() })

			out := runTool(t, toolOf(t)(NewExportDocxTool(ws, allowAll(), "letter")), tc.args)
			if tc.wantErr != "" {
				assert.Contains(t, errOf(out), tc.wantErr)
				return
			}
			require.Empty(t, errOf(out))
			assert.Equal(t, tc.wantOut, out["output"])
			data, err := os.ReadFile(filepath.Join(dir, tc.wantOut))
			require.NoError(t, err)
			assert.EqualValues(t, len(data), out["bytes"])
			z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			require.NoError(t, err, "a .docx is a zip")
			media := 0
			for _, f := range z.File {
				if strings.HasPrefix(f.Name, "word/media/") {
					media++
				}
			}
			assert.Equal(t, tc.images, media)
		})
	}
}

func TestExportDocxApproval(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "notes.md"), "# Notes")
	h, reqs := approverHooks(false)
	out := runTool(t, toolOf(t)(NewExportDocxTool(ws, h, "")), map[string]any{"path": "notes.md"})
	assert.NotEmpty(t, errOf(out))
	require.Len(t, *reqs, 1)
	assert.Equal(t, "export_docx", (*reqs)[0].Tool)
	assert.Equal(t, api.ActionWrite, (*reqs)[0].Kind)
	assert.Contains(t, (*reqs)[0].Detail, "Create notes.docx from notes.md (")
	assert.NoFileExists(t, filepath.Join(dir, "notes.docx"))
	assert.Equal(t, "a/b.docx", ExportDocxOutputPath("a/b.md", ""))
}
