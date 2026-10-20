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

package connectors_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	_ "github.com/retail-cortex/blitz/pkg/engine/databases" // the toolbox backend
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

func write(t *testing.T, path, text string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
}

// The built-in descriptors load without a problem: the databases category
// and its four kinds, each with every capability, in three languages.
func TestBuiltinCatalog(t *testing.T) {
	cat := connectors.Load("", "")
	require.Len(t, cat.Categories, 1)
	db := cat.Categories[0]
	assert.Empty(t, db.Problem)
	assert.Equal(t, "databases", db.ID)
	assert.Equal(t, connectors.OriginBuiltin, db.Origin)
	assert.Equal(t, "Bases de datos", db.Name.In("es"))
	assert.Equal(t, "Bases de données", db.Name.In("fr-CA"))
	var ids []string
	for _, k := range cat.Kinds {
		assert.Empty(t, k.Problem, k.ID)
		assert.Equal(t, connectors.Capabilities, k.Capabilities, k.ID)
		assert.NotNil(t, k.ReadOnly, k.ID)
		ids = append(ids, k.ID)
		for _, f := range k.Fields {
			for _, lang := range []string{"en", "es", "fr-CA"} {
				assert.NotEmpty(t, f.Label[lang], "%s %s %s", k.ID, f.Key, lang)
			}
		}
	}
	assert.Equal(t, []string{"alloydb-postgres", "bigquery", "postgres", "sqlite"}, ids)
	pg, ok := cat.Kind("postgres")
	require.True(t, ok)
	pw, ok := pg.Field("password")
	require.True(t, ok)
	assert.True(t, pw.Secret())
	assert.Len(t, cat.KindsOf("databases", "toolbox"), 4)
	assert.Empty(t, cat.KindsOf("databases", "nope"))
	assert.Len(t, cat.Usable(), 1)
}

