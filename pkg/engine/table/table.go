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

// Package table reads delimited tables (CSV, TSV) for the desktop app's
// table viewer and workspace search (spec_files_029, spec_search_035):
// their delimiter and encoding (Latin-1 is decoded), a profile of their
// columns, and pages of rows matched by a search across columns, filters
// on columns and a sort, streamed so a file of hundreds of megabytes isn't
// loaded whole.
package table

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// IsTable reports whether a file is read as a table, by its name.
func IsTable(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".csv", ".tsv", ".tab":
		return true
	}
	return false
}

// Format is how a table's bytes are read: its delimiter, and whether it's
// Latin-1 (Windows-1252) rather than UTF-8.
type Format struct {
	Delimiter rune
	Latin1    bool
}

// Sniff decides a table's format from its name and first bytes: TSV by
// name, else the delimiter its header line has most of (comma, semicolon,
// tab, bar); Latin-1 when the bytes aren't UTF-8. A NUL means it's no text.
func Sniff(name string, head []byte) (Format, error) {
	if bytes.IndexByte(head, 0) >= 0 {
		return Format{}, errors.New("not text")
	}
	f := Format{Delimiter: ',', Latin1: !validUTF8(head)}
	ext := strings.ToLower(path.Ext(name))
	if ext == ".tsv" || ext == ".tab" {
		f.Delimiter = '\t'
		return f, nil
	}
	line, _, _ := bytes.Cut(head, []byte("\n"))
	best := 0
	for _, d := range []rune{',', ';', '\t', '|'} {
		if n := bytes.Count(line, []byte(string(d))); n > best {
			best, f.Delimiter = n, d
		}
	}
	return f, nil
}

// validUTF8 reports whether head is UTF-8, forgiving a character cut at
// its end (head is a file's first bytes).
func validUTF8(head []byte) bool {
	for cut := 0; cut < utf8.UTFMax && cut <= len(head); cut++ {
		if utf8.Valid(head[:len(head)-cut]) {
			return true
		}
	}
	return false
}

// Reader reads r's records as f says, decoding Latin-1; quotes are
// lenient and rows may have any number of fields.
func Reader(r io.Reader, f Format) *csv.Reader {
	if f.Latin1 {
		r = charmap.Windows1252.NewDecoder().Reader(r)
	}
	cr := csv.NewReader(bufio.NewReaderSize(r, 1<<16))
	cr.Comma = f.Delimiter
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = false
	return cr
}

// Decode is head as text: Latin-1 decoded when f says so.
func Decode(head []byte, f Format) string {
	if !f.Latin1 {
		return string(head)
	}
	s, err := charmap.Windows1252.NewDecoder().Bytes(head)
	if err != nil {
		return string(head)
	}
	return string(s)
}

// The kinds of column a profile tells apart.
const (
	KindNumber = "number"
	KindDate   = "date"
	KindText   = "text"
	KindEmpty  = "empty"
)

// Column is a column's name and what its values look like in the rows
// profiled: its kind, how many were empty, the range of a number column,
// and a few distinct values.
type Column struct {
	Name     string
	Kind     string
	Empty    int
	Min, Max float64
	Samples  []string
}

// maxSamples is how many distinct values a profile keeps per column.
const maxSamples = 5

// dateLayouts are the dates a column may hold to count as dates.
var dateLayouts = []string{time.DateOnly, time.RFC3339, "2006-01-02 15:04:05", "01/02/2006", "2006/01/02", "02/01/2006"}

func isDate(s string) bool {
	for _, l := range dateLayouts {
		if _, err := time.Parse(l, s); err == nil {
			return true
		}
	}
	return false
}

// number parses a cell as a number ("" and text aren't).
func number(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f, err == nil && !math.IsNaN(f)
}

// Profile reads the header and up to rows records of r and describes
// each column, and how many records it read.
func Profile(r *csv.Reader, rows int) ([]Column, int, error) {
	header, err := r.Read()
	if err != nil {
		return nil, 0, err
	}
	cols := make([]Column, len(header))
	nums, dates, texts := make([]int, len(header)), make([]int, len(header)), make([]int, len(header))
	for i, h := range header {
		cols[i] = Column{Name: strings.TrimSpace(strings.TrimPrefix(h, "\uFEFF")), Min: math.Inf(1), Max: math.Inf(-1)}
	}
	n := 0
	for ; n < rows; n++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, n, err
		}
		for i := range cols {
			v := ""
			if i < len(rec) {
				v = strings.TrimSpace(rec[i])
			}
			c := &cols[i]
			switch f, ok := number(v); {
			case v == "":
				c.Empty++
				continue
			case ok:
				nums[i]++
				c.Min, c.Max = min(c.Min, f), max(c.Max, f)
			case isDate(v):
				dates[i]++
			default:
				texts[i]++
			}
			if len(c.Samples) < maxSamples && !slices.Contains(c.Samples, v) {
				c.Samples = append(c.Samples, v)
			}
		}
	}
	for i := range cols {
		c := &cols[i]
		switch {
		case nums[i]+dates[i]+texts[i] == 0:
			c.Kind = KindEmpty
		case texts[i] == 0 && dates[i] == 0:
			c.Kind = KindNumber
		case texts[i] == 0 && nums[i] == 0:
			c.Kind = KindDate
		default:
			c.Kind = KindText
		}
		if c.Kind != KindNumber {
			c.Min, c.Max = 0, 0
		}
	}
	return cols, n, nil
}

// Describe writes a profile as text, a line per column, for a model to
// read (search.enrich).
func Describe(cols []Column, rows int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d columns; %d rows profiled.\n", len(cols), rows)
	for _, c := range cols {
		fmt.Fprintf(&b, "- %s (%s", c.Name, c.Kind)
		if c.Kind == KindNumber {
			fmt.Fprintf(&b, ", %g to %g", c.Min, c.Max)
		}
		if c.Empty > 0 {
			fmt.Fprintf(&b, ", %d empty", c.Empty)
		}
		b.WriteString(")")
		if len(c.Samples) > 0 {
			fmt.Fprintf(&b, ": %s", strings.Join(c.Samples, " | "))
		}
		b.WriteString("\n")
	}
	return b.String()
}
