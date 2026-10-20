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
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// kitchenDB is a SQLite file with two recipes.
func kitchenDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kitchen.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE recipes(name TEXT, minutes INTEGER); INSERT INTO recipes VALUES ('soup', 30), ('bread', 90)`)
	require.NoError(t, err)
	return path
}

// kitchen is a read-only SQLite connection with a query of its own.
func kitchen(t *testing.T) connectors.Connection {
	return connectors.Connection{Name: "kitchen", Category: "databases", Scope: connectors.ScopeWorkspace, Kind: "sqlite", Settings: map[string]string{"database": kitchenDB(t)}, ReadOnly: true,
		Queries: []connectors.Query{{Name: "count", Description: "How many recipes there are", Statement: "SELECT count(*) AS n FROM recipes"}}}
}

// A workspace's connection is served in-process: a turn's agent queries
// it (a read-only connection's tool, unasked), /mcp lists it, and saving
// or deleting a connection starts or stops its category's server.
func TestConnectorsInATurn(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	call := &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "db__kitchen_count", Args: map[string]any{}}}}}
	w, _ := openTestWith(t, nil, call, text("two"))
	ctx := context.Background()
	assert.Empty(t, w.Connectors())
	assert.Empty(t, w.ConnectorState("databases").Name, "no files, no server")
	assert.Empty(t, w.ConnectorState("email").Name, "no such category")

	c := kitchen(t)
	saved, problems, err := w.SaveConnector(ctx, c, nil, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, filepath.Join(w.Dir(), ".agents", "connectors", "databases", "kitchen.yaml"), saved.Path)
	assert.True(t, saved.Trusted, "saved in Studio: trusted")
	list := w.Connectors()
	require.Len(t, list, 1)
	assert.True(t, list[0].Trusted)

	servers := w.ListMCPServers()
	require.Len(t, servers, 1)
	assert.Equal(t, "databases", servers[0].Name)
	assert.Contains(t, servers[0].Target, "in-process: ")
	assert.Equal(t, "idle", w.ConnectorState("databases").State)

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
	assert.Equal(t, "connected", w.ConnectorState("databases").State)

	tools, err := w.TestConnector(ctx, c, nil)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	out, isErr, err := w.TryConnectorTool(ctx, c, nil, "count", nil)
	require.NoError(t, err)
	assert.False(t, isErr)
	assert.Contains(t, out, `"n":2`)
	tables, err := w.ReadConnectorSchema(ctx, c, nil)
	require.NoError(t, err)
	assert.Equal(t, "recipes", tables[0].Name)
	assert.False(t, w.HasConnectorSecret(c, "password"))

	require.NoError(t, w.DeleteConnector(ctx, "databases", connectors.ScopeWorkspace, "kitchen"))
	assert.Empty(t, w.ListMCPServers(), "no files: the server stops")
	assert.ErrorIs(t, w.DeleteConnector(ctx, "databases", connectors.ScopeWorkspace, "kitchen"), connectors.ErrUnknown)
}

// readOnlyTools are the read-only tools the databases server runs unasked.
func readOnlyTools(w *Workspace) []string {
	for _, s := range w.mcpConfig().Servers {
		if s.Name == "databases" {
			return s.ReadOnlyTools
		}
	}
	return nil
}

// A workspace connection file the person didn't save in Studio (it came
// with the workspace) is served, but its tools ask first and it can't
// reach the global secrets, until they save it; trusting the project's
// settings trusts it too.
func TestUntrustedConnector(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	c := kitchen(t)
	saved, _, err := w.SaveConnector(ctx, c, nil, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"kitchen_count"}, readOnlyTools(w))

	// The same file, as a clone brings it: not the one trusted.
	text, err := os.ReadFile(saved.Path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(saved.Path, append(text, '\n'), 0o600))
	require.NoError(t, w.ReloadConnectors(ctx))
	assert.Empty(t, readOnlyTools(w), "every tool asks first")
	list := w.Connectors()
	require.Len(t, list, 1)
	assert.False(t, list[0].Trusted)

	_, _, err = w.SaveConnector(ctx, list[0], nil, "kitchen")
	require.NoError(t, err)
	assert.Equal(t, []string{"kitchen_count"}, readOnlyTools(w), "saved: trusted again")

	require.NoError(t, os.WriteFile(saved.Path, append(text, '\n', '\n'), 0o600))
	w.project.Loaded = true
	assert.True(t, w.connectorTrusted(saved.Path), "trusted project settings trust it")
}

// A connection file written by hand is picked up by the settings watch.
func TestConnectorsWatched(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	fastSettings(t)
	w := openTest(t)
	file := filepath.Join(connectors.GlobalDir(""), "databases", "notes.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
	require.NoError(t, os.WriteFile(file, []byte("kind: source\nname: notes\ntype: sqlite\ndatabase: "+kitchenDB(t)+"\n"), 0o600))
	require.Eventually(t, func() bool { return len(w.ListMCPServers()) == 1 }, 5*time.Second, 10*time.Millisecond, "the watch serves it")
	assert.True(t, slices.ContainsFunc(w.Connectors(), func(c connectors.Connection) bool { return c.Name == "notes" && c.Trusted }), "a global one is the person's own")
	assert.NotEmpty(t, w.ConnectorCatalog().Kinds)
	_ = config.ConfigDir("")
}

// A connection turned off in a workspace isn't served there, though its
// file stays; turning it on serves it again, a rename keeps the choice
// and a delete forgets it.
func TestConnectorOnOff(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	saved, _, err := w.SaveConnector(ctx, kitchen(t), nil, "")
	require.NoError(t, err)
	assert.True(t, saved.On, "on until turned off")
	assert.Equal(t, []string{"kitchen_count"}, readOnlyTools(w))

	require.NoError(t, w.SetConnectorOn(ctx, "databases", connectors.ScopeWorkspace, "kitchen", false))
	assert.Empty(t, readOnlyTools(w), "off: its tools are gone")
	assert.Empty(t, w.ListMCPServers(), "and its category's server, with nothing else on")
	list := w.Connectors()
	require.Len(t, list, 1)
	assert.False(t, list[0].On)
	assert.FileExists(t, saved.Path, "the file stays")
	_, err = w.TestConnector(ctx, list[0], nil)
	assert.NoError(t, err, "Studio can still test it")

	renamed := list[0]
	renamed.Name = "pantry"
	saved, _, err = w.SaveConnector(ctx, renamed, nil, "kitchen")
	require.NoError(t, err)
	assert.False(t, saved.On, "a rename keeps it off")
	assert.Empty(t, readOnlyTools(w))

	require.NoError(t, w.SetConnectorOn(ctx, "databases", connectors.ScopeWorkspace, "pantry", true))
	assert.Equal(t, []string{"pantry_count"}, readOnlyTools(w), "on again")

	require.NoError(t, w.SetConnectorOn(ctx, "databases", connectors.ScopeWorkspace, "pantry", false))
	require.NoError(t, w.DeleteConnector(ctx, "databases", connectors.ScopeWorkspace, "pantry"))
	assert.Empty(t, connectorScope(w.cfg).Off(w.Dir()), "a delete forgets it")
	assert.ErrorIs(t, w.SetConnectorOn(ctx, "databases", connectors.ScopeGlobal, "pantry", true), connectors.ErrUnknown)
}
