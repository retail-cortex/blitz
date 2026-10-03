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

package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/retail-cortex/blitz/pkg/engine/table"
)

// ErrNotTable is returned for a file the table viewer doesn't read.
var ErrNotTable = errors.New("not a table (CSV or TSV)")

// ErrBadTableFilter is returned for a column filter that can't be read.
var ErrBadTableFilter = table.ErrBadFilter

// TableQuery asks for a page of a table file's rows: those with Search in
// a cell and passing each column's filter, sorted by a column (SortColumn
// 1-based; 0: file order), from Offset, up to Limit (0: 100).
type TableQuery struct {
	Path       string
	Search     string
	Filters    map[int]string
	SortColumn int
	Desc       bool
	Offset     int
	Limit      int
}

// TableColumn is a table's column: its name, what its first rows look
// like (kind, empty cells, a number column's range), and what a model said
// it holds (search.enrich), if it has.
type TableColumn struct {
	Name     string
	Kind     string
	Empty    int
	Min, Max float64
	Intent   string
}

// TablePage is a page of a table: its columns, the rows asked for (each
// with its number, 1-based), how many matched and how many there are.
// Sorted is false when a sort was asked for but too many rows matched.
type TablePage struct {
	Columns []TableColumn
	Rows    []table.Row
	Matched int
	Total   int
	Sorted  bool
	Latin1  bool
}

// tableHead is how much of a table is read to tell its format and profile
// its columns.
const tableHead = 256 << 10

// ReadTable reads a page of a CSV or TSV file in the workspace (the table
// viewer, spec_files_029): streamed, so a file of any size can be paged,
// searched, filtered and sorted.
func (w *Workspace) ReadTable(ctx context.Context, q TableQuery) (TablePage, error) {
	rel, slash, err := userPath(q.Path)
	if err != nil {
		return TablePage{}, err
	}
	if !table.IsTable(slash) {
		return TablePage{}, fmt.Errorf("%w: %s", ErrNotTable, slash)
	}
	root, err := os.OpenRoot(w.Dir())
	if err != nil {
		return TablePage{}, err
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return TablePage{}, err
	}
	defer f.Close()
	head := make([]byte, tableHead)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return TablePage{}, err
	}
	format, err := table.Sniff(slash, head[:n])
	if err != nil {
		return TablePage{}, fmt.Errorf("%w: %s isn't text", ErrNotTable, slash)
	}
	whole := head[:n]
	if n == tableHead { // profile whole rows
		if i := bytes.LastIndexByte(whole, '\n'); i > 0 {
			whole = whole[:i+1]
		}
	}
	profile, _, _ := table.Profile(table.Reader(bytes.NewReader(whole), format), tableProfileRows)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return TablePage{}, err
	}
	page, err := table.Read(ctx, table.Reader(f, format), table.Query{
		Search: q.Search, Filters: q.Filters, Sort: q.SortColumn - 1, Desc: q.Desc, Offset: q.Offset, Limit: q.Limit,
	})
	if errors.Is(err, io.EOF) {
		return TablePage{Latin1: format.Latin1}, nil // an empty file: no header
	}
	if err != nil {
		return TablePage{}, err
	}
	intents := map[string]string{}
	if st := w.searchNow(); st != nil {
		if notes, err := st.index.Columns(ctx, "files", slash); err == nil {
			for _, c := range notes {
				intents[c.Name] = c.Intent
			}
		}
	}
	out := TablePage{Rows: page.Rows, Matched: page.Matched, Total: page.Total, Sorted: page.Sorted, Latin1: format.Latin1}
	for i, name := range page.Columns {
		c := TableColumn{Name: name, Intent: intents[name]}
		if i < len(profile) {
			p := profile[i]
			c.Name, c.Kind, c.Empty, c.Min, c.Max = p.Name, p.Kind, p.Empty, p.Min, p.Max
			c.Intent = intents[p.Name]
		}
		out.Columns = append(out.Columns, c)
	}
	return out, nil
}
