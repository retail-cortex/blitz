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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/charmap"
)

const ocean = `Sta_ID,Depthm,T_degC,Salnty,Date,Note
054.0 056.0,0,10.5,33.44,1949-03-01,surface
054.0 056.0,8,10.46,33.44,1949-03-01,
054.0 056.0,10,10.46,,1949-03-01,thermocline
060.0 060.0,30,9.9,33.5,1951-07-12,deep
060.0 060.0,50,,33.6,1951-07-12,Deep cast
`

func TestSniff(t *testing.T) {
	latin1, err := charmap.Windows1252.NewEncoder().String("Name;Café\nx;é\n")
	require.NoError(t, err)
	for _, tc := range []struct {
		name, head string
		want       Format
		err        bool
	}{
		{"a.csv", ocean, Format{Delimiter: ','}, false},
		{"a.tsv", "a,b\tc\n", Format{Delimiter: '\t'}, false},
		{"a.csv", "a;b;c\n1;2;3\n", Format{Delimiter: ';'}, false},
		{"a.csv", "a|b|c\n", Format{Delimiter: '|'}, false},
		{"a.csv", latin1, Format{Delimiter: ';', Latin1: true}, false},
		{"a.csv", "a,b\n\xc3", Format{Delimiter: ','}, false}, // a cut é is still UTF-8
		{"a.csv", "a\x00b", Format{}, true},
	} {
		got, err := Sniff(tc.name, []byte(tc.head))
		if tc.err {
			assert.Error(t, err, tc.head)
			continue
		}
		require.NoError(t, err, tc.head)
		assert.Equal(t, tc.want, got, "%q", tc.head)
	}
	assert.Equal(t, "Name;Café\nx;é\n", Decode([]byte(latin1), Format{Latin1: true}))
	assert.Equal(t, "plain", Decode([]byte("plain"), Format{}))
	f := Format{Delimiter: ';', Latin1: true}
	rec, err := Reader(strings.NewReader(latin1), f).Read()
	require.NoError(t, err)
	assert.Equal(t, []string{"Name", "Café"}, rec, "decoded while read")
	assert.True(t, IsTable("data/x.CSV"))
	assert.True(t, IsTable("x.tsv"))
	assert.False(t, IsTable("x.json"))
}

func TestProfile(t *testing.T) {
	cols, rows, err := Profile(Reader(strings.NewReader("\uFEFF"+ocean+"\n"), Format{Delimiter: ','}), 100)
	require.NoError(t, err)
	assert.Equal(t, 5, rows)
	byName := map[string]Column{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	assert.Equal(t, KindText, byName["Sta_ID"].Kind, "the BOM is dropped from the first name")
	assert.Equal(t, KindNumber, byName["Depthm"].Kind)
	assert.Equal(t, 0.0, byName["Depthm"].Min)
	assert.Equal(t, 50.0, byName["Depthm"].Max)
	assert.Equal(t, 1, byName["Salnty"].Empty)
	assert.Equal(t, KindDate, byName["Date"].Kind)
	assert.Equal(t, []string{"1949-03-01", "1951-07-12"}, byName["Date"].Samples)
	text := Describe(cols, rows)
	assert.Contains(t, text, "6 columns; 5 rows profiled.")
	assert.Contains(t, text, "- Depthm (number, 0 to 50): 0 | 8 | 10 | 30 | 50")
	assert.Contains(t, text, "- Salnty (number, 33.44 to 33.6, 1 empty)")

	empty, _, err := Profile(Reader(strings.NewReader("a,b\n,\n"), Format{Delimiter: ','}), 10)
	require.NoError(t, err)
	assert.Equal(t, KindEmpty, empty[0].Kind)
	_, _, err = Profile(Reader(strings.NewReader(""), Format{Delimiter: ','}), 10)
	assert.Error(t, err, "no header")
}

func TestFilters(t *testing.T) {
	for _, tc := range []struct {
		expr string
		yes  []string
		no   []string
	}{
		{">10", []string{"10.5", " 11 "}, []string{"10", "x", ""}},
		{">=10", []string{"10"}, []string{"9.9"}},
		{"<0", []string{"-1"}, []string{"0"}},
		{"<=0", []string{"0"}, []string{"1"}},
		{"=10", []string{"10.0", "10"}, []string{"10.5"}},
		{"!=10", []string{"9"}, []string{"10.00"}},
		{"=north", []string{"North"}, []string{"northeast"}},
		{"!=north", []string{"south"}, []string{"NORTH"}},
		{"10..20", []string{"10", "20", "15.5"}, []string{"9.99", "20.1", "x"}},
		{"20..10", []string{"15"}, nil},
		{"empty", []string{"", "  "}, []string{"x"}},
		{"!empty", []string{"x"}, []string{""}},
		{"Deep", []string{"deep", "Deep cast"}, []string{"surface"}},
	} {
		f, err := ParseFilter(tc.expr)
		require.NoError(t, err, tc.expr)
		for _, v := range tc.yes {
			assert.True(t, f.Match(v), "%s matches %q", tc.expr, v)
		}
		for _, v := range tc.no {
			assert.False(t, f.Match(v), "%s doesn't match %q", tc.expr, v)
		}
	}
	for _, bad := range []string{"", " ", ">x", "<=north", "1..x"} {
		_, err := ParseFilter(bad)
		assert.Error(t, err, bad)
	}
}

func read(t *testing.T, csv string, q Query) Page {
	t.Helper()
	p, err := Read(context.Background(), Reader(strings.NewReader(csv), Format{Delimiter: ','}), q)
	require.NoError(t, err)
	return p
}

func numbers(p Page) []int {
	out := make([]int, len(p.Rows))
	for i, r := range p.Rows {
		out[i] = r.Number
	}
	return out
}

func TestRead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		q       Query
		rows    []int
		matched int
		sorted  bool
	}{
		{"all", Query{Sort: -1}, []int{1, 2, 3, 4, 5}, 5, false},
		{"search", Query{Search: "DEEP", Sort: -1}, []int{4, 5}, 2, false},
		{"filter", Query{Filters: map[int]string{1: ">=10"}, Sort: -1}, []int{3, 4, 5}, 3, false},
		{"filters and search", Query{Search: "1949", Filters: map[int]string{3: "empty"}, Sort: -1}, []int{3}, 1, false},
		{"a column past the row", Query{Filters: map[int]string{9: "empty"}, Sort: -1}, []int{1, 2, 3, 4, 5}, 5, false},
		{"page", Query{Sort: -1, Offset: 1, Limit: 2}, []int{2, 3}, 5, false},
		{"sorted by number, descending", Query{Sort: 1, Desc: true}, []int{5, 4, 3, 2, 1}, 5, true},
		{"empty cells last", Query{Sort: 2}, []int{4, 2, 3, 1, 5}, 5, true},
		{"sorted by text", Query{Sort: 5, Limit: 3}, []int{4, 5, 1}, 5, true},
		{"sorted, a page", Query{Sort: 1, Offset: 4}, []int{5}, 5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := read(t, ocean, tc.q)
			assert.Equal(t, tc.rows, numbers(p))
			assert.Equal(t, tc.matched, p.Matched)
			assert.Equal(t, 5, p.Total)
			assert.Equal(t, tc.sorted, p.Sorted)
			assert.Equal(t, "Sta_ID", p.Columns[0])
		})
	}
}