// Text is a string (English) or a map of languages; In falls back from
// fr-CA to fr, then to English.
func TestText(t *testing.T) {
	var x struct {
		A connectors.Text `yaml:"a"`
		B connectors.Text `yaml:"b"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("a: Host\nb: {en: Host, fr: Hôte}\n"), &x))
	assert.Equal(t, "Host", x.A.In("es"))
	assert.Equal(t, "Hôte", x.B.In("fr-CA"))
	assert.Equal(t, "Host", x.B.In("de"))
	assert.Error(t, yaml.Unmarshal([]byte("a: [1, 2]\n"), &x))
}

// supabaseKind is a kind a person adds: Postgres with other defaults.
const supabaseKind = `id: supabase
category: databases
name: Supabase
backend: toolbox
source: postgres
capabilities: [test, try, queries]
fields:
  - {key: host, required: true}
  - {key: port, default: "6543"}
  - {key: database, default: postgres}
  - {key: user, required: true}
  - {key: password, type: secret, required: true}
readOnly: {key: readOnly, value: true}
tools:
  - {id: execute_sql, type: postgres-execute-sql, on: true, agent: "Run SQL on {connection}."}
queries: {type: postgres-sql, dialect: PostgreSQL, placeholders: "$1"}
`

// A global or workspace descriptor adds a kind; one that can't be used
// is listed with its problem, and can't change a built-in kind.
func TestUserDescriptors(t *testing.T) {
	ws := home(t)
	write(t, filepath.Join(connectors.GlobalDir(""), "kinds", "supabase.yaml"), supabaseKind)
	write(t, filepath.Join(connectors.WorkspaceDir(ws), "kinds", "postgres.yaml"), strings.Replace(supabaseKind, "id: supabase", "id: postgres", 1))
	for name, text := range map[string]string{
		"bad-backend":   "id: bad-backend\ncategory: databases\nname: X\nbackend: nope\n",
		"bad-category":  "id: bad-category\ncategory: email\nname: X\nbackend: toolbox\n",
		"bad-cap":       "id: bad-cap\ncategory: databases\nname: X\nbackend: toolbox\ncapabilities: [fly]\n",
		"no-queries":    "id: no-queries\ncategory: databases\nname: X\nbackend: toolbox\ncapabilities: [queries]\n",
		"no-schema":     "id: no-schema\ncategory: databases\nname: X\nbackend: toolbox\ncapabilities: [schema]\n",
		"lone-builder":  "id: lone-builder\ncategory: databases\nname: X\nbackend: toolbox\ncapabilities: [query-builder]\n",
		"bad-field":     "id: bad-field\ncategory: databases\nname: X\nbackend: toolbox\nfields: [{key: a, type: colour}, {key: a}]\n",
		"bad-tool":      "id: bad-tool\ncategory: databases\nname: X\nbackend: toolbox\ntools: [{id: Bad}]\n",
		"bad-ph":        "id: bad-ph\ncategory: databases\nname: X\nbackend: toolbox\nqueries: {type: q, placeholders: '?'}\n",
		"unnamed":       "id: unnamed\ncategory: databases\nbackend: toolbox\n",
		"unknown-field": "id: unknown-field\ncategory: databases\nname: X\nbackend: toolbox\ncolour: red\n",
		"Bad ID":        "id: Bad ID\ncategory: databases\nname: X\nbackend: toolbox\n",
	} {
		write(t, filepath.Join(connectors.WorkspaceDir(ws), "kinds", strings.ReplaceAll(name, " ", "_")+".yaml"), text)
	}
	cat := connectors.Load("", ws)
	k, ok := cat.Kind("supabase")
	require.True(t, ok, "a global kind is added")
	assert.Equal(t, connectors.OriginGlobal, k.Origin)
	assert.NotEmpty(t, k.Path)
	pg, ok := cat.Kind("postgres")
	require.True(t, ok)
	assert.Equal(t, connectors.OriginBuiltin, pg.Origin, "the built-in one stays")

	problems := map[string]string{}
	for _, k := range cat.Kinds {
		if k.Problem != "" {
			problems[k.ID+"@"+k.Origin] = k.Problem
		}
	}
	for id, want := range map[string]string{
		"postgres@workspace":      "already",
		"bad-backend@workspace":   "backend",
		"bad-category@workspace":  "category",
		"bad-cap@workspace":       "fly",
		"no-queries@workspace":    "queries",
		"no-schema@workspace":     "schema",
		"lone-builder@workspace":  "needs schema and queries",
		"bad-field@workspace":     "type is",
		"bad-tool@workspace":      "tool",
		"bad-ph@workspace":        "placeholders",
		"unnamed@workspace":       "name",
		"unknown-field@workspace": "colour",
		"Bad ID@workspace":        "lower case",
	} {
		assert.Contains(t, problems[id], want, id)
	}
	assert.Len(t, connectors.DescriptorFiles("", ws), 14)

	write(t, filepath.Join(connectors.WorkspaceDir(ws), "categories", "email.yaml"), "id: email\nname: Email\nserver: email\nprefix: mail\nsecretPrefix: MAIL\nbackend: toolbox\n")
	write(t, filepath.Join(connectors.WorkspaceDir(ws), "categories", "kinds.yaml"), "id: kinds\nname: X\nserver: x\nprefix: x\nsecretPrefix: x\nbackend: nope\n")
	write(t, filepath.Join(connectors.WorkspaceDir(ws), "categories", "twin.yaml"), "id: twin\nname: X\nserver: databases\nprefix: db\nsecretPrefix: DB\nbackend: toolbox\n")
	write(t, filepath.Join(connectors.WorkspaceDir(ws), "categories", "unknown.yaml"), "id: unknown\ncolour: red\n")
	write(t, filepath.Join(connectors.WorkspaceDir(ws), "categories", "zz-again.yaml"), "id: email\nname: Again\nserver: again\nprefix: again\nsecretPrefix: AGAIN\nbackend: toolbox\n")
	write(t, filepath.Join(connectors.WorkspaceDir(ws), "categories", "names.yaml"), "id: names\nname: X\nserver: Bad-Server\nprefix: a-b\nsecretPrefix: NAMES\nbackend: toolbox\n")
	cat = connectors.Load("", ws)
	_, ok = cat.Category("email")
	assert.True(t, ok, "a category is added")
	_, ok = cat.Kind("bad-category")
	assert.True(t, ok, "its kinds can be used")
	var catProblems []string
	for _, c := range cat.Categories {
		catProblems = append(catProblems, c.Problem)
	}
	all := strings.Join(catProblems, "\n")
	for _, want := range []string{"not kinds or categories", "secretPrefix", "backend", "has its server or prefix", "colour", `there's a category "email" already`, `server "Bad-Server"`, `prefix "a-b"`} {
		assert.Contains(t, all, want)
	}
}

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

