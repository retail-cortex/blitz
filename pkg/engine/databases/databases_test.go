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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/config"
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
	_, err = db.Exec(`CREATE TABLE recipes(name TEXT); INSERT INTO recipes VALUES ('soup'), ('bread')`)
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

const scratch = `kind: source
name: scratch
type: sqlite
database: ${SCRATCH_DB:/tmp/none.db}
---
kind: tool
name: scratch_sql
type: sqlite-execute-sql
source: scratch
description: Run any SQL on the scratch database
`

// The global and the workspace's files make one server, their ${NAME}s
// the workspace's secret, else the global one; tools on a read-only
// source run unasked.
func TestConfig(t *testing.T) {
	ws := home(t)
	_, ok := Config("", ws)
	assert.False(t, ok, "no database files, no server")

	write(t, filepath.Join(GlobalDir(""), "kitchen.yaml"), kitchen)
	write(t, filepath.Join(WorkspaceDir(ws), "scratch.yml"), scratch)
	write(t, filepath.Join(WorkspaceDir(ws), "notes.md"), "not a database file")
	v := config.Vault("")
	require.NoError(t, v.Set(secrets.GlobalScope, "KITCHEN_DB", "/global/kitchen.db"))
	require.NoError(t, v.Set(config.SecretScope("", ws), "KITCHEN_DB", recipes(t)))

	c, ok := Config("", ws)
	require.True(t, ok)
	assert.Equal(t, Server, c.Name)
	assert.Equal(t, Server, c.InProcess)
	assert.Equal(t, Prefix, c.Prefix)
	assert.Equal(t, []string{"*"}, c.Agents)
	assert.Equal(t, []string{filepath.Join(GlobalDir(""), "kitchen.yaml"), filepath.Join(WorkspaceDir(ws), "scratch.yml")}, c.Args)
	assert.Equal(t, []string{"KITCHEN_DB", "SCRATCH_DB"}, Names(c.Args))
	assert.NotEqual(t, "/global/kitchen.db", c.Env["KITCHEN_DB"], "the workspace's own first")
	assert.NotContains(t, c.Env, "SCRATCH_DB", "no secret: its default")
	assert.Equal(t, []string{"count_recipes"}, c.ReadOnlyTools)

	g, ok := Config("", "")
	require.True(t, ok, "the global files alone")
	assert.Equal(t, "/global/kitchen.db", g.Env["KITCHEN_DB"])

	before := c.Fingerprint
	time.Sleep(10 * time.Millisecond)
	write(t, filepath.Join(WorkspaceDir(ws), "scratch.yml"), scratch+"\n")
	assert.NotEqual(t, before, Fingerprint(Files("", ws)), "a changed file")
}

// With puts the databases server in place of the old one; a server of the
// user's named databases wins.
func TestWith(t *testing.T) {
	ws := home(t)
	user := config.MCPServerConfig{Name: "github", Command: "gh"}
	stale := config.MCPServerConfig{Name: Server, InProcess: Server, Fingerprint: "old"}
	assert.Equal(t, []config.MCPServerConfig{user}, With([]config.MCPServerConfig{user, stale}, "", ws, nil), "no files: none")

	write(t, filepath.Join(WorkspaceDir(ws), "scratch.yaml"), scratch)
	out := With([]config.MCPServerConfig{user, stale}, "", ws, nil)
	require.Len(t, out, 2)
	assert.NotEqual(t, "old", out[1].Fingerprint)

	mine := config.MCPServerConfig{Name: Server, Command: "my-db-server"}
	var warned []string
	out = With([]config.MCPServerConfig{mine}, "", ws, func(s string) { warned = append(warned, s) })
	assert.Equal(t, []config.MCPServerConfig{mine}, out)
	require.Len(t, warned, 1)
	assert.Contains(t, warned[0], "named \"databases\"")
}

// The transport starts Toolbox in-process: its tools listed and called
// over MCP; a database that can't be opened fails the connection; a
// cancelled one gives up.
func TestTransport(t *testing.T) {
	ws := home(t)
	write(t, filepath.Join(WorkspaceDir(ws), "kitchen.yaml"), kitchen)
	require.NoError(t, config.Vault("").Set(config.SecretScope("", ws), "KITCHEN_DB", recipes(t)))
	c, ok := Config("", ws)
	require.True(t, ok)

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
