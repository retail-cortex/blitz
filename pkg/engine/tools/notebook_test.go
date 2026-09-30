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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleNotebook = `{
 "cells": [
  {"cell_type": "markdown", "id": "intro", "metadata": {}, "source": ["# Sales\n", "By month."]},
  {"cell_type": "code", "execution_count": 3, "id": "load", "metadata": {"tags": ["setup"]},
   "outputs": [{"output_type": "stream", "name": "stdout", "text": ["loaded 12 rows\n"]},
               {"output_type": "execute_result", "execution_count": 3, "data": {"text/plain": ["42"], "image/png": "iVBOR"}, "metadata": {}}],
   "source": ["import pandas as pd\n", "df = pd.read_csv('sales.csv')"]},
  {"cell_type": "code", "execution_count": 4, "id": "plot", "metadata": {}, "outputs": [{"output_type": "error", "ename": "KeyError", "evalue": "'month'", "traceback": []}], "source": "df.plot(x='month')"}
 ],
 "metadata": {"kernelspec": {"language": "python", "name": "python3"}, "custom": {"ratio": 0.1}},
 "nbformat": 4,
 "nbformat_minor": 5
}`

func TestRenderNotebook(t *testing.T) {
	nb, err := parseNotebook([]byte(sampleNotebook))
	require.NoError(t, err)
	got := nb.render()
	for _, want := range []string{
		"## Cell 0 [markdown] id=intro\n# Sales\nBy month.",
		"## Cell 1 [code] id=load\n```python\nimport pandas as pd\ndf = pd.read_csv('sales.csv')\n```",
		"Output:\nloaded 12 rows", "Output:\n42",
		"Output:\nKeyError: 'month'",
	} {
		assert.Contains(t, got, want)
	}
	_, err = parseNotebook([]byte(`{"cells": 3}`))
	assert.ErrorContains(t, err, "no cells")
	_, err = parseNotebook([]byte(`nope`))
	assert.ErrorContains(t, err, "not a notebook")
}

func TestEditNotebook(t *testing.T) {
	cellsOf := func(t *testing.T, data []byte) []map[string]any {
		nb, err := parseNotebook(data)
		require.NoError(t, err)
		return nb.cells
	}
	tests := []struct {
		name  string
		in    NotebookEditInput
		check func(t *testing.T, cells []map[string]any, changed string)
		err   string
	}{
		{name: "replace a code cell clears its outputs", in: NotebookEditInput{Cell: "load", Action: "replace", Source: "df = load()\nprint(len(df))"},
			check: func(t *testing.T, c []map[string]any, changed string) {
				assert.Equal(t, "load", changed)
				assert.Equal(t, []any{"df = load()\n", "print(len(df))"}, c[1]["source"])
				assert.Equal(t, []any{}, c[1]["outputs"])
				assert.Nil(t, c[1]["execution_count"])
				assert.Equal(t, map[string]any{"tags": []any{"setup"}}, c[1]["metadata"], "metadata kept")
			}},
		{name: "replace by index", in: NotebookEditInput{Cell: "0", Action: "replace", Source: "# Revenue"},
			check: func(t *testing.T, c []map[string]any, _ string) {
				assert.Equal(t, []any{"# Revenue"}, c[0]["source"])
				assert.NotContains(t, c[0], "outputs")
			}},
		{name: "turn code into markdown", in: NotebookEditInput{Cell: "plot", Action: "replace", Source: "Plot later.", CellType: "markdown"},
			check: func(t *testing.T, c []map[string]any, _ string) {
				assert.Equal(t, "markdown", c[2]["cell_type"])
				assert.NotContains(t, c[2], "outputs")
			}},
		{name: "insert after a cell, with an id", in: NotebookEditInput{Cell: "intro", Action: "insert", Source: "x = 1"},
			check: func(t *testing.T, c []map[string]any, changed string) {
				require.Len(t, c, 4)
				assert.Equal(t, "code", c[1]["cell_type"])
				assert.Equal(t, changed, c[1]["id"])
				assert.Len(t, changed, 8)
				assert.Equal(t, "load", c[2]["id"])
			}},
		{name: "insert first", in: NotebookEditInput{Cell: "-1", Action: "insert", Source: "Notes", CellType: "markdown"},
			check: func(t *testing.T, c []map[string]any, _ string) {
				assert.Equal(t, "markdown", c[0]["cell_type"])
				assert.Equal(t, []any{"Notes"}, c[0]["source"])
			}},
		{name: "delete", in: NotebookEditInput{Cell: "plot", Action: "delete"},
			check: func(t *testing.T, c []map[string]any, _ string) { assert.Len(t, c, 2) }},
		{name: "no such cell", in: NotebookEditInput{Cell: "9", Action: "delete"}, err: "no cell 9"},
		{name: "a name that isn't there", in: NotebookEditInput{Cell: "chart", Action: "delete"}, err: `no cell "chart"`},
		{name: "a bad action", in: NotebookEditInput{Cell: "0", Action: "move"}, err: "replace, insert or delete"},
		{name: "a bad cell type", in: NotebookEditInput{Cell: "0", Action: "insert", CellType: "video"}, err: "code, markdown or raw"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, changed, _, err := editNotebook([]byte(sampleNotebook), tt.in)
			if tt.err != "" {
				assert.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			tt.check(t, cellsOf(t, out), changed)
			// Everything else is as it was: numbers and unknown fields.
			var doc map[string]any
			require.NoError(t, json.Unmarshal(out, &doc))
			assert.Equal(t, map[string]any{"ratio": 0.1}, doc["metadata"].(map[string]any)["custom"])
			assert.True(t, strings.HasSuffix(string(out), "}\n"))
			assert.Contains(t, string(out), "\n \"cells\": [", "Jupyter's one-space indent")
		})
	}
}

func TestNotebookTools(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sales.ipynb"), []byte(sampleNotebook), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.ipynb"), []byte("{oops"), 0o644))

	read := toolOf(t)(NewReadFileTool(ws))
	out := runTool(t, read, map[string]any{"path": "sales.ipynb"})
	assert.Contains(t, out["content"], "## Cell 1 [code] id=load")
	out = runTool(t, read, map[string]any{"path": "sales.ipynb", "start_line": 1, "end_line": 2})
	assert.Contains(t, out["content"], `"cells"`, "a line range reads the JSON")
	out = runTool(t, read, map[string]any{"path": "broken.ipynb"})
	assert.Contains(t, out["content"], "{oops", "one that doesn't parse is shown as it is")

	edit := toolOf(t)(NewNotebookEditTool(ws, allowAll()))
	out = runTool(t, edit, map[string]any{"path": "sales.ipynb", "cell": "plot", "action": "delete"})
	assert.Empty(t, errOf(out))
	assert.EqualValues(t, 2, out["cells"])
	data, _ := os.ReadFile(filepath.Join(dir, "sales.ipynb"))
	assert.NotContains(t, string(data), "df.plot")

	out = runTool(t, edit, map[string]any{"path": "notes.md", "cell": "0", "action": "delete"})
	assert.Contains(t, errOf(out), ".ipynb files")
	hooks, reqs := decisionHooks(0) // deny
	denied := toolOf(t)(NewNotebookEditTool(ws, hooks))
	out = runTool(t, denied, map[string]any{"path": "sales.ipynb", "cell": "0", "action": "delete"})
	assert.Contains(t, errOf(out), "not approved")
	require.Len(t, *reqs, 1)
	assert.Contains(t, (*reqs)[0].Diff, "-   \"cell_type\": \"markdown\"")
}