// Save writes the file and keeps the password as a secret; renaming moves
// both; Delete removes both; List reads them back, global ones first.
func TestSaveAndDelete(t *testing.T) {
	ws := home(t)
	cat := connectors.Load("", ws)
	c := connectors.Connection{Name: "shop", Category: "databases", Scope: connectors.ScopeWorkspace, Kind: "postgres", Settings: map[string]string{"host": "h", "database": "d", "user": "u"}, ReadOnly: true, Tools: []string{"execute_sql"}}
	_, problems, err := connectors.Save(cat, "", ws, c, nil, "")
	require.NoError(t, err)
	assert.Contains(t, problems, "password is required")

	saved, problems, err := connectors.Save(cat, "", ws, c, map[string]string{"password": "s3cret-pass"}, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, filepath.Join(connectors.WorkspaceDir(ws), "databases", "shop.yaml"), saved.Path)
	b, err := os.ReadFile(saved.Path)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "s3cret-pass", "never the value")
	assert.Contains(t, string(b), "# kind: postgres")
	db, _ := cat.Category("databases")
	assert.True(t, connectors.HasSecret("", ws, db, saved, "password"))
	assert.Equal(t, "${DB_SHOP_PASSWORD}", saved.Settings["password"])

	_, problems, err = connectors.Save(cat, "", ws, c, nil, "")
	require.NoError(t, err)
	assert.Contains(t, strings.Join(problems, "\n"), "already")

	c.Name = "store"
	moved, problems, err := connectors.Save(cat, "", ws, c, nil, "shop")
	require.NoError(t, err)
	require.Empty(t, problems, "the password is kept")
	assert.True(t, connectors.HasSecret("", ws, db, moved, "password"))
	assert.False(t, connectors.HasSecret("", ws, db, saved, "password"), "the old one's went with it")
	assert.NoFileExists(t, saved.Path)

	_, _, err = connectors.Save(cat, "", ws, c, nil, "gone")
	assert.ErrorIs(t, err, connectors.ErrUnknown)

	list := connectors.List(cat, "", ws)
	require.Len(t, list, 1)
	assert.Equal(t, "store", list[0].Name)
	assert.Equal(t, "databases", list[0].Category)
	assert.Equal(t, connectors.ScopeWorkspace, list[0].Scope)

	g := connectors.Connection{Name: "notes", Category: "databases", Scope: connectors.ScopeGlobal, Kind: "sqlite", Settings: map[string]string{"database": "x.db"}}
	_, problems, err = connectors.Save(cat, "", "", g, nil, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	list = connectors.List(cat, "", ws)
	assert.Equal(t, []string{connectors.ScopeGlobal, connectors.ScopeWorkspace}, []string{list[0].Scope, list[1].Scope}, "the global ones first")

	require.NoError(t, connectors.Delete(cat, "", ws, "databases", connectors.ScopeWorkspace, "store"))
	assert.False(t, connectors.HasSecret("", ws, db, moved, "password"))
	assert.ErrorIs(t, connectors.Delete(cat, "", ws, "databases", connectors.ScopeWorkspace, "store"), connectors.ErrUnknown)
	assert.ErrorIs(t, connectors.Delete(cat, "", ws, "email", connectors.ScopeWorkspace, "x"), connectors.ErrUnknown)

	for _, tc := range []struct {
		name string
		conn connectors.Connection
		ws   string
		want string
	}{
		{"no workspace", connectors.Connection{Name: "x", Category: "databases", Scope: connectors.ScopeWorkspace, Kind: "sqlite", Settings: map[string]string{"database": "x"}}, "", "no workspace"},
		{"no category", connectors.Connection{Name: "x", Category: "email"}, ws, "category"},
		{"no kind", connectors.Connection{Name: "x", Category: "databases", Kind: "mysql"}, ws, "one of alloydb-postgres"},
		{"a bad scope", connectors.Connection{Name: "x", Category: "databases", Scope: "team", Kind: "sqlite", Settings: map[string]string{"database": "x"}}, ws, "scope"},
		{"broken YAML", connectors.Connection{Name: "raw", Category: "databases", Custom: true, Text: "kind: ["}, ws, "YAML"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, problems, err := connectors.Save(cat, "", tc.ws, tc.conn, nil, "")
			require.NoError(t, err)
			assert.Contains(t, strings.Join(problems, "\n"), tc.want)
		})
	}
}

