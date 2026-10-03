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

package table

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Filter is a test on one column's cell: a comparison with a number
// (">5", ">=5", "<5", "<=5", "=5", "!=5", a range "10..20"), equality with
// text ("=north", "!=north"), "empty" or "!empty", else that the cell
// contains the text, ignoring case.
type Filter struct {
	op       string // > >= < <= = != .. empty !empty contains
	text     string
	num, hi  float64
	isNumber bool
}

// ParseFilter reads a column filter.
func ParseFilter(expr string) (Filter, error) {
	e := strings.TrimSpace(expr)
	switch strings.ToLower(e) {
	case "":
		return Filter{}, errors.New("an empty filter")
	case "empty":
		return Filter{op: "empty"}, nil
	case "!empty":
		return Filter{op: "!empty"}, nil
	}
	if lo, hi, ok := strings.Cut(e, ".."); ok {
		a, okA := number(lo)
		b, okB := number(hi)
		if !okA || !okB {
			return Filter{}, fmt.Errorf("a range is two numbers: %q", expr)
		}
		return Filter{op: "..", num: min(a, b), hi: max(a, b)}, nil
	}
	for _, op := range []string{">=", "<=", "!=", ">", "<", "="} {
		rest, ok := strings.CutPrefix(e, op)
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		f := Filter{op: op, text: strings.ToLower(rest)}
		f.num, f.isNumber = number(rest)
		if !f.isNumber && op != "=" && op != "!=" {
			return Filter{}, fmt.Errorf("%s compares numbers: %q", op, expr)
		}
		return f, nil
	}
	return Filter{op: "contains", text: strings.ToLower(e)}, nil
}

// Match reports whether a cell passes the filter.
func (f Filter) Match(cell string) bool {
	v := strings.TrimSpace(cell)
	switch f.op {
	case "empty":
		return v == ""
	case "!empty":
		return v != ""
	case "contains":
		return strings.Contains(strings.ToLower(v), f.text)
	case "=", "!=":
		eq := strings.EqualFold(v, f.text)
		if n, ok := number(v); ok && f.isNumber {
			eq = n == f.num
		}
		return eq == (f.op == "=")
	}
	n, ok := number(v)
	if !ok {
		return false
	}
	switch f.op {
	case ">":
		return n > f.num
	case ">=":
		return n >= f.num
	case "<":
		return n < f.num
	case "<=":
		return n <= f.num
	case "..":
		return n >= f.num && n <= f.hi
	}
	return false
}

// Query asks for a page of a table's rows: those with Search in a cell
// (ignoring case) and passing every Filter (by column), sorted by a
// column (-1: in file order), from Offset, up to Limit.
type Query struct {
	Search  string
	Filters map[int]string
	Sort    int
	Desc    bool
	Offset  int
	Limit   int
}

// Row is a row of a table: its number (1-based, the header not counted)
// and its cells.
type Row struct {
	Number int
	Cells  []string
}

// Page is a page of a table: its columns, the rows asked for, how many
// rows matched and how many there are in all. Sorted is false when there
// were too many matches to sort (more than SortLimit).
type Page struct {
	Columns []string
	Rows    []Row
	Matched int
	Total   int
	Sorted  bool
}

// SortLimit is the most matching rows a page sorts: past it, the rows come
// in file order (filter first).
var SortLimit = 200_000

// ErrBadFilter wraps a filter that can't be read.
var ErrBadFilter = errors.New("bad filter")

// Read reads r to the end for q's page; ctx stops it between rows.
func Read(ctx context.Context, r *csv.Reader, q Query) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	header, err := r.Read()
	if err != nil {
		return Page{}, err
	}
	p := Page{Columns: header, Sorted: q.Sort >= 0}
	filters := map[int]Filter{}
	for col, expr := range q.Filters {
		f, err := ParseFilter(expr)
		if err != nil {
			return Page{}, fmt.Errorf("%w on %s: %w", ErrBadFilter, columnName(header, col), err)
		}
		filters[col] = f
	}
	search := strings.ToLower(strings.TrimSpace(q.Search))
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	var sorting []Row
	for n := 1; ; n++ {
		if n%4096 == 0 && ctx.Err() != nil {
			return Page{}, ctx.Err()
		}
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				continue // a malformed row is skipped, not the table
			}
			return Page{}, err
		}
		p.Total++
		if !matches(rec, search, filters) {
			continue
		}
		p.Matched++
		switch {
		case p.Sorted && p.Matched <= SortLimit:
			sorting = append(sorting, Row{Number: n, Cells: rec})
		case p.Sorted: // too many to sort: file order, from the first match
			p.Sorted = false
			from := min(q.Offset, len(sorting))
			p.Rows = append(p.Rows, sorting[from:min(from+limit, len(sorting))]...)
			sorting = nil
		}
		if !p.Sorted && p.Matched > q.Offset && len(p.Rows) < limit {
			p.Rows = append(p.Rows, Row{Number: n, Cells: rec})
		}
	}
	if p.Sorted {
		sortRows(sorting, q.Sort, q.Desc)
		from := min(q.Offset, len(sorting))
		p.Rows = sorting[from:min(from+limit, len(sorting))]
	}
	return p, nil
}

func columnName(header []string, col int) string {
	if col >= 0 && col < len(header) {
		return header[col]
	}
	return "column " + strconv.Itoa(col+1)
}

func matches(rec []string, search string, filters map[int]Filter) bool {
	for col, f := range filters {
		cell := ""
		if col < len(rec) {
			cell = rec[col]
		}
		if !f.Match(cell) {
			return false
		}
	}
	if search == "" {
		return true
	}
	for _, c := range rec {
		if strings.Contains(strings.ToLower(c), search) {
			return true
		}
	}
	return false
}

// sortRows sorts rows by a column: as numbers when both cells are, else as
// text ignoring case; empty cells last; ties in file order.
func sortRows(rows []Row, col int, desc bool) {
	cell := func(r Row) string {
		if col < len(r.Cells) {
			return strings.TrimSpace(r.Cells[col])
		}
		return ""
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := cell(rows[i]), cell(rows[j])
		if a == "" || b == "" {
			return a != "" && b == ""
		}
		var less, greater bool
		if x, ok := number(a); ok {
			if y, ok := number(b); ok {
				less, greater = x < y, x > y
			}
		}
		if !less && !greater {
			la, lb := strings.ToLower(a), strings.ToLower(b)
			if _, ok := number(a); !ok || la != lb {
				less, greater = la < lb, la > lb
			}
		}
		if desc {
			return greater
		}
		return less
	})
}
