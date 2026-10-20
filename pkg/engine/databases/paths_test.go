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
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
)

// A path setting is resolved against the folder given, ~/ as the home
// folder; what isn't a file's path is left alone.
func TestResolvePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tc := range []struct {
		name, in, want string
		file           bool
	}{
		{name: "relative", in: "./kitchen.db", want: "/w/kitchen.db", file: true},
		{name: "in a folder", in: "data/kitchen.db", want: "/w/data/kitchen.db", file: true},
		{name: "up a folder", in: "../shared/k.db", want: "/shared/k.db", file: true},
		{name: "absolute", in: "/srv/k.db", want: "/srv/k.db", file: true},
		{name: "home", in: "~/k.db", want: filepath.Join(home, "k.db"), file: true},
		{name: "in memory", in: ":memory:", want: ":memory:"},
		{name: "a URI", in: "file:k.db?mode=ro", want: "file:k.db?mode=ro"},
		{name: "a secret filled later", in: "${KITCHEN_DB}", want: "${KITCHEN_DB}"},
		{name: "empty", in: "", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, file := connectors.ResolvePath(tc.in, "/w")
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.file, file)
		})
	}
}

// A workspace connection's paths start at its workspace, a global one's
// at the home folder; the path settings are the kinds' path fields.
func TestBaseAndKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	assert.Equal(t, "/w/shop", baseOf("/w/shop/.agents/connectors/databases/kitchen.yaml"))
	assert.Equal(t, home, baseOf(filepath.Join(home, ".blitz", "connectors", "databases", "kitchen.yaml")))
	assert.Equal(t, home, baseOf("/tmp/blitz-connector-1/kitchen.yaml"))
	assert.Equal(t, "/w", scopeBase(connectors.ScopeWorkspace, "/w"))
	assert.Equal(t, "/w", scopeBase("", "/w"), "a new one is the workspace's")
	assert.Equal(t, home, scopeBase(connectors.ScopeGlobal, "/w"))
	assert.Equal(t, home, scopeBase(connectors.ScopeWorkspace, ""))
	_, _, kinds := catalog(t)
	assert.Equal(t, map[string][]string{"sqlite": {"database"}}, pathKeys(kinds))
}

// The rewrite resolves a source's path settings and leaves the rest (its
// tools, another type's settings, a file that isn't YAML) as they are; a
// file that isn't there is an error rather than an empty database.
func TestResolvePaths(t *testing.T) {
	ws := t.TempDir()
	write(t, filepath.Join(ws, "data", "kitchen.db"), "")
	keys := map[string][]string{"sqlite": {"database"}}
	src := "# kind: sqlite\nkind: source\nname: kitchen\ntype: sqlite\ndatabase: ./data/kitchen.db\n---\nkind: tool\nname: count\ntype: sqlite-sql\nsource: kitchen\ndatabase: ./not/a/source\nstatement: SELECT 1\n---\nkind: source\nname: shop\ntype: postgres\ndatabase: shop\n"
	out, err := resolvePaths([]byte(src), keys, ws)
	require.NoError(t, err)
	assert.Contains(t, string(out), "database: "+filepath.Join(ws, "data", "kitchen.db"))
	assert.Contains(t, string(out), "database: ./not/a/source", "a tool's settings aren't a source's")
	assert.Contains(t, string(out), "database: shop", "postgres names a database, not a file")

	same := "kind: source\nname: kitchen\ntype: sqlite\ndatabase: ${KITCHEN_DB}\n"
	out, err = resolvePaths([]byte(same), keys, ws)
	require.NoError(t, err)
	assert.Equal(t, same, string(out), "nothing to resolve: as it is")
	out, err = resolvePaths([]byte("kind: ["), keys, ws)
	require.NoError(t, err)
	assert.Equal(t, "kind: [", string(out), "Toolbox says what's wrong with it")
	out, err = resolvePaths([]byte(src), nil, ws)
	require.NoError(t, err)
	assert.Equal(t, src, string(out))

	_, err = resolvePaths([]byte("kind: source\nname: k\ntype: sqlite\ndatabase: ./gone.db\n"), keys, ws)
	assert.ErrorIs(t, err, errNoFile)
	assert.ErrorContains(t, err, filepath.Join(ws, "gone.db"))
	assert.NoFileExists(t, filepath.Join(ws, "gone.db"), "no empty database is made")
}

// ./kitchen.db works where the connection belongs: a workspace's, served
// to its agents and tested in Studio, from the workspace's folder; a
// global one's from the home folder; a missing one says so.
func TestRelativeDatabase(t *testing.T) {
	ws := home(t)
	homeDir, _ := os.UserHomeDir()
	db := recipes(t)
	data, err := os.ReadFile(db)
	require.NoError(t, err)
	write(t, filepath.Join(ws, "kitchen.db"), string(data))
	write(t, filepath.Join(homeDir, "pantry.db"), string(data))
	cat, category, _ := catalog(t)
	k := kindOf(t, "sqlite")
	ctx := context.Background()

	// Served: the file in .agents/connectors/databases, its path from the workspace.
	c := connectors.Connection{Name: "kitchen", Category: "databases", Scope: connectors.ScopeWorkspace, Kind: "sqlite", Settings: map[string]string{"database": "./kitchen.db"}, ReadOnly: true,
		Queries: []connectors.Query{{Name: "count", Description: "How many", Statement: "SELECT count(*) AS n FROM recipes"}}}
	text, err := Backend{}.Render(category, k, c)
	require.NoError(t, err)
	assert.Contains(t, text, "database: ./kitchen.db", "the file keeps the path as written")
	file := filepath.Join(connectors.WorkspaceDir(ws), "databases", "kitchen.yaml")
	write(t, file, text)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, Transport(config.MCPServerConfig{Args: []string{file}}), nil)
	require.NoError(t, err)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "kitchen_count"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"n": 2}`, res.Content[0].(*mcp.TextContent).Text)
	require.NoError(t, cs.Close())

	// Tested in Studio: the same, though it's written to a temporary folder.
	env := connectors.Env{Workspace: ws, Category: category}
	out, _, err := Backend{}.Try(ctx, env, k, c, "count", nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"n": 2}`, out)

	// A global one: from the home folder.
	g := c
	g.Name, g.Scope, g.Settings = "pantry", connectors.ScopeGlobal, map[string]string{"database": "pantry.db"}
	out, _, err = Backend{}.Try(ctx, connectors.Env{Category: category}, k, g, "count", nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"n": 2}`, out)

	// Missing, read-only or not: said, and no empty file made.
	c.ReadOnly, c.Settings = false, map[string]string{"database": "./gone.db"}
	_, err = Backend{}.Test(ctx, env, k, c)
	assert.ErrorContains(t, err, "no file at "+filepath.Join(ws, "gone.db"))
	assert.NoFileExists(t, filepath.Join(ws, "gone.db"))
	_ = cat
}