// Check lists what keeps a connection from being saved.
func TestCheck(t *testing.T) {
	cat := connectors.Load("", "")
	good := connectors.Connection{Name: "shop", Category: "databases", Kind: "sqlite", Settings: map[string]string{"database": "x.db"}}
	k, _ := cat.Kind("sqlite")
	b, _ := connectors.BackendOf("toolbox")
	assert.Empty(t, connectors.Check(b, &k, good))
	for _, tc := range []struct {
		name string
		edit func(*connectors.Connection)
		kind string
		want string
	}{
		{"a bad name", func(c *connectors.Connection) { c.Name = "Shop!" }, "sqlite", "name"},
		{"a missing setting", func(c *connectors.Connection) { c.Settings = nil }, "sqlite", "database is required"},
		{"an unknown tool", func(c *connectors.Connection) { c.Tools = []string{"nope"} }, "sqlite", "no ready-made tool"},
		{"a query named as a tool", func(c *connectors.Connection) {
			c.Queries = []connectors.Query{{Name: "list_tables", Description: "d", Statement: "SELECT 1"}}
		}, "sqlite", "taken"},
		{"a bad query name", func(c *connectors.Connection) {
			c.Queries = []connectors.Query{{Name: "Q", Description: "d", Statement: "SELECT 1"}}
		}, "sqlite", "lower case"},
		{"an empty query", func(c *connectors.Connection) { c.Queries = []connectors.Query{{Name: "q", Description: "d"}} }, "sqlite", "empty"},
		{"an undescribed query", func(c *connectors.Connection) { c.Queries = []connectors.Query{{Name: "q", Statement: "SELECT 1"}} }, "sqlite", "say what"},
		{"a bad parameter", func(c *connectors.Connection) {
			c.Queries = []connectors.Query{{Name: "q", Description: "d", Statement: "SELECT 1", Params: []connectors.Param{{Name: "N", Type: "date"}}}}
		}, "sqlite", "type is"},
		{"a number that isn't", func(c *connectors.Connection) {
			c.Kind, c.Settings = "bigquery", map[string]string{"project": "p", "maximumBytesBilled": "1 GB"}
		}, "bigquery", "whole number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := good
			tc.edit(&c)
			k, _ := cat.Kind(tc.kind)
			assert.Contains(t, strings.Join(connectors.Check(b, &k, c), "\n"), tc.want)
		})
	}
	noRO := k
	noRO.ReadOnly, noRO.Capabilities = nil, nil
	c := good
	c.ReadOnly, c.Queries = true, []connectors.Query{{Name: "q", Description: "d", Statement: "SELECT 1"}}
	all := strings.Join(connectors.Check(b, &noRO, c), "\n")
	assert.Contains(t, all, "can't be read-only")
	assert.Contains(t, all, "no queries of your own")
}

