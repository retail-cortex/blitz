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
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/charmap"
)

const bottle = `Sta_ID,Depthm,T_degC,Salnty
054.0 056.0,0,10.5,33.44
054.0 056.0,8,10.46,33.44
060.0 060.0,30,9.9,
060.0 060.0,50,8.2,33.6
`

func rowNumbers(p TablePage) []int {
	out := make([]int, len(p.Rows))
	for i, r := range p.Rows {
		out[i] = r.Number
	}
	return out
}

// A table file pages, searches, filters and sorts; its columns say their
// kind and, once described, what they hold; Latin-1 is decoded; what
// isn't a table, or isn't in the workspace, is refused.
func TestReadTable(t *testing.T) {
	latin1, err := charmap.Windows1252.NewEncoder().String("Station;Température\nNord;12,5\n")
	require.NoError(t, err)
	w, _ := openTestWith(t, func(c *config.Config) {
		write(t, c.Tools.WorkspaceDir, "data/bottle.csv", bottle)
		write(t, c.Tools.WorkspaceDir, "data/latin.csv", latin1)
		write(t, c.Tools.WorkspaceDir, "data/empty.tsv", "")
		write(t, c.Tools.WorkspaceDir, "notes.md", "# Notes\n")
		write(t, c.Tools.WorkspaceDir, "data/big.csv", bigTable(tableHead+100))
		write(t, c.Tools.WorkspaceDir, "data/binary.csv", "a,b\n\x00\x01,2\n")
		require.NoError(t, os.MkdirAll(filepath.Join(c.Tools.WorkspaceDir, "data", "folder.csv"), 0o755))
	})
	ctx := context.Background()

	p, err := w.ReadTable(ctx, TableQuery{Path: "data/bottle.csv", Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, rowNumbers(p))
	assert.Equal(t, 4, p.Matched)
	assert.Equal(t, 4, p.Total)
	require.Len(t, p.Columns, 4)
	assert.Equal(t, TableColumn{Name: "Depthm", Kind: "number", Min: 0, Max: 50}, p.Columns[1])
	assert.Equal(t, 1, p.Columns[3].Empty)

	p, err = w.ReadTable(ctx, TableQuery{Path: "data/bottle.csv", Search: "060", Filters: map[int]string{2: ">9"}})
	require.NoError(t, err)
	assert.Equal(t, []int{3}, rowNumbers(p))
	p, err = w.ReadTable(ctx, TableQuery{Path: "data/bottle.csv", SortColumn: 3, Desc: true})
	require.NoError(t, err)
	assert.True(t, p.Sorted)
	assert.Equal(t, []int{1, 2, 3, 4}, rowNumbers(p), "by temperature, warmest first")

	p, err = w.ReadTable(ctx, TableQuery{Path: "data/latin.csv"})
	require.NoError(t, err)
	assert.True(t, p.Latin1)
	assert.Equal(t, "Température", p.Columns[1].Name)

	p, err = w.ReadTable(ctx, TableQuery{Path: "data/big.csv", Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, strings.Count(bigTable(tableHead+100), "\n")-1, p.Total, "every row, not just the profiled head")
	assert.Equal(t, TableColumn{Name: "n", Kind: "number", Min: 1, Max: p.Columns[0].Max}, p.Columns[0])

	p, err = w.ReadTable(ctx, TableQuery{Path: "data/empty.tsv"})
	require.NoError(t, err)
	assert.Empty(t, p.Columns)

	w.closeSearch() // an index with the table described in its place
	x, err := search.Open(t.TempDir(), []search.Source{describedFiles{}}, nil, search.WithEnricher(describeColumns{}, 5, nil))
	require.NoError(t, err)
	require.NoError(t, x.Scan(ctx))
	root, err := os.OpenRoot(w.Dir())
	require.NoError(t, err)
	done := make(chan struct{})
	close(done)
	w.searchMu.Lock()
	w.search = &searchState{index: x, root: root, kick: make(chan struct{}, 1), stop: func() {}, done: done}
	w.searchMu.Unlock()
	p, err = w.ReadTable(ctx, TableQuery{Path: "data/bottle.csv"})
	require.NoError(t, err)
	assert.Equal(t, "water temperature, °C", p.Columns[2].Intent)

	for name, tc := range map[string]struct {
		q    TableQuery
		want error
	}{
		"not a table":  {TableQuery{Path: "notes.md"}, ErrNotTable},
		"outside":      {TableQuery{Path: "../x.csv"}, ErrBadPath},
		"missing":      {TableQuery{Path: "data/none.csv"}, fs.ErrNotExist},
		"binary":       {TableQuery{Path: "data/binary.csv"}, ErrNotTable},
		"a folder":     {TableQuery{Path: "data/folder.csv"}, nil},
		"a bad filter": {TableQuery{Path: "data/bottle.csv", Filters: map[int]string{1: ">deep"}}, ErrBadTableFilter},
	} {
		_, err := w.ReadTable(ctx, tc.q)
		if tc.want == nil {
			assert.Error(t, err, name)
			continue
		}
		assert.ErrorIs(t, err, tc.want, name)
	}
}

// bigTable is a two-column table of at least size bytes.
func bigTable(size int) string {
	var b strings.Builder
	b.WriteString("n,label\n")
	for i := 1; b.Len() < size; i++ {
		fmt.Fprintf(&b, "%d,row number %d\n", i, i)
	}
	return b.String()
}

// A table is indexed by its head, cut at a whole row; one that isn't
// text is skipped; what can't be read is an error.
func TestReadTableHead(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "big.csv", bigTable(tableIndexBytes+100))
	write(t, dir, "binary.csv", "a,b\n\x00\x01,2\n")
	write(t, dir, "small.tsv", "a\tb\n1\t2\n")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "folder.csv"), 0o755))
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	defer root.Close()

	d, err := readTableHead(root, "big.csv")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(d.Text), tableIndexBytes)
	assert.True(t, strings.HasSuffix(d.Text, "\n"), "cut at a whole row")
	d, err = readTableHead(root, "small.tsv")
	require.NoError(t, err)
	assert.Equal(t, "a\tb\n1\t2\n", d.Text)
	_, err = readTableHead(root, "binary.csv")
	assert.ErrorIs(t, err, search.ErrSkip)
	_, err = readTableHead(root, "folder.csv")
	assert.Error(t, err)
	_, err = readTableHead(root, "none.csv")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// describedFiles is a source with data/bottle.csv in it.
type describedFiles struct{}

func (describedFiles) Name() string { return api.SearchFiles }

func (describedFiles) List(context.Context) ([]search.Item, error) {
	return []search.Item{{Ref: "data/bottle.csv", Size: int64(len(bottle))}}, nil
}

func (describedFiles) Read(context.Context, string) (search.Doc, error) {
	return search.Doc{Title: "data/bottle.csv", Text: bottle}, nil
}

// describeColumns says what T_degC holds.
type describeColumns struct{}

func (describeColumns) Describe(context.Context, string, string) (search.Description, error) {
	return search.Description{Summary: "Bottle samples.", Columns: []search.ColumnNote{{Name: "T_degC", Intent: "water temperature, °C"}}}, nil
}
