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

package databases

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// home makes a Blitz home with in-memory secrets, and a workspace; it
// returns the workspace.
func home(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	secrets.SetDefault(&secrets.Memory{})
	return t.TempDir()
}

// recipes is a SQLite file with two recipes.
func recipes(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recipes.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE recipes(name TEXT, minutes INTEGER); INSERT INTO recipes VALUES ('soup', 30), ('bread', 90)`)
	require.NoError(t, err)
	return path
}

func write(t *testing.T, path, text string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
}

const kitchen = `kind: source
name: kitchen
type: sqlite
database: ${KITCHEN_DB}
readOnly: true
---
kind: tool
name: count_recipes
type: sqlite-sql
source: kitchen
description: How many recipes there are
statement: SELECT count(*) AS n FROM recipes
`

const pantry = `kind: source
name: pantry
type: sqlite
database: ${PANTRY_DB}
readOnly: true
---
kind: tool
name: pantry_count
type: sqlite-sql
source: pantry
description: How many there are
statement: SELECT 1 AS n
`

// The global and the workspace's files make one server: a global file's
// ${NAME}s are the workspace's secret, else the global one, and its
// read-only tools run unasked; so are a trusted workspace file's. An
// untrusted one gets only the workspace's own secrets, and asks first.
func TestServe(t *testing.T) {
	ws := home(t)
	_, cat, _ := catalog(t)
	global := filepath.Join(t.TempDir(), "kitchen.yaml")
	local := filepath.Join(t.TempDir(), "pantry.yaml")
	write(t, global, kitchen)
	write(t, local, pantry)
	v := config.Vault("")
	require.NoError(t, v.Set(secrets.GlobalScope, "KITCHEN_DB", recipes(t)))
	require.NoError(t, v.Set(secrets.GlobalScope, "PANTRY_DB", "/global/pantry.db"))
	assert.Empty(t, Backend{}.Serve(connectors.Serving{Category: cat, Workspace: ws}), "no files, no server")

	serve := func(trusted bool) config.MCPServerConfig {
		out := Backend{}.Serve(connectors.Serving{Category: cat, Workspace: ws, Global: []string{global}, Local: []string{local}, Trusted: func(string) bool { return trusted }})
		require.Len(t, out, 1)
		return out[0]
	}
	c := serve(false)
	assert.Equal(t, "databases", c.Name)
	assert.Equal(t, ID, c.InProcess)
	assert.Equal(t, "db", c.Prefix)
	assert.Equal(t, []string{"*"}, c.Agents)
	assert.Equal(t, []string{global, local}, c.Args)
	assert.Equal(t, []string{"KITCHEN_DB", "PANTRY_DB"}, Names(c.Args))
	assert.NotContains(t, c.Env, "PANTRY_DB", "an untrusted file gets no global secret")
	assert.Equal(t, []string{"count_recipes"}, c.ReadOnlyTools, "nor runs unasked")

	require.NoError(t, v.Set(config.SecretScope("", ws), "PANTRY_DB", recipes(t)))
	c = serve(false)
	assert.Contains(t, c.Env, "PANTRY_DB", "the workspace's own secret")

	require.NoError(t, v.Delete(config.SecretScope("", ws), "PANTRY_DB"))
	c = serve(true)
	assert.Equal(t, "/global/pantry.db", c.Env["PANTRY_DB"], "trusted: the global one too")
	assert.ElementsMatch(t, []string{"count_recipes", "pantry_count"}, c.ReadOnlyTools)
}

// The transport starts Toolbox in-process: its tools listed and called
// over MCP; a database that can't be opened fails the connection; a
// cancelled one gives up.
func TestTransport(t *testing.T) {
	home(t)
	file := filepath.Join(t.TempDir(), "kitchen.yaml")
	write(t, file, kitchen)
	c := config.MCPServerConfig{Args: []string{file}, Env: map[string]string{"KITCHEN_DB": recipes(t)}}

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, Transport(c), nil)
	require.NoError(t, err)
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "count_recipes"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.JSONEq(t, `{"n": 2}`, res.Content[0].(*mcp.TextContent).Text)

	bad := c
	bad.Env = map[string]string{"KITCHEN_DB": filepath.Join(t.TempDir(), "missing", "x.db")}
	_, err = client.Connect(ctx, Transport(bad), nil)
	assert.Error(t, err)

	none := c
	none.Env = nil
	_, err = Transport(none).Connect(ctx)
	assert.ErrorContains(t, err, "KITCHEN_DB")

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = Transport(c).Connect(cancelled)
	if err != nil { // Start may win the race
		assert.True(t, strings.Contains(err.Error(), "canceled"), err.Error())
	}
}

// Test lists what a connection offers, as written; Try runs a tool;
// ReadSchema reads its tables and columns, and finds none in an empty
// file.
func TestTestTryAndSchema(t *testing.T) {
	ws := home(t)
	_, cat, _ := catalog(t)
	env := connectors.Env{Workspace: ws, Category: cat}
	k := kindOf(t, "sqlite")
	c := connectors.Connection{Name: "kitchen", Category: "databases", Scope: connectors.ScopeWorkspace, Kind: "sqlite", Settings: map[string]string{"database": recipes(t)}, ReadOnly: true, Tools: []string{"execute_sql", "list_tables"},
		Queries: []connectors.Query{{Name: "count", Description: "How many recipes", Statement: "SELECT count(*) AS n FROM recipes"}}}
	ctx := context.Background()
	tools, err := Backend{}.Test(ctx, env, k, c)
	require.NoError(t, err)
	var names []string
	for _, tl := range tools {
		names = append(names, tl.Name)
		assert.True(t, tl.ReadOnly, tl.Name)
	}
	assert.Equal(t, []string{"kitchen_count", "kitchen_list_tables"}, names, "execute_sql writes: hidden on a read-only one")

	text, isErr, err := Backend{}.Try(ctx, env, k, c, "count", nil)
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.JSONEq(t, `{"n": 2}`, text)

	tables, err := Backend{}.ReadSchema(ctx, env, k, c)
	require.NoError(t, err)
	require.Len(t, tables, 1)
	assert.Equal(t, "recipes", tables[0].Name)
	assert.Equal(t, []connectors.Column{{Name: "name", Type: "TEXT"}, {Name: "minutes", Type: "INTEGER"}}, tables[0].Columns)

	empty := filepath.Join(t.TempDir(), "empty.db")
	db, err := sql.Open("sqlite3", empty)
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA user_version = 1")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	c.Settings = map[string]string{"database": empty}
	tables, err = Backend{}.ReadSchema(ctx, env, k, c)
	require.NoError(t, err)
	assert.Empty(t, tables)

	c.Settings = map[string]string{"database": filepath.Join(t.TempDir(), "missing", "x.db")}
	_, err = Backend{}.Test(ctx, env, k, c)
	assert.Error(t, err)
	_, err = Backend{}.ReadSchema(ctx, env, nil, c)
	assert.Error(t, err, "no kind, no schema")

	pg := connectors.Connection{Name: "shop", Category: "databases", Scope: connectors.ScopeWorkspace, Kind: "postgres", Settings: map[string]string{"host": "127.0.0.1", "port": "1", "database": "d", "user": "u", "password": "${DB_SHOP_PASSWORD}"}, Tools: []string{"execute_sql"}}
	require.NoError(t, config.Vault("").Set(config.SecretScope("", ws), "DB_SHOP_PASSWORD", "kept-pass"))
	env.Values = map[string]string{"password": "typed-pass"}
	_, err = Backend{}.Test(ctx, env, kindOf(t, "postgres"), pg)
	require.Error(t, err, "nothing listens on port 1")
	assert.NotContains(t, err.Error(), "typed-pass")
}

// BigQuery's table metadata gives its description and columns.
func TestTableInfo(t *testing.T) {
	about, cols := tableInfo(`{"Description": "Orders", "Schema": [{"Name": "day", "Type": "DATE", "Description": "When"}, {"Name": "qty", "Type": "INTEGER"}]}`)
	assert.Equal(t, "Orders", about)
	assert.Equal(t, []connectors.Column{{Name: "day", Type: "DATE", About: "When"}, {Name: "qty", Type: "INTEGER"}}, cols)
	about, cols = tableInfo("not json")
	assert.Empty(t, about)
	assert.Empty(t, cols)
}