// The capabilities run through the kind's backend: a SQLite connection is
// tested, tried and read; a kind without a capability refuses it; a
// schema of no tables is ErrNoTables.
func TestCapabilities(t *testing.T) {
	ws := home(t)
	write(t, filepath.Join(connectors.GlobalDir(""), "kinds", "supabase.yaml"), supabaseKind)
	cat := connectors.Load("", ws)
	env := connectors.Env{Workspace: ws}
	ctx := context.Background()
	c := connectors.Connection{Name: "kitchen", Category: "databases", Scope: connectors.ScopeWorkspace, Kind: "sqlite", Settings: map[string]string{"database": recipes(t)}, ReadOnly: true, Tools: []string{"list_tables"},
		Queries: []connectors.Query{{Name: "count", Description: "How many", Statement: "SELECT count(*) AS n FROM recipes"}}}
	tools, err := connectors.TestConnection(ctx, cat, env, c)
	require.NoError(t, err)
	assert.Len(t, tools, 2)
	out, isErr, err := connectors.TryTool(ctx, cat, env, c, "count", nil)
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.JSONEq(t, `{"n": 2}`, out)
	tables, err := connectors.ReadSchema(ctx, cat, env, c)
	require.NoError(t, err)
	assert.Equal(t, "recipes", tables[0].Name)

	empty := filepath.Join(t.TempDir(), "empty.db")
	edb, err := sql.Open("sqlite3", empty)
	require.NoError(t, err)
	_, err = edb.Exec("PRAGMA user_version = 1")
	require.NoError(t, err)
	require.NoError(t, edb.Close())
	c.Settings = map[string]string{"database": empty}
	_, err = connectors.ReadSchema(ctx, cat, env, c)
	assert.ErrorIs(t, err, connectors.ErrNoTables)
	c.Settings = map[string]string{"database": filepath.Join(t.TempDir(), "missing", "x.db")}
	_, err = connectors.ReadSchema(ctx, cat, env, c)
	assert.ErrorIs(t, err, connectors.ErrNoTables, "can't connect: none read")

	sb := connectors.Connection{Name: "app", Category: "databases", Kind: "supabase", Settings: map[string]string{"host": "h", "user": "u"}}
	_, err = connectors.ReadSchema(ctx, cat, env, sb)
	assert.ErrorContains(t, err, "can't schema", "its kind has no schema capability")
	_, err = connectors.TestConnection(ctx, cat, env, connectors.Connection{Name: "Bad", Category: "databases", Kind: "sqlite"})
	assert.Error(t, err)
	_, err = connectors.TestConnection(ctx, cat, env, connectors.Connection{Name: "x", Category: "email"})
	assert.Error(t, err)
}

// With serves each category's files by their backends in place of the
// old in-process servers; a server of the user's named databases wins;
// the fingerprint follows every connector file.
func TestWith(t *testing.T) {
	ws := home(t)
	user := config.MCPServerConfig{Name: "github", Command: "gh"}
	stale := config.MCPServerConfig{Name: "databases", InProcess: "toolbox", Fingerprint: "old"}
	assert.Equal(t, []config.MCPServerConfig{user}, connectors.With([]config.MCPServerConfig{user, stale}, "", ws, nil, nil, nil), "no files: none")

	file := filepath.Join(connectors.WorkspaceDir(ws), "databases", "scratch.yaml")
	write(t, file, "kind: source\nname: scratch\ntype: sqlite\ndatabase: /tmp/none.db\n")
	write(t, filepath.Join(connectors.WorkspaceDir(ws), "databases", "notes.md"), "not a connection file")
	out := connectors.With([]config.MCPServerConfig{user, stale}, "", ws, nil, nil, nil)
	require.Len(t, out, 2)
	assert.Equal(t, "databases", out[1].Name)
	assert.NotEqual(t, "old", out[1].Fingerprint)

	var asked []string
	off := func(key string) bool { asked = append(asked, key); return false }
	assert.Equal(t, []config.MCPServerConfig{user}, connectors.With([]config.MCPServerConfig{user, stale}, "", ws, nil, off, nil), "turned off: not served")
	assert.Equal(t, []string{"workspace/databases/scratch"}, asked)
	assert.Equal(t, "workspace/databases/scratch", connectors.Connection{Scope: "workspace", Category: "databases", Name: "scratch"}.Key())

	mine := config.MCPServerConfig{Name: "databases", Command: "my-db-server"}
	var warned []string
	out = connectors.With([]config.MCPServerConfig{mine}, "", ws, nil, nil, func(s string) { warned = append(warned, s) })
	assert.Equal(t, []config.MCPServerConfig{mine}, out)
	require.Len(t, warned, 1)
	assert.Contains(t, warned[0], `named "databases"`)

	files := connectors.AllFiles("", ws)
	assert.Equal(t, []string{file}, files)
	before := connectors.Fingerprint(files)
	time.Sleep(10 * time.Millisecond)
	write(t, file, "kind: source\nname: scratch\ntype: sqlite\ndatabase: /tmp/other.db\n")
	assert.NotEqual(t, before, connectors.Fingerprint(files))
	assert.NotEmpty(t, connectors.Hash(file))
	assert.Empty(t, connectors.Hash(filepath.Join(ws, "missing")))

	s := connectors.Serving{Local: []string{"a", "b"}, Trusted: func(f string) bool { return f == "a" }}
	trusted, untrusted := s.Split()
	assert.Equal(t, []string{"a"}, trusted)
	assert.Equal(t, []string{"b"}, untrusted)
	trusted, untrusted = connectors.Serving{Local: []string{"a"}}.Split()
	assert.Empty(t, trusted, "nil trusts none")
	assert.Equal(t, []string{"a"}, untrusted)
}

