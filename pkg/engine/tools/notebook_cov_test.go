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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Rendering uses the notebook's language, shows images and empty or odd
// outputs sensibly, and labels cells without ids by index.
func TestRenderNotebookVariants(t *testing.T) {
	nb, err := parseNotebook([]byte(`{"cells": [
  {"cell_type": "code", "source": 7, "outputs": [
    "not an output",
    {"output_type": "display_data", "data": {"image/svg+xml": "<svg/>"}},
    {"output_type": "display_data", "data": {"application/json": {}}},
    {"output_type": "unknown"}
  ]}
 ], "metadata": {"language_info": {"name": "julia"}}}`))
	require.NoError(t, err)
	got := nb.render()
	assert.Contains(t, got, "## Cell 0 [code]\n```julia\n\n```")
	assert.Contains(t, got, "Output:\n[image/svg+xml image]")
	assert.Equal(t, 1, strings.Count(got, "Output:"), "outputs without text were shown:\n%s", got)

	nb, err = parseNotebook([]byte(`{"cells": [], "metadata": {}}`))
	require.NoError(t, err)
	assert.Equal(t, "python", nb.language(), "the default language")
	_, err = parseNotebook([]byte(`{"cells": [3]}`))
	assert.ErrorContains(t, err, "cell 0 isn't an object")
}

// Edits that can't apply say why; a cell without an id is named by its
// index; an empty source is stored as no lines.
func TestEditNotebookMore(t *testing.T) {
	plain := []byte(`{"cells": [{"cell_type": "code", "source": "x", "outputs": []}], "metadata": {}, "nbformat": 4, "nbformat_minor": 4}`)
	cases := map[string]struct {
		data []byte
		in   NotebookEditInput
		err  string
	}{
		"not a notebook":     {[]byte("nope"), NotebookEditInput{Action: "delete"}, "not a notebook"},
		"replace a missing":  {plain, NotebookEditInput{Cell: "5", Action: "replace"}, "no cell 5"},
		"insert after none":  {plain, NotebookEditInput{Cell: "zz", Action: "insert"}, `no cell "zz"`},
		"negative elsewhere": {plain, NotebookEditInput{Cell: "-2", Action: "delete"}, "no cell -2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := editNotebook(c.data, c.in)
			assert.ErrorContains(t, err, c.err)
		})
	}

	out, changed, cells, err := editNotebook(plain, NotebookEditInput{Cell: "0", Action: "insert", Source: ""})
	require.NoError(t, err)
	assert.Equal(t, "1", changed, "a cell without an id is named by its index")
	assert.Equal(t, 2, cells)
	nb, err := parseNotebook(out)
	require.NoError(t, err)
	assert.Equal(t, []any{}, nb.cells[1]["source"])
	assert.NotContains(t, nb.cells[1], "id", "nbformat 4.4 has no cell ids")
}

// A notebook too big to show whole is truncated when read.
func TestReadNotebookTruncated(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	big := strings.Repeat("x", maxReadOutputBytes+10)
	writeFile(t, filepath.Join(dir, "big.ipynb"), `{"cells": [{"cell_type": "markdown", "source": "`+big+`"}]}`)
	out := runTool(t, toolOf(t)(NewReadFileTool(ws)), map[string]any{"path": "big.ipynb"})
	assert.Equal(t, true, out["truncated"])
	assert.Contains(t, out["content"], "## Cell 0 [markdown]")

	// Outside the workspace, or missing: read_file's own error.
	out = runTool(t, toolOf(t)(NewReadFileTool(ws)), map[string]any{"path": "../x.ipynb"})
	assert.NotEmpty(t, errOf(out))
	out = runTool(t, toolOf(t)(NewReadFileTool(ws)), map[string]any{"path": "missing.ipynb"})
	assert.NotEmpty(t, errOf(out))
}

// notebook_edit reports each way an edit can fail before or while writing.
func TestNotebookEditFailures(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	writeFile(t, filepath.Join(dir, "nb.ipynb"), sampleNotebook)
	writeFile(t, filepath.Join(dir, "bad.ipynb"), "{oops")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "dir.ipynb"), 0o755))
	edit := toolOf(t)(NewNotebookEditTool(ws, allowAll()))
	cases := map[string]struct {
		args map[string]any
		want string
	}{
		"outside":        {map[string]any{"path": "../nb.ipynb", "cell": "0", "action": "delete"}, ""},
		"missing":        {map[string]any{"path": "missing.ipynb", "cell": "0", "action": "delete"}, "failed to read the notebook"},
		"a directory":    {map[string]any{"path": "dir.ipynb", "cell": "0", "action": "delete"}, "failed to read the notebook"},
		"not a notebook": {map[string]any{"path": "bad.ipynb", "cell": "0", "action": "delete"}, "not a notebook"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out := runTool(t, edit, c.args)
			assert.NotEmpty(t, errOf(out))
			assert.Contains(t, errOf(out), c.want)
		})
	}

	t.Run("locked and cancelled", func(t *testing.T) {
		unlock, err := ws.lockPaths(context.Background(), "nb.ipynb")
		require.NoError(t, err)
		defer unlock()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		out, err := edit.Run(createTestToolContextWith(ctx), map[string]any{"path": "nb.ipynb", "cell": "0", "action": "delete"})
		require.NoError(t, err)
		assert.Contains(t, errOf(out), "canceled")
	})

	t.Run("changed while asked", func(t *testing.T) {
		h := NewHooks(Policy{})
		h.SetApprover(func(ctx context.Context, req api.ApprovalRequest) (api.Decision, error) {
			writeFile(t, filepath.Join(dir, "nb.ipynb"), sampleNotebook+" ")
			return api.DecisionOnce, nil
		})
		out := runTool(t, toolOf(t)(NewNotebookEditTool(ws, h)), map[string]any{"path": "nb.ipynb", "cell": "0", "action": "delete"})
		assert.Contains(t, errOf(out), "changed while waiting")
	})

	t.Run("not writable", func(t *testing.T) {
		ro := filepath.Join(dir, "ro")
		writeFile(t, filepath.Join(ro, "nb.ipynb"), sampleNotebook)
		require.NoError(t, os.Chmod(ro, 0o555))
		t.Cleanup(func() { os.Chmod(ro, 0o755) })
		out := runTool(t, edit, map[string]any{"path": "ro/nb.ipynb", "cell": "0", "action": "delete"})
		assert.Contains(t, errOf(out), "failed to write the notebook")
	})
}

// A notebook whose nbformat is missing or not a number is read as an old
// one (cells without ids), not a panic.
func TestNotebookNeedsIDsOddFormat(t *testing.T) {
	cases := map[string]struct {
		doc  string
		want bool
	}{
		"missing":    {`{"cells": []}`, false},
		"a string":   {`{"cells": [], "nbformat": "4", "nbformat_minor": "5"}`, false},
		"4.5":        {`{"cells": [], "nbformat": 4, "nbformat_minor": 5}`, true},
		"4.4":        {`{"cells": [], "nbformat": 4, "nbformat_minor": 4}`, false},
		"no minor 5": {`{"cells": [], "nbformat": 5}`, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			nb, err := parseNotebook([]byte(tc.doc))
			require.NoError(t, err)
			assert.Equal(t, tc.want, nb.needsIDs())
		})
	}
}
