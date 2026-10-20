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

package engine

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/databases"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// kitchenDB is a SQLite file with two recipes.
func kitchenDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kitchen.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE recipes(name TEXT); INSERT INTO recipes VALUES ('soup'), ('bread')`)
	require.NoError(t, err)
	return path
}

// A workspace's database file is served in-process: a turn's agent
// queries it (a read-only connection's tool, unasked), /mcp lists it, and
// saving or deleting a connection starts or stops the server.
func TestDatabasesInATurn(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	call := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "db__kitchen_count", Args: map[string]any{}}}}}
	w, _ := openTestWith(t, nil, call, text("two"))
	ctx := context.Background()
	assert.Empty(t, w.Databases())
	assert.Empty(t, w.DatabaseState().Name, "no files, no server")

	c := databases.Connection{Name: "kitchen", Scope: databases.ScopeWorkspace, Type: "sqlite", Settings: map[string]string{"database": kitchenDB(t)}, ReadOnly: true,
		Queries: []databases.Query{{Name: "count", Description: "How many recipes there are", Statement: "SELECT count(*) AS n FROM recipes"}}}
	saved, problems, err := w.SaveDatabase(ctx, c, nil, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, filepath.Join(w.Dir(), ".agents", "databases", "kitchen.yaml"), saved.Path)
	require.Len(t, w.Databases(), 1)

	servers := w.ListMCPServers()
	require.Len(t, servers, 1)
	assert.Equal(t, databases.Server, servers[0].Name)
	assert.Contains(t, servers[0].Target, "in-process: ")
	assert.Equal(t, "idle", w.DatabaseState().State)

	s, _ := w.NewSession()
	var result map[string]any
	w.Run(ctx, s.ID, api.Turn{Text: "how many recipes?"}, func(e api.Event) {
		if e.ToolResult != nil && e.ToolResult.Name == "db__kitchen_count" {
			result = e.ToolResult.Result
		}
	})
	require.NotNil(t, result)
	// With no one to ask, a tool that asks is refused: this one ran.
	assert.Contains(t, fmt.Sprint(result), `"n":2`, "a read-only database's tool runs unasked")
	assert.Equal(t, "connected", w.DatabaseState().State)

	tools, err := w.TestDatabase(ctx, c, nil)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	out, isErr, err := w.TryDatabaseTool(ctx, c, nil, "count", nil)
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.Contains(t, out, `"n":2`)

	require.NoError(t, w.DeleteDatabase(ctx, databases.ScopeWorkspace, "kitchen"))
	assert.Empty(t, w.ListMCPServers(), "no files: the server stops")
	assert.ErrorIs(t, w.DeleteDatabase(ctx, databases.ScopeWorkspace, "kitchen"), databases.ErrUnknown)
}

// A database file written by hand is picked up by the settings watch.
func TestDatabasesWatched(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	fastSettings(t)
	w := openTest(t)
	file := filepath.Join(databases.GlobalDir(""), "notes.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	require.NoError(t, os.WriteFile(file, []byte("kind: source\nname: notes\ntype: sqlite\ndatabase: "+kitchenDB(t)+"\n"), 0o600))
	require.Eventually(t, func() bool { return len(w.ListMCPServers()) == 1 }, 5*time.Second, 10*time.Millisecond, "the watch serves it")
}
