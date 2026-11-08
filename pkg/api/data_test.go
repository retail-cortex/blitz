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

package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A table's rows read from JSON keep their numbers as written; none is
// an empty list (spec_059_data DAT-01, DAT-03).
func TestDataTableFrom(t *testing.T) {
	cols := []DataColumn{{Name: "id", Type: "integer"}}
	for _, tc := range []struct {
		name string
		rows string
		want [][]any
		err  bool
	}{
		{"numbers as written", `[[9007199254740993], [1.50]]`, [][]any{{json.Number("9007199254740993")}, {json.Number("1.50")}}, false},
		{"text and NULL", `[["a", null]]`, [][]any{{"a", nil}}, false},
		{"none", "", [][]any{}, false},
		{"null", "null", [][]any{}, false},
		{"not JSON", "[[", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DataTableFrom(cols, tc.rows, 2, true, "| id |")
			if tc.err {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Rows)
			assert.Equal(t, DataTable{Columns: cols, Rows: tc.want, Total: 2, Truncated: true, Markdown: "| id |"}, got)
		})
	}
}

// A connection a chat or the CLI may use is on, trusted and readable
// (DAT-44).
func TestDataConnectionUsable(t *testing.T) {
	for name, tc := range map[string]struct {
		c    DataConnection
		want bool
	}{
		"usable":      {DataConnection{On: true, Trusted: true}, true},
		"off":         {DataConnection{Trusted: true}, false},
		"not trusted": {DataConnection{On: true}, false},
		"unreadable":  {DataConnection{On: true, Trusted: true, Problem: "bad"}, false},
	} {
		t.Run(name, func(t *testing.T) { assert.Equal(t, tc.want, tc.c.Usable()) })
	}
}
