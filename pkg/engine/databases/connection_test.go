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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/config"
)

// What Studio's form writes, it reads back, for every kind.
func TestRenderParse(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    Connection
		has  []string
	}{
		{"postgres", Connection{Name: "shop", Type: "postgres", Description: "The shop's orders.", Settings: map[string]string{"host": "db.local", "port": "5433", "database": "shop", "user": "reader", "password": "x"}, ReadOnly: true, Tools: []string{"execute_sql", "list_tables"},
			Queries: []Query{{Name: "top_customers", Description: "The best customers", Statement: "SELECT name FROM customers\nORDER BY spent DESC\nLIMIT $1", Params: []Param{{Name: "limit", Type: "integer", Description: "How many"}}}}},
			[]string{"password: ${DB_SHOP_PASSWORD}", "readOnly: true", "# about: The shop's orders.", "name: shop_top_customers", "type: postgres-sql", `port: "5433"`}},
		{"alloydb", Connection{Name: "ledger", Type: "alloydb-postgres", Settings: map[string]string{"project": "p", "region": "us-central1", "cluster": "c", "instance": "i", "database": "ledger"}, Tools: []string{"list_schemas"}},
			[]string{"ipType: public", "type: postgres-list-schemas"}},
		{"sqlite", Connection{Name: "notes", Type: "sqlite", Settings: map[string]string{"database": "/data/notes.db"}, ReadOnly: true, Tools: []string{"list_tables"}},
			[]string{"database: /data/notes.db", "statement: SELECT name, type, sql FROM sqlite_master"}},
		{"bigquery", Connection{Name: "sales", Type: "bigquery", Settings: map[string]string{"project": "acme", "allowedDatasets": "eu, us", "maximumBytesBilled": "1000000000"}, ReadOnly: true, Tools: []string{"execute_sql", "list_datasets"}},
			[]string{"writeMode: blocked", "allowedDatasets: [eu, us]", "type: bigquery-list-dataset-ids"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := Render(tc.c)
			require.NoError(t, err)
			for _, h := range tc.has {
				assert.Contains(t, text, h)
			}
			got := Parse(tc.c.Name, text)
			assert.False(t, got.Custom, text)
			assert.Equal(t, tc.c.Type, got.Type)
			assert.Equal(t, tc.c.ReadOnly, got.ReadOnly)
			assert.Equal(t, tc.c.Tools, got.Tools)
			assert.Equal(t, tc.c.Queries, got.Queries)
			assert.Equal(t, strings.TrimSpace(tc.c.Description), got.Description)
			for k, v := range tc.c.Settings {
				if k == "password" {
					v = "${" + SecretKey(tc.c.Name, k) + "}"
				}
				assert.Equal(t, v, got.Settings[k], k)
			}
			_, err = loadConfig(context.Background(), []string{writeTemp(t, text)}, map[string]string{"DB_SHOP_PASSWORD": "pw"})
			assert.NoError(t, err, "Toolbox reads it")
		})
	}
}

func writeTemp(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.yaml")
	require.NoError(t, os.WriteFile(p, []byte(text), 0o600))
	return p
}

// A file that's more than the form describes is Custom; a broken one
// says why.
func TestParseCustom(t *testing.T) {
	for name, text := range map[string]string{
		"two sources":     "kind: source\nname: a\ntype: sqlite\ndatabase: x\n---\nkind: source\nname: b\ntype: sqlite\ndatabase: y\n",
		"another name":    "kind: source\nname: other\ntype: sqlite\ndatabase: x\n",
		"an unknown type": "kind: source\nname: a\ntype: mysql\nhost: x\n",
		"another setting": "kind: source\nname: a\ntype: postgres\nhost: x\nport: 1\ndatabase: d\nuser: u\npassword: p\nqueryParams: {sslmode: require}\n",
		"another tool":    "kind: source\nname: a\ntype: postgres\nhost: x\n---\nkind: tool\nname: a_locks\ntype: postgres-list-locks\nsource: a\ndescription: x\n",
		"a toolset":       "kind: source\nname: a\ntype: sqlite\ndatabase: x\n---\nkind: toolset\nname: all\ntools: [a_x]\n",
	} {
		t.Run(name, func(t *testing.T) {
			c := Parse("a", text)
			assert.True(t, c.Custom)
			assert.Equal(t, text, c.Text)
		})
	}
	c := Parse("a", "kind: [")
	assert.True(t, c.Custom)
	assert.NotEmpty(t, c.Problem)
}

// Check lists what keeps a connection from being saved.
func TestCheck(t *testing.T) {
	good := Connection{Name: "shop", Type: "sqlite", Settings: map[string]string{"database": "x.db"}}
	assert.Empty(t, Check(good))
	for _, tc := range []struct {
		name string
		edit func(*Connection)
		want string
	}{
		{"a bad name", func(c *Connection) { c.Name = "Shop!" }, "name"},
		{"an unknown type", func(c *Connection) { c.Type = "mysql" }, "type"},
		{"a missing setting", func(c *Connection) { c.Settings = nil }, "database is required"},
		{"an unknown tool", func(c *Connection) { c.Tools = []string{"nope"} }, "no ready-made tool"},
		{"a query named as a tool", func(c *Connection) {
			c.Queries = []Query{{Name: "list_tables", Description: "d", Statement: "SELECT 1"}}
		}, "taken"},
		{"an empty query", func(c *Connection) { c.Queries = []Query{{Name: "q", Description: "d"}} }, "empty"},
		{"an undescribed query", func(c *Connection) { c.Queries = []Query{{Name: "q", Statement: "SELECT 1"}} }, "say what"},
		{"a bad parameter", func(c *Connection) {
			c.Queries = []Query{{Name: "q", Description: "d", Statement: "SELECT 1", Params: []Param{{Name: "n", Type: "date"}}}}
		}, "type is"},
		{"broken YAML", func(c *Connection) { c.Custom, c.Text = true, "kind: [" }, "YAML"},
		{"a number that isn't", func(c *Connection) {
			c.Type, c.Settings = "bigquery", map[string]string{"project": "p", "maximumBytesBilled": "1 GB"}
		}, "whole number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := good
			tc.edit(&c)
			assert.Contains(t, strings.Join(Check(c), "\n"), tc.want)
		})
	}
}

