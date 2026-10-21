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

package embed

import (
	"context"
	"errors"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ncruces/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/sources/sqlite"
)

// recipes is a SQLite file with two recipes.
func recipes(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recipes.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE recipes(name TEXT); INSERT INTO recipes VALUES ('soup'), ('bread')`)
	require.NoError(t, err)
	return path
}

// connect starts cfg's server and connects an MCP client to it, as Blitz
// does.
func connect(t *testing.T, cfg Config) *mcp.ClientSession {
	t.Helper()
	s, err := Start(context.Background(), cfg, testLog{t})
	require.NoError(t, err)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.IOTransport{Reader: s, Writer: s}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { cs.Close(); s.Close() })
	return cs
}

// testLog is Toolbox's log in the test's output.
type testLog struct{ t *testing.T }

func (l testLog) Write(p []byte) (int, error) {
	l.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// call calls a tool and returns its text and whether it was an error.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	var out []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			out = append(out, tc.Text)
		}
	}
	return strings.Join(out, "\n"), res.IsError
}

// writeConfig writes a database file and returns its path.
func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "db.yaml")
	require.NoError(t, os.WriteFile(p, []byte(yaml), 0o600))
	return p
}

const sqliteTools = `kind: source
name: recipes
type: sqlite
database: ${DB_PATH}
---
kind: tool
name: count_recipes
type: sqlite-sql
source: recipes
description: How many recipes there are
statement: SELECT count(*) AS n FROM recipes
---
kind: tool
name: run_sql
type: sqlite-execute-sql
source: recipes
description: Run any SQL
`

// Toolbox serves a config's tools in-process: listed and called over MCP,
// a ${NAME} filled from the values given and never the environment.
func TestServesTools(t *testing.T) {
	ctx := context.Background()
	file := writeConfig(t, sqliteTools)
	t.Setenv("DB_PATH", "/from/the/environment")
	_, err := Load(ctx, []string{file}, nil)
	require.ErrorContains(t, err, `"DB_PATH"`, "the process's environment isn't read")

	cfg, err := Load(ctx, []string{file}, map[string]string{"DB_PATH": recipes(t)})
	require.NoError(t, err)
	cs := connect(t, cfg)
	init := cs.InitializeResult()
	require.NotNil(t, init)
	assert.Equal(t, "Toolbox", init.ServerInfo.Name)
	assert.Equal(t, Version, init.ServerInfo.Version)

	list, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	assert.ElementsMatch(t, []string{"count_recipes", "run_sql"}, names)
	got, isErr := call(t, cs, "count_recipes", nil)
	assert.False(t, isErr)
	assert.JSONEq(t, `{"n": 2}`, got)

	_, err = Load(ctx, nil, nil)
	assert.Error(t, err, "no files")
}

// LoadWith gives each file's text to rewrite before it's parsed (Blitz
// resolves relative paths there); a rewrite's error names the file, and
// several files are merged as Load merges them.
func TestLoadWith(t *testing.T) {
	ctx := context.Background()
	db := recipes(t)
	file := writeConfig(t, strings.Replace(sqliteTools, "${DB_PATH}", "./recipes.db", 1))
	var seen []string
	cfg, err := LoadWith(ctx, []string{file}, nil, func(f string, raw []byte) ([]byte, error) {
		seen = append(seen, f)
		return []byte(strings.Replace(string(raw), "./recipes.db", db, 1)), nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{file}, seen)
	got, _ := call(t, connect(t, cfg), "count_recipes", nil)
	assert.JSONEq(t, `{"n": 2}`, got)

	_, err = LoadWith(ctx, []string{file}, nil, func(string, []byte) ([]byte, error) { return nil, errors.New("no file at x") })
	assert.ErrorContains(t, err, "db.yaml: no file at x")
	_, err = LoadWith(ctx, []string{filepath.Join(t.TempDir(), "gone.yaml")}, nil, func(_ string, raw []byte) ([]byte, error) { return raw, nil })
	assert.ErrorContains(t, err, "unable to read")
	_, err = LoadWith(ctx, []string{writeConfig(t, "kind: [")}, nil, func(_ string, raw []byte) ([]byte, error) { return raw, nil })
	assert.ErrorContains(t, err, "unable to parse")
	other := writeConfig(t, "kind: tool\nname: other\ntype: sqlite-sql\nsource: recipes\ndescription: d\nstatement: SELECT 1\n")
	cfg, err = LoadWith(ctx, []string{file, other}, map[string]string{}, func(_ string, raw []byte) ([]byte, error) {
		return []byte(strings.Replace(string(raw), "./recipes.db", db, 1)), nil
	})
	require.NoError(t, err)
	assert.Len(t, cfg.Tools, 3, "merged")
}

// A SQLite query returns at most MaxRows rows, the last one saying so
// (patch 08).
func TestSQLiteMaxRows(t *testing.T) {
	old := sqlite.MaxRows
	sqlite.MaxRows = 1
	t.Cleanup(func() { sqlite.MaxRows = old })
	cfg, err := Load(context.Background(), []string{writeConfig(t, sqliteTools)}, map[string]string{"DB_PATH": recipes(t)})
	require.NoError(t, err)
	got, isErr := call(t, connect(t, cfg), "run_sql", map[string]any{"sql": "SELECT name FROM recipes ORDER BY name"})
	require.False(t, isErr, got)
	lines := strings.Split(got, "\n")
	require.Len(t, lines, 2)
	assert.JSONEq(t, `{"name": "bread"}`, lines[0])
	assert.Contains(t, lines[1], "only the first 1 rows")
}

// A start that fails once a source is open (here a tool naming a source
// that isn't there) closes that source: its database/sql opener goroutine
// doesn't outlive the attempt (patch 07).
func TestFailedStartClosesSources(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	cfg, err := Load(context.Background(), []string{writeConfig(t, strings.Replace(sqliteTools, "source: recipes\ndescription: Run any SQL", "source: nowhere\ndescription: Run any SQL", 1))}, map[string]string{"DB_PATH": recipes(t)})
	require.NoError(t, err)
	for range 3 {
		_, err = Start(context.Background(), cfg, nil)
		require.Error(t, err)
	}
}

// Every SQLite source has sqlite-vec: vec0 tables and a nearest-neighbour
// query.
func TestSQLiteVec(t *testing.T) {
	// ncruces' own runtime has the threads sqlite-vec needs and a 256 MB
	// memory limit; one of Blitz's, with wazero's 4 GB, reserved 4 GB a
	// connection and lost the session on CI's Linux race run.
	assert.Nil(t, sqlite3.RuntimeConfig, "no runtime of Blitz's own")
	cfg, err := Load(context.Background(), []string{writeConfig(t, sqliteTools)}, map[string]string{"DB_PATH": recipes(t)})
	require.NoError(t, err)
	cs := connect(t, cfg)
	for _, tc := range []struct {
		name, sql string
	}{
		{"its version", "SELECT vec_version() AS v"},
		{"a vec0 table", "CREATE VIRTUAL TABLE dishes USING vec0(taste float[2])"},
		{"vectors in it", "INSERT INTO dishes(rowid, taste) VALUES (1, '[0,1]'), (2, '[1,0]'), (3, '[0.9,0.1]')"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, isErr := call(t, cs, "run_sql", map[string]any{"sql": tc.sql})
			assert.False(t, isErr, got)
		})
	}
	got, isErr := call(t, cs, "run_sql", map[string]any{"sql": "SELECT rowid FROM dishes WHERE taste MATCH '[1,0]' AND k = 2 ORDER BY distance"})
	require.False(t, isErr, got)
	var rows []map[string]any
	for _, line := range strings.Split(got, "\n") {
		var row map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &row), line)
		rows = append(rows, row)
	}
	require.Len(t, rows, 2)
	assert.EqualValues(t, 2, rows[0]["rowid"], "the nearest first")
	assert.EqualValues(t, 3, rows[1]["rowid"])
}

// A read-only SQLite source offers no tool that writes, and SQLite itself
// refuses a write, even from a tool that says it doesn't write.
func TestSQLiteReadOnly(t *testing.T) {
	db := recipes(t)
	cfg, err := Load(context.Background(), []string{writeConfig(t, `kind: source
name: recipes
type: sqlite
database: `+db+`
readOnly: true
---
kind: tool
name: run_sql
type: sqlite-execute-sql
source: recipes
description: Run any SQL
---
kind: tool
name: wipe
type: sqlite-sql
source: recipes
description: Says it reads, but deletes
statement: DELETE FROM recipes
annotations:
  readOnlyHint: true
`)}, nil)
	require.NoError(t, err)
	cs := connect(t, cfg)
	list, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, list.Tools, 1, "run_sql writes: not offered")
	assert.Equal(t, "wipe", list.Tools[0].Name)
	got, isErr := call(t, cs, "wipe", nil)
	assert.True(t, isErr)
	assert.Contains(t, got, "readonly")
}

// A database that can't be reached fails Start; Close is safe twice.
func TestStartAndClose(t *testing.T) {
	ctx := context.Background()
	cfg, err := Load(ctx, []string{writeConfig(t, sqliteTools)}, map[string]string{"DB_PATH": filepath.Join(t.TempDir(), "missing", "x.db")})
	require.NoError(t, err)
	_, err = Start(ctx, cfg, nil)
	assert.Error(t, err, "its folder isn't there")

	cfg, err = Load(ctx, []string{writeConfig(t, sqliteTools)}, map[string]string{"DB_PATH": recipes(t)})
	require.NoError(t, err)
	s, err := Start(ctx, cfg, nil)
	require.NoError(t, err)
	assert.NoError(t, s.Close())
	assert.NoError(t, s.Close(), "twice")
}

// A statement that only reads is told from one that may write.
func TestReadsOnly(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		want bool
	}{
		{"SELECT * FROM t", true},
		{"  select\n  count(*) from t;", true},
		{"-- the totals\nSELECT sum(n) FROM t", true},
		{"/* a */ WITH x AS (SELECT 1) SELECT * FROM x", true},
		{"(SELECT 1)", true},
		{"EXPLAIN SELECT 1", true},
		{"PRAGMA table_info(t)", true},
		{"SELECT replace(name, 'a', 'b') FROM t", true},
		{"WITH gone AS (DELETE FROM t RETURNING *) SELECT * FROM gone", false},
		{"DELETE FROM t", false},
		{"insert into t values (1)", false},
		{"UPDATE t SET n = 1", false},
		{"DROP TABLE t", false},
		{"SELECT 1; DROP TABLE t", false},
		{"", false},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			assert.Equal(t, tc.want, ReadsOnly(tc.sql))
		})
	}
}

// Read-only sources and the tools on them; a custom SQL tool that only
// reads is marked so, one that writes isn't.
func TestReadOnlyTools(t *testing.T) {
	cfg, err := Load(context.Background(), []string{writeConfig(t, `kind: source
name: ro
type: sqlite
database: /tmp/a.db
readOnly: true
---
kind: source
name: rw
type: sqlite
database: /tmp/b.db
---
kind: source
name: bq_blocked
type: bigquery
project: p
writeMode: blocked
---
kind: source
name: bq_ro
type: bigquery
project: p
readOnly: true
---
kind: source
name: bq_rw
type: bigquery
project: p
---
kind: tool
name: reads
type: sqlite-sql
source: ro
description: Reads
statement: SELECT 1
---
kind: tool
name: writes
type: sqlite-sql
source: rw
description: Writes
statement: DELETE FROM t
---
kind: tool
name: rw_reads
type: sqlite-sql
source: rw
description: Reads, but the source writes
statement: SELECT 1
`)}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"bq_blocked", "bq_ro", "ro"}, ReadOnlySources(cfg))
	assert.Equal(t, []string{"reads"}, ReadOnlyTools(cfg))
	for name, want := range map[string]bool{"reads": true, "rw_reads": true, "writes": false} {
		ann := reflect.ValueOf(cfg.Tools[name]).FieldByName("Annotations")
		assert.Equal(t, want, !ann.IsNil(), name)
	}
}

// A read-only Postgres source refuses writes, from any SQL and from a
// tool that says it reads; a writable one writes. It
// runs against a real Postgres when BLITZ_TEST_POSTGRES is
// host:port:password (a database shop with a table orders).
func TestPostgresReadOnly(t *testing.T) {
	addr := os.Getenv("BLITZ_TEST_POSTGRES")
	if addr == "" {
		t.Skip("BLITZ_TEST_POSTGRES isn't set")
	}
	parts := strings.SplitN(addr, ":", 3)
	require.Len(t, parts, 3, "host:port:password")
	env := map[string]string{"PG_HOST": parts[0], "PG_PORT": parts[1], "PG_PASSWORD": parts[2]}
	source := func(name string, readOnly bool) string {
		ro := ""
		if readOnly {
			ro = "readOnly: true\n"
		}
		return `kind: source
name: ` + name + `
type: postgres
host: ${PG_HOST}
port: ${PG_PORT}
user: postgres
password: ${PG_PASSWORD}
database: shop
` + ro
	}
	tool := func(name, typ, src, extra string) string {
		return "---\nkind: tool\nname: " + name + "\ntype: " + typ + "\nsource: " + src + "\ndescription: " + name + "\n" + extra
	}
	cfg, err := Load(context.Background(), []string{writeConfig(t, source("ro", true)+
		tool("ro_sql", "postgres-execute-sql", "ro", "")+
		tool("ro_total", "postgres-sql", "ro", "statement: SELECT sum(total) AS total FROM orders\n")+
		tool("ro_sneaky", "postgres-sql", "ro", "statement: DELETE FROM orders\nannotations:\n  readOnlyHint: true\n")+
		tool("ro_tables", "postgres-list-tables", "ro", "")+
		"---\n"+source("rw", false)+
		tool("rw_sql", "postgres-execute-sql", "rw", ""))}, env)
	require.NoError(t, err)
	assert.Equal(t, []string{"ro_sneaky", "ro_sql", "ro_tables", "ro_total"}, ReadOnlyTools(cfg))
	cs := connect(t, cfg)

	list, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)
	var names []string
	for _, tl := range list.Tools {
		names = append(names, tl.Name)
	}
	assert.ElementsMatch(t, []string{"ro_sql", "ro_total", "ro_sneaky", "ro_tables", "rw_sql"}, names)

	got, isErr := call(t, cs, "ro_total", nil)
	require.False(t, isErr, got)
	assert.Contains(t, got, "29.5")

	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
	}{
		{"a tool that says it reads", "ro_sneaky", nil},
		{"any SQL", "ro_sql", map[string]any{"sql": "DELETE FROM orders"}},
		{"a transaction of its own", "ro_sql", map[string]any{"sql": "BEGIN READ WRITE; DELETE FROM orders; COMMIT"}},
		{"a session setting", "ro_sql", map[string]any{"sql": "SET default_transaction_read_only = off; DELETE FROM orders"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, isErr := call(t, cs, tc.tool, tc.args)
			assert.True(t, isErr, got)
		})
	}
	got, isErr = call(t, cs, "ro_sql", map[string]any{"sql": "SELECT count(*) AS n FROM orders"})
	require.False(t, isErr, got)

	got, isErr = call(t, cs, "rw_sql", map[string]any{"sql": "CREATE TABLE IF NOT EXISTS notes(n int)"})
	assert.False(t, isErr, got)
	got, isErr = call(t, cs, "rw_sql", map[string]any{"sql": "SELECT count(*) AS n FROM orders"})
	require.False(t, isErr, got)
	assert.Contains(t, got, `"n":2`, "the sneaky delete deleted nothing")
}