// Make global refuses what it can't move, saying why, and moves nothing:
// a category or name that isn't there, a file that can't be read as a
// connection, one more than the form describes; Save and Prepare say
// what's wrong with a kind or category that isn't there.
func TestMakeGlobalAndPrepareRefuse(t *testing.T) {
	ws := home(t)
	cat := connectors.Load("", ws)
	dir := filepath.Join(connectors.WorkspaceDir(ws), "databases")

	_, _, err := connectors.MakeGlobal("", ws, "email", "inbox")
	assert.ErrorIs(t, err, connectors.ErrUnknown)
	_, _, err = connectors.MakeGlobal("", ws, "databases", "gone")
	assert.ErrorIs(t, err, connectors.ErrUnknown)

	write(t, filepath.Join(dir, "broken.yaml"), "kind: [")
	_, problems, err := connectors.MakeGlobal("", ws, "databases", "broken")
	require.NoError(t, err)
	require.Len(t, problems, 1)
	assert.NoFileExists(t, filepath.Join(connectors.GlobalDir(""), "databases", "broken.yaml"))

	write(t, filepath.Join(dir, "two.yaml"), "kind: source\nname: a\ntype: sqlite\ndatabase: /tmp/a.db\n---\nkind: source\nname: b\ntype: sqlite\ndatabase: /tmp/b.db\n")
	_, problems, err = connectors.MakeGlobal("", ws, "databases", "two")
	require.NoError(t, err)
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "by hand")
	assert.FileExists(t, filepath.Join(dir, "two.yaml"), "nothing moved")

	_, problems, err = connectors.Save(cat, "", ws, connectors.Connection{Name: "x", Category: "databases", Kind: "mysql"}, nil, "")
	require.NoError(t, err)
	assert.Contains(t, strings.Join(problems, " "), `kind "mysql": one of`)

	_, _, _, err = connectors.Prepare(cat, connectors.Env{Workspace: ws}, connectors.Connection{Name: "x", Category: "email"})
	assert.ErrorContains(t, err, `category "email" isn't there`)
	_, _, _, err = connectors.Prepare(cat, connectors.Env{Workspace: ws}, connectors.Connection{Name: "Bad Name", Category: "databases", Kind: "sqlite", Settings: map[string]string{"database": "/tmp/x.db"}})
	assert.Error(t, err, "a name that can't be saved")
	_, _, _, err = connectors.Prepare(cat, connectors.Env{Workspace: ws}, connectors.Connection{Name: "x", Category: "databases", Kind: "mysql"})
	assert.ErrorContains(t, err, `kind "mysql"`)
}