// Save writes the file and keeps the password as a secret; renaming moves
// both; Delete removes both; List reads them back.
func TestSaveAndDelete(t *testing.T) {
	ws := home(t)
	c := Connection{Name: "shop", Scope: ScopeWorkspace, Type: "postgres", Settings: map[string]string{"host": "h", "database": "d", "user": "u"}, ReadOnly: true, Tools: []string{"execute_sql"}}
	_, problems, err := Save("", ws, c, nil, "")
	require.NoError(t, err)
	assert.Contains(t, problems, "password is required")

	saved, problems, err := Save("", ws, c, map[string]string{"password": "s3cret-pass"}, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, filepath.Join(WorkspaceDir(ws), "shop.yaml"), saved.Path)
	b, err := os.ReadFile(saved.Path)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "s3cret-pass", "never the value")
	assert.True(t, HasSecret("", ws, saved, "password"))

	_, problems, err = Save("", ws, c, nil, "")
	require.NoError(t, err)
	assert.Contains(t, strings.Join(problems, "\n"), "already")

	c.Name = "store"
	moved, problems, err := Save("", ws, c, nil, "shop")
	require.NoError(t, err)
	require.Empty(t, problems, "the password is kept")
	assert.True(t, HasSecret("", ws, moved, "password"))
	assert.False(t, HasSecret("", ws, saved, "password"), "the old one's went with it")
	assert.NoFileExists(t, saved.Path)

	_, _, err = Save("", ws, c, nil, "gone")
	assert.ErrorIs(t, err, ErrUnknown)

	list := List("", ws)
	require.Len(t, list, 1)
	assert.Equal(t, "store", list[0].Name)
	assert.Equal(t, ScopeWorkspace, list[0].Scope)

	g := Connection{Name: "notes", Scope: ScopeGlobal, Type: "sqlite", Settings: map[string]string{"database": "x.db"}}
	_, problems, err = Save("", "", g, nil, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, []string{ScopeGlobal, ScopeWorkspace}, []string{List("", ws)[0].Scope, List("", ws)[1].Scope}, "the global ones first")

	require.NoError(t, Delete("", ws, ScopeWorkspace, "store"))
	assert.False(t, HasSecret("", ws, moved, "password"))
	assert.ErrorIs(t, Delete("", ws, ScopeWorkspace, "store"), ErrUnknown)

	_, problems, _ = Save("", "", Connection{Name: "x", Scope: ScopeWorkspace, Type: "sqlite", Settings: map[string]string{"database": "x"}}, nil, "")
	assert.Contains(t, problems, "no workspace for a workspace database")
}

// Test lists what a connection offers, as written; Try runs a tool.
func TestTestAndTry(t *testing.T) {
	ws := home(t)
	c := Connection{Name: "kitchen", Scope: ScopeWorkspace, Type: "sqlite", Settings: map[string]string{"database": recipes(t)}, ReadOnly: true, Tools: []string{"execute_sql", "list_tables"},
		Queries: []Query{{Name: "count", Description: "How many recipes", Statement: "SELECT count(*) AS n FROM recipes"}}}
	tools, err := Test(context.Background(), "", ws, c, nil)
	require.NoError(t, err)
	var names []string
	for _, tl := range tools {
		names = append(names, tl.Name)
		assert.True(t, tl.ReadOnly, tl.Name)
	}
	assert.Equal(t, []string{"kitchen_count", "kitchen_list_tables"}, names, "execute_sql writes: hidden on a read-only one")

	text, isErr, err := Try(context.Background(), "", ws, c, nil, "count", nil)
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.JSONEq(t, `{"n": 2}`, text)

	c.Settings = map[string]string{"database": filepath.Join(t.TempDir(), "missing", "x.db")}
	_, err = Test(context.Background(), "", ws, c, nil)
	assert.Error(t, err)
	_, err = Test(context.Background(), "", ws, Connection{Name: "Bad"}, nil)
	assert.Error(t, err)

	pg := Connection{Name: "shop", Scope: ScopeWorkspace, Type: "postgres", Settings: map[string]string{"host": "127.0.0.1", "port": "1", "database": "d", "user": "u"}, Tools: []string{"execute_sql"}}
	require.NoError(t, config.Vault("").Set(config.SecretScope("", ws), SecretKey("shop", "password"), "kept-pass"))
	_, err = Test(context.Background(), "", ws, pg, map[string]string{"password": "typed-pass"})
	require.Error(t, err, "nothing listens on port 1")
	assert.NotContains(t, err.Error(), "typed-pass")
}