// Past SortLimit matches, the page comes in file order from the first
// match, and says it isn't sorted.
func TestReadTooManyToSort(t *testing.T) {
	old := SortLimit
	SortLimit = 2
	t.Cleanup(func() { SortLimit = old })
	p := read(t, ocean, Query{Sort: 1, Desc: true, Limit: 3})
	assert.False(t, p.Sorted)
	assert.Equal(t, []int{1, 2, 3}, numbers(p))
	assert.Equal(t, 5, p.Matched)
	p = read(t, ocean, Query{Sort: 1, Offset: 1, Limit: 2})
	assert.Equal(t, []int{2, 3}, numbers(p))
}

// A bad filter names its column; a malformed row is skipped; a table with
// no header, or a caller gone, fails.
func TestReadFailures(t *testing.T) {
	_, err := Read(context.Background(), Reader(strings.NewReader(ocean), Format{Delimiter: ','}), Query{Filters: map[int]string{1: ">deep"}, Sort: -1})
	assert.ErrorIs(t, err, ErrBadFilter)
	assert.ErrorContains(t, err, "Depthm")
	_, err = Read(context.Background(), Reader(strings.NewReader(ocean), Format{Delimiter: ','}), Query{Filters: map[int]string{8: ""}, Sort: -1})
	assert.ErrorContains(t, err, "column 9")

	p, err := Read(context.Background(), Reader(strings.NewReader("a,b\n1,x\"y\n3,4\n"), Format{Delimiter: ','}), Query{Sort: -1})
	require.NoError(t, err)
	assert.Equal(t, 2, p.Total, "lenient quotes read the odd row")
	bad := Reader(strings.NewReader("a,b\n\"unclosed,1\n"), Format{Delimiter: ','})
	bad.LazyQuotes = false
	p, err = Read(context.Background(), bad, Query{Sort: -1})
	require.NoError(t, err)
	assert.Zero(t, p.Total, "a malformed row is skipped")

	_, err = Read(context.Background(), Reader(strings.NewReader(""), Format{Delimiter: ','}), Query{Sort: -1})
	assert.Error(t, err)
	var b strings.Builder
	b.WriteString("n\n")
	for i := range 5000 {
		b.WriteString(strings.Repeat("x", 1+i%3) + "\n")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Read(ctx, Reader(strings.NewReader(b.String()), Format{Delimiter: ','}), Query{Sort: -1})
	assert.ErrorIs(t, err, context.Canceled)
}
