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

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kitchenWorkspace is a workspace with a trusted, read-only SQLite
// connection, kitchen, and its saved queries.
func kitchenWorkspace(t *testing.T) string {
	t.Helper()
	secrets.SetDefault(&secrets.Memory{})
	dir := t.TempDir()
	file := filepath.Join(t.TempDir(), "kitchen.db")
	db, err := sql.Open("sqlite3", file)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE recipes(name TEXT, minutes INTEGER, notes TEXT); INSERT INTO recipes VALUES ('soup', 30, 'hot, ' || char(10) || 'filling'), ('bread', 90, NULL)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = dir
	w, err := engine.Open(context.Background(), cfg, engine.Options{})
	require.NoError(t, err)
	defer w.Close()
	_, probs, err := w.SaveConnector(context.Background(), connectors.Connection{Name: "kitchen", Category: "databases", Scope: connectors.ScopeWorkspace, Kind: "sqlite",
		Settings: map[string]string{"database": file}, ReadOnly: true, Queries: []connectors.Query{
			{Name: "count", Description: "How many recipes", Statement: "SELECT count(*) AS n FROM recipes"},
			{Name: "longer", Description: "Longer than", Statement: "SELECT name FROM recipes WHERE minutes > ?", Params: []connectors.Param{{Name: "m", Type: "integer", Description: "Minutes"}}},
		}}, nil, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	return dir
}

// blitz data lists the workspace's databases and runs a query on one,
// read-only, printing its table as text, JSON or CSV (spec_059_data
// DAT-45).
func TestDataCommand(t *testing.T) {
	isolate(t)
	dir := kitchenWorkspace(t)

	out, err := runCLI(t, "-d", dir, "data", "list")
	require.NoError(t, err, out)
	for _, want := range []string{"@db:kitchen", "sqlite · read-only", "/count: How many recipes", "(takes m)"} {
		assert.Contains(t, out, want)
	}
	out, err = runCLI(t, "-d", dir, "data", "list", "--json")
	require.NoError(t, err, out)
	assert.JSONEq(t, `[{"name":"kitchen","kind":"sqlite","read_only":true,"on":true,"trusted":true,"queries":[
		{"name":"count","description":"How many recipes","params":[]},{"name":"longer","description":"Longer than","params":["m"]}]}]`, out)

	out, err = runCLI(t, "-d", dir, "data", "query", "kitchen", "SELECT name, minutes, notes FROM recipes ORDER BY name")
	require.NoError(t, err, out)
	assert.Equal(t, "name    minutes  notes\nstring  integer  string\nbread   90\nsoup    30       hot, filling\nRows: 2\n", out)

	out, err = runCLI(t, "-d", dir, "data", "query", "kitchen", "SELECT name, minutes FROM recipes WHERE minutes < ?", "--params", "[60]", "--json")
	require.NoError(t, err, out)
	var table map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &table), out)
	assert.Equal(t, []any{[]any{"soup", float64(30)}}, table["rows"])
	assert.EqualValues(t, 1, table["total"])
	assert.Equal(t, "minutes", table["columns"].([]any)[1].(map[string]any)["name"])

	out, err = runCLI(t, "-d", dir, "data", "query", "kitchen", "--query", "longer", "--params", `{"m": 60}`, "--csv")
	require.NoError(t, err, out)
	assert.Equal(t, "name\nbread\n", out)

	out, err = runCLI(t, "-d", dir, "data", "query", "kitchen", "-q", "count", "--max-rows", "1")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Rows: 1")

	out, err = runCLI(t, "-d", dir, "data", "query", "kitchen", "SELECT name FROM recipes", "--max-rows", "1")
	require.NoError(t, err, out)
	assert.Contains(t, out, "cut at the limit")

	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"writes refused", []string{"data", "query", "kitchen", "DELETE FROM recipes"}, exitFailure, "read-only"},
		{"no such connection", []string{"data", "query", "gone", "SELECT 1"}, exitFailure, `no database connection "gone"`},
		{"nothing to run", []string{"data", "query", "kitchen"}, exitUsage, "give the SQL"},
		{"both formats", []string{"data", "query", "kitchen", "SELECT 1", "--json", "--csv"}, exitUsage, "one of --json and --csv"},
		{"SQL and a saved query", []string{"data", "query", "kitchen", "SELECT 1", "-q", "count"}, exitUsage, "not both"},
		{"parameters not JSON", []string{"data", "query", "kitchen", "SELECT ?", "--params", "{"}, exitUsage, "an array"},
		{"a saved query's not an object", []string{"data", "query", "kitchen", "-q", "longer", "--params", "[1]"}, exitUsage, "an object"},
		{"negative rows", []string{"data", "query", "kitchen", "SELECT 1", "--max-rows", "-1"}, exitUsage, "--max-rows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runCLI(t, append([]string{"-d", dir}, tc.args...)...)
			require.Error(t, err)
			assert.Equal(t, tc.code, exitCodeFor(err), "%v", err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// A workspace with no databases says how to add one.
func TestDataListNone(t *testing.T) {
	isolate(t)
	out, err := runCLI(t, "-d", t.TempDir(), "data", "list")
	require.NoError(t, err, out)
	assert.Contains(t, out, "No database connections")
}

// The text table cuts long values and keeps columns apart; CSV leaves
// NULL empty and writes JSON values as JSON.
func TestDataFormats(t *testing.T) {
	long := strings.Repeat("x", 60)
	tab := api.DataTable{Columns: []api.DataColumn{{Name: "a", Type: "string"}, {Name: "b", Type: "json"}},
		Rows: [][]any{{long, map[string]any{"k": 1}}, {nil, true}}, Total: 2}
	text := dataText(tab)
	assert.Contains(t, text, strings.Repeat("x", cellWidth-1)+"…")
	assert.NotContains(t, text, strings.Repeat("x", cellWidth+1))
	assert.Contains(t, text, `{"k":1}`)

	var b bytes.Buffer
	require.NoError(t, writeCSV(&b, tab))
	assert.Equal(t, "a,b\n"+long+",\"{\"\"k\"\":1}\"\n,true\n", b.String())

	for v, want := range map[any]string{nil: "", "t": "t", json.Number("1.50"): "1.50", false: "false"} {
		assert.Equal(t, want, cellText(v))
	}

	// Output that can't be written (a closed pipe) is an error, from the
	// header or a row, once more than the writer's buffer is waiting.
	big := strings.Repeat("y", 8<<10)
	for name, tc := range map[string]api.DataTable{
		"the header": {Columns: []api.DataColumn{{Name: big}}},
		"a row":      {Columns: []api.DataColumn{{Name: "a"}}, Rows: [][]any{{big}}},
	} {
		assert.ErrorIs(t, writeCSV(failingWriter{}, tc), errClosedPipe, name)
	}
}

var errClosedPipe = errors.New("closed pipe")

// failingWriter fails every write, as a closed pipe does.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errClosedPipe }
