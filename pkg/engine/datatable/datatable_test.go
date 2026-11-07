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

package datatable

import (
	"database/sql/driver"
	"encoding/json"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypeOf(t *testing.T) {
	for _, tc := range []struct{ db, want string }{
		{"VARCHAR(80)", String}, {"bpchar", String}, {"text", String}, {"uuid", String}, {"character varying", String},
		{"int4", Integer}, {"BIGINT", Integer}, {"bigserial", Integer}, {"INT64", Integer},
		{"NUMERIC(10,2)", Number}, {"float8", Number}, {"double precision", Number}, {"decimal", Number},
		{"bool", Boolean}, {"BOOLEAN", Boolean},
		{"date", Date}, {"time", Time}, {"timetz", Time},
		{"timestamptz", Timestamp}, {"timestamp with time zone", Timestamp}, {"DATETIME", Timestamp},
		{"jsonb", JSON}, {"_int4", JSON}, {"text[]", JSON}, {"STRUCT", JSON},
		{"bytea", Bytes}, {"BLOB", Bytes}, {"varbinary", Bytes},
		{"interval", String}, {"tsvector", Other}, {"", Other},
	} {
		t.Run(tc.db, func(t *testing.T) { assert.Equal(t, tc.want, TypeOf(tc.db)) })
	}
}

type valuer struct{ v any }

func (v valuer) Value() (driver.Value, error) { return v.v, nil }

type stringer struct{}

func (stringer) String() string { return "s" }

func TestValue(t *testing.T) {
	day := time.Date(2026, 11, 7, 9, 30, 15, 0, time.UTC)
	for _, tc := range []struct {
		name string
		v    any
		typ  string
		want any
	}{
		{"null", nil, String, nil},
		{"int", int64(1042), Integer, int64(1042)},
		{"small ints", int32(7), Integer, int64(7)},
		{"past 2^53", int64(math.MaxInt64), Integer, "9223372036854775807"},
		{"below -2^53", int64(math.MinInt64), Integer, "-9223372036854775808"},
		{"uint past 2^53", uint64(math.MaxUint64), Integer, "18446744073709551615"},
		{"SQLite's boolean", int64(1), Boolean, true},
		{"float", 3.5, Number, 3.5},
		{"NaN", math.NaN(), Number, "NaN"},
		{"decimal kept", "12.50", Number, 12.5},
		{"decimal too long", "12345678901234567.89", Number, "12345678901234567.89"},
		{"decimal leading zeros", "0.000123", Number, 0.000123},
		{"big int", new(big.Int).Lsh(big.NewInt(1), 70), Integer, "1180591620717411303424"},
		{"small big int", big.NewInt(5), Integer, int64(5)},
		{"rational", big.NewRat(1, 4), Number, 0.25},
		{"date", day, Date, "2026-11-07"},
		{"time", day, Time, "09:30:15"},
		{"timestamp", day, Timestamp, "2026-11-07T09:30:15Z"},
		{"bytes", []byte{0xff, 0x00}, Bytes, "/wA="},
		{"text bytes", []byte("soup"), String, "soup"},
		{"binary in text", []byte{0xff}, String, "/w=="},
		{"json text", `{"a":1}`, JSON, json.RawMessage(`{"a":1}`)},
		{"json bytes", []byte(`[1]`), JSON, json.RawMessage(`[1]`)},
		{"bad json", "{", JSON, "{"},
		{"uuid", [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, String, "01020304-0506-0708-090a-0b0c0d0e0f10"},
		{"map", map[string]any{"a": 1}, JSON, map[string]any{"a": 1}},
		{"valuer", valuer{"7.25"}, Number, 7.25},
		{"stringer", stringer{}, Other, "s"},
		{"struct", struct{ A int }{1}, Other, struct{ A int }{1}},
		{"channel", make(chan int), Other, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Value(tc.v, tc.typ)
			if tc.name == "channel" {
				assert.IsType(t, "", got, "printed when JSON can't hold it")
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNew(t *testing.T) {
	n := int64(80)
	yes := true
	tab := New([]Column{
		{Name: "id", DBType: "int4"},
		{Name: "customer", DBType: "varchar", Length: &n, Nullable: &yes, Description: "Who placed it"},
		{Name: "n"},
		{Name: "kind", Type: String},
	}, [][]any{{int64(1042), []byte("Ada"), int64(3), "a"}, {int64(1043), nil, int64(4)}}, true)
	assert.Equal(t, []string{Integer, String, Integer, String}, []string{tab.Columns[0].Type, tab.Columns[1].Type, tab.Columns[2].Type, tab.Columns[3].Type})
	assert.Equal(t, 2, tab.Total)
	assert.Equal(t, []any{int64(1043), nil, int64(4), nil}, tab.Rows[1], "a short row is padded with NULLs")

	data, err := json.Marshal(tab)
	require.NoError(t, err)
	assert.JSONEq(t, `{"columns":[
		{"name":"id","description":"","type":"integer","length":null,"db_type":"int4"},
		{"name":"customer","description":"Who placed it","type":"string","length":80,"db_type":"varchar","nullable":true},
		{"name":"n","description":"","type":"integer","length":null},
		{"name":"kind","description":"","type":"string","length":null}],
		"rows":[[1042,"Ada",3,"a"],[1043,null,4,null]],"total":2,"truncated":true}`, string(data))

	items := tab.Items()
	require.Len(t, items, 2)
	assert.Equal(t, 1, items[1].Index)
	assert.Equal(t, map[string]any{"id": int64(1043), "customer": nil, "n": int64(4), "kind": nil}, items[1].Row)
	assert.Equal(t, tab.Columns, items[1].Columns)

	for _, tc := range []struct {
		v    any
		want string
	}{{3.5, Number}, {true, Boolean}, {"x", String}, {[]byte{0xff}, Bytes}, {time.Now(), Timestamp}, {struct{}{}, Other}} {
		assert.Equal(t, tc.want, New([]Column{{Name: "e"}}, [][]any{{nil}, {tc.v}}, false).Columns[0].Type)
	}
	assert.Equal(t, Other, New([]Column{{Name: "e"}}, [][]any{{nil}}, false).Columns[0].Type)
}

func TestMarkdown(t *testing.T) {
	tab := New([]Column{{Name: "id", Type: Integer}, {Name: "note|s", Type: String}, {Name: "j", Type: JSON}},
		[][]any{{int64(1), "a | b\nc", `{"k":1}`}, {int64(2), nil, map[string]any{"k": 2}}, {int64(3), "z", nil}}, true)
	assert.Equal(t, "| id | note\\|s | j |\n| ---: | --- | --- |\n| 1 | a \\| b c | {\"k\":1} |\n| 2 |  | {\"k\":2} |\n\n1 more rows (3 in all).\n\nThe result was cut at 3 rows: the query returned more.\n", tab.Markdown(2))
	assert.NotContains(t, tab.Markdown(0), "more rows")
	assert.Equal(t, "(no columns)", Table{}.Markdown(0))
}
