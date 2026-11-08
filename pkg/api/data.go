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
	"strings"
)

// DataConnection is one of the workspace's database connections, as the
// CLI's /data and blitz data list show it (spec_059_data DAT-45).
type DataConnection struct {
	Name string
	// Kind is its kind's ID (sqlite, postgres, bigquery…).
	Kind     string
	ReadOnly bool
	// On says it's on in this workspace (CON-17).
	On bool
	// Trusted says the person trusted it; a chat or the CLI uses only
	// trusted ones (DAT-44).
	Trusted bool
	// Problem is why it can't be read ("" when it can).
	Problem string
	Queries []DataQueryInfo
}

// Usable reports whether a chat or the CLI may query c: on, trusted and
// readable.
func (c DataConnection) Usable() bool { return c.On && c.Trusted && c.Problem == "" }

// DataQueryInfo is one of a connection's saved queries.
type DataQueryInfo struct {
	Name        string
	Description string
	// Params are its parameters' names, in order.
	Params []string
}

// DataQuery is a query to run on one of the workspace's connections, by
// name: a saved one (Query, its parameters in Named) or a statement (its
// parameters in Params, in order). It runs read-only.
type DataQuery struct {
	Connection string
	Query      string
	Named      map[string]any
	Statement  string
	Params     []any
	// MaxRows is the most rows the table holds (1,000 when 0).
	MaxRows int
}

// DataTable is a query's result (spec_059_data DAT-01): its columns
// described, its rows as arrays in the columns' order.
type DataTable struct {
	Columns   []DataColumn `json:"columns"`
	Rows      [][]any      `json:"rows"`
	Total     int          `json:"total"`
	Truncated bool         `json:"truncated"`
	// Markdown is the table as Markdown, its first 50 rows (DAT-05).
	Markdown string `json:"-"`
}

// DataColumn is a column of a DataTable (DAT-02).
type DataColumn struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Type is portable: string, integer, number, boolean, date, time,
	// timestamp, json, bytes or other.
	Type   string `json:"type"`
	Length *int64 `json:"length"`
	// DBType is the database's own type.
	DBType   string `json:"db_type,omitempty"`
	Nullable *bool  `json:"nullable,omitempty"`
}

// DataTableFrom is a table read from its JSON (DAT-01): the engine's
// table, or rows_json beside its columns.
func DataTableFrom(columns []DataColumn, rowsJSON string, total int, truncated bool, markdown string) (DataTable, error) {
	t := DataTable{Columns: columns, Total: total, Truncated: truncated, Markdown: markdown, Rows: [][]any{}}
	if rowsJSON != "" {
		d := json.NewDecoder(strings.NewReader(rowsJSON))
		d.UseNumber() // numbers as written
		if err := d.Decode(&t.Rows); err != nil {
			return DataTable{}, err
		}
	}
	if t.Rows == nil { // "null": none
		t.Rows = [][]any{}
	}
	return t, nil
}
