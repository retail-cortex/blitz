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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/retail-cortex/blitz/pkg/textutil"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Jupyter notebooks (spec_parity_027 PAR-TOOL-02): notebook_edit replaces,
// inserts or deletes a cell, keeping the rest of the JSON as it was
// (unknown fields, numbers as written); read_file shows a notebook as its
// cells with their outputs.

// notebook is a parsed .ipynb: its top-level fields, cells included.
type notebook struct {
	doc   map[string]any
	cells []map[string]any
}

func parseNotebook(data []byte) (*notebook, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("not a notebook (JSON): %w", err)
	}
	raw, ok := doc["cells"].([]any)
	if !ok {
		return nil, errors.New("not a notebook: no cells")
	}
	nb := &notebook{doc: doc}
	for i, c := range raw {
		m, ok := c.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("cell %d isn't an object", i)
		}
		nb.cells = append(nb.cells, m)
	}
	return nb, nil
}

// encode writes the notebook as Jupyter does: keys sorted, one-space
// indent, a final newline.
func (nb *notebook) encode() ([]byte, error) {
	cells := make([]any, len(nb.cells))
	for i, c := range nb.cells {
		cells[i] = c
	}
	nb.doc["cells"] = cells
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(nb.doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// needsIDs reports whether cells carry ids (nbformat 4.5 and later).
func (nb *notebook) needsIDs() bool {
	major, _ := nb.doc["nbformat"].(json.Number).Int64()
	minor, _ := nb.doc["nbformat_minor"].(json.Number).Int64()
	return major > 4 || major == 4 && minor >= 5
}

// language is the notebook's kernel language, for code fences.
func (nb *notebook) language() string {
	if meta, ok := nb.doc["metadata"].(map[string]any); ok {
		if li, ok := meta["language_info"].(map[string]any); ok {
			if name, ok := li["name"].(string); ok {
				return name
			}
		}
		if ks, ok := meta["kernelspec"].(map[string]any); ok {
			if name, ok := ks["language"].(string); ok {
				return name
			}
		}
	}
	return "python"
}

// find is the index of the cell a reference names: its id, or its index
// (0-based, as the rendering shows).
func (nb *notebook) find(ref string) (int, error) {
	for i, c := range nb.cells {
		if id, _ := c["id"].(string); id != "" && id == ref {
			return i, nil
		}
	}
	var i int
	if _, err := fmt.Sscanf(ref, "%d", &i); err == nil && fmt.Sprint(i) == strings.TrimSpace(ref) {
		if i < 0 || i >= len(nb.cells) {
			return 0, fmt.Errorf("no cell %d: the notebook has %d", i, len(nb.cells))
		}
		return i, nil
	}
	return 0, fmt.Errorf("no cell %q (a cell id, or an index from 0)", ref)
}

// sourceLines is text as a notebook stores it: lines, each but the last
// ending in a newline.
func sourceLines(text string) []any {
	var out []any
	for line := range strings.Lines(text) {
		out = append(out, line)
	}
	if out == nil {
		out = []any{}
	}
	return out
}

// text is a cell field (source, a stream's text) as one string.
func text(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, s := range t {
			if str, ok := s.(string); ok {
				b.WriteString(str)
			}
		}
		return b.String()
	}
	return ""
}

func newCellID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// render is the notebook as its cells with their outputs, for read_file.
func (nb *notebook) render() string {
	var b strings.Builder
	lang := nb.language()
	for i, c := range nb.cells {
		kind, _ := c["cell_type"].(string)
		id, _ := c["id"].(string)
		fmt.Fprintf(&b, "## Cell %d [%s]", i, kind)
		if id != "" {
			fmt.Fprintf(&b, " id=%s", id)
		}
		b.WriteString("\n")
		src := strings.TrimRight(text(c["source"]), "\n")
		if kind == "code" {
			fmt.Fprintf(&b, "```%s\n%s\n```\n", lang, src)
		} else {
			fmt.Fprintf(&b, "%s\n", src)
		}
		outputs, _ := c["outputs"].([]any)
		for _, o := range outputs {
			if s := renderOutput(o); s != "" {
				fmt.Fprintf(&b, "Output:\n%s\n", textutil.Ellipsize(strings.TrimRight(s, "\n"), 2000))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderOutput(o any) string {
	m, ok := o.(map[string]any)
	if !ok {
		return ""
	}
	switch m["output_type"] {
	case "stream":
		return text(m["text"])
	case "error":
		return fmt.Sprintf("%v: %v", m["ename"], m["evalue"])
	case "execute_result", "display_data":
		data, _ := m["data"].(map[string]any)
		if t := text(data["text/plain"]); t != "" {
			return t
		}
		for mime := range data {
			if strings.HasPrefix(mime, "image/") {
				return "[" + mime + " image]"
			}
		}
	}
	return ""
}

// readNotebook is a notebook as its cells (ok false when it doesn't parse:
// read_file then shows its JSON).
func readNotebook(ws *Workspace, path string) (ReadFileOutput, bool) {
	rel, err := ws.Rel(path)
	if err != nil {
		return ReadFileOutput{}, false
	}
	data, err := ws.ReadFileLimit(rel, ws.MaxFileSize())
	if err != nil {
		return ReadFileOutput{}, false
	}
	nb, err := parseNotebook(data)
	if err != nil {
		return ReadFileOutput{}, false
	}
	content := nb.render()
	out := ReadFileOutput{Path: path, Content: content, TotalLines: strings.Count(content, "\n")}
	if len(content) > maxReadOutputBytes {
		out.Content, out.Truncated = textutil.TruncateUTF8(content, maxReadOutputBytes), true
	}
	return out, true
}

// NotebookEditInput is what notebook_edit takes.
type NotebookEditInput struct {
	Path     string `json:"path" jsonschema:"The notebook (.ipynb)"`
	Cell     string `json:"cell" jsonschema:"The cell: its id, or its index from 0 (as read_file shows them); for insert, the new cell goes after it (-1: first)"`
	Action   string `json:"action" jsonschema:"replace (the cell's source), insert (a new cell after cell), or delete"`
	Source   string `json:"source,omitempty" jsonschema:"The cell's new source (replace, insert)"`
	CellType string `json:"cell_type,omitempty" jsonschema:"code or markdown (insert; replace keeps the cell's unless given)"`
}

// NotebookEditOutput is what it returns.
type NotebookEditOutput struct {
	Path  string `json:"path"`
	Cell  string `json:"cell,omitempty"` // the cell changed or made (its id, or index)
	Cells int    `json:"cells,omitempty"`
	Error string `json:"error,omitempty"`
}

// editNotebook applies in to data and says which cell it changed.
func editNotebook(data []byte, in NotebookEditInput) ([]byte, string, int, error) {
	nb, err := parseNotebook(data)
	if err != nil {
		return nil, "", 0, err
	}
	kind := strings.ToLower(strings.TrimSpace(in.CellType))
	if kind != "" && kind != "code" && kind != "markdown" && kind != "raw" {
		return nil, "", 0, fmt.Errorf("cell_type %q: code, markdown or raw", in.CellType)
	}
	label := func(i int) string {
		if id, _ := nb.cells[i]["id"].(string); id != "" {
			return id
		}
		return fmt.Sprint(i)
	}
	var changed string
	switch strings.ToLower(in.Action) {
	case "replace":
		i, err := nb.find(in.Cell)
		if err != nil {
			return nil, "", 0, err
		}
		c := nb.cells[i]
		c["source"] = sourceLines(in.Source)
		if kind != "" && kind != c["cell_type"] {
			c["cell_type"] = kind
			if kind != "code" {
				delete(c, "outputs")
				delete(c, "execution_count")
			}
		}
		if c["cell_type"] == "code" { // its outputs were the old code's
			c["outputs"] = []any{}
			c["execution_count"] = nil
		}
		changed = label(i)
	case "insert":
		at := 0
		if strings.TrimSpace(in.Cell) != "-1" {
			i, err := nb.find(in.Cell)
			if err != nil {
				return nil, "", 0, err
			}
			at = i + 1
		}
		if kind == "" {
			kind = "code"
		}
		c := map[string]any{"cell_type": kind, "metadata": map[string]any{}, "source": sourceLines(in.Source)}
		if kind == "code" {
			c["outputs"], c["execution_count"] = []any{}, nil
		}
		if nb.needsIDs() {
			c["id"] = newCellID()
		}
		nb.cells = append(nb.cells[:at], append([]map[string]any{c}, nb.cells[at:]...)...)
		changed = label(at)
	case "delete":
		i, err := nb.find(in.Cell)
		if err != nil {
			return nil, "", 0, err
		}
		changed = label(i)
		nb.cells = append(nb.cells[:i], nb.cells[i+1:]...)
	default:
		return nil, "", 0, fmt.Errorf("action %q: replace, insert or delete", in.Action)
	}
	out, err := nb.encode()
	return out, changed, len(nb.cells), err
}

// NewNotebookEditTool is notebook_edit: a Jupyter cell replaced, inserted
// or deleted, through the file sandbox, approvals (with the diff) and
// checkpoints, as other edits.
func NewNotebookEditTool(ws *Workspace, hooks *Hooks) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "notebook_edit",
			Description: "Edit a Jupyter notebook: replace a cell's source, insert a cell, or delete one. Cells are named by id or by index from 0, as read_file shows them. Replacing a code cell clears its outputs.",
		},
		func(ctx agent.Context, in NotebookEditInput) (NotebookEditOutput, error) {
			fail := func(err error) (NotebookEditOutput, error) {
				return NotebookEditOutput{Path: in.Path, Error: err.Error()}, nil
			}
			if !strings.HasSuffix(strings.ToLower(in.Path), ".ipynb") {
				return fail(errors.New("notebook_edit edits .ipynb files; use replace_in_file for others"))
			}
			rel, err := ws.WritablePath(in.Path)
			if err != nil {
				return fail(err)
			}
			unlock, err := ws.lockPaths(ctx, rel)
			if err != nil {
				return fail(err)
			}
			defer unlock()
			data, err := ws.ReadFile(rel)
			if err != nil {
				return fail(fmt.Errorf("failed to read the notebook: %w", err))
			}
			updated, cell, cells, err := editNotebook(data, in)
			if err != nil {
				return fail(err)
			}
			if err := hooks.Approve(ctx, writeApproval(ws, "notebook_edit",
				fmt.Sprintf("%s cell %s of %s", in.Action, cell, rel), unifiedDiff(rel, string(data), string(updated)), rel)); err != nil {
				return fail(err)
			}
			if err := ws.unchanged(rel, true, data); err != nil {
				return fail(err)
			}
			if err := ws.WriteFileAtomic(ctx, rel, updated); err != nil {
				return fail(fmt.Errorf("failed to write the notebook: %w", err))
			}
			return NotebookEditOutput{Path: in.Path, Cell: cell, Cells: cells}, nil
		})
}
