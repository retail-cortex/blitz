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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// shop is a Postgres connection with a password, saved without
// connecting.
func shop() connectors.Connection {
	return connectors.Connection{Name: "shop", Category: "databases", Scope: connectors.ScopeWorkspace, Kind: "postgres", ReadOnly: true,
		Settings: map[string]string{"host": "db.internal", "port": "5432", "database": "shop", "user": "reader"}, Tools: []string{"list_tables"}}
}

// Studio changes only the workspace's own connections: a global one is
// Settings' (CON-18).
func TestStudioLeavesGlobalConnectors(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	c := kitchen(t)
	c.Scope = connectors.ScopeGlobal
	_, _, err := w.SaveConnector(ctx, c, nil, "")
	assert.ErrorIs(t, err, ErrGlobalConnector)
	assert.ErrorIs(t, w.DeleteConnector(ctx, "databases", connectors.ScopeGlobal, "kitchen"), ErrGlobalConnector)

	c.Scope = ""
	saved, problems, err := w.SaveConnector(ctx, c, nil, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, connectors.ScopeWorkspace, saved.Scope, "no scope: the workspace's")
}

// Make Global moves a trusted workspace connection, with its password,
// to the global folder; it stays off here if it was. An untrusted one,
// a name a global one has, or a kind of the workspace's own can't be.
func TestMakeConnectorGlobal(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	g := GlobalConnectors{Config: w.cfg}

	saved, problems, err := w.SaveConnector(ctx, shop(), map[string]string{"password": "s3cret"}, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	require.NoError(t, w.SetConnectorOn(ctx, "databases", connectors.ScopeWorkspace, "shop", false))

	// A clone's copy: not trusted.
	text, err := os.ReadFile(saved.Path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(saved.Path, append(text, '\n'), 0o600))
	_, _, err = w.MakeConnectorGlobal(ctx, "databases", "shop")
	assert.ErrorIs(t, err, ErrUntrustedConnector)
	_, _, err = w.SaveConnector(ctx, w.Connectors()[0], nil, "shop")
	require.NoError(t, err)

	moved, problems, err := w.MakeConnectorGlobal(ctx, "databases", "shop")
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, connectors.ScopeGlobal, moved.Scope)
	assert.False(t, moved.On, "still off here")
	assert.NoFileExists(t, saved.Path)
	assert.FileExists(t, moved.Path)
	assert.True(t, g.HasSecret(moved, "password"), "the password moved")
	assert.False(t, w.HasConnectorSecret(saved, "password"), "and left the workspace")
	require.Len(t, g.List(), 1)

	_, _, err = w.MakeConnectorGlobal(ctx, "databases", "shop")
	assert.ErrorIs(t, err, connectors.ErrUnknown)
	_, _, err = w.SaveConnector(ctx, shop(), map[string]string{"password": "x"}, "")
	require.NoError(t, err)
	_, problems, err = w.MakeConnectorGlobal(ctx, "databases", "shop")
	require.NoError(t, err)
	assert.Contains(t, problems[0], "a global connection named")

	// A relative path meant the workspace's folder: made global, it still names that file.
	_, problems, err = w.SaveConnector(ctx, connectors.Connection{Name: "notes", Category: "databases", Kind: "sqlite", Settings: map[string]string{"database": "./data/notes.db"}}, nil, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	notes, problems, err := w.MakeConnectorGlobal(ctx, "databases", "notes")
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, filepath.Join(w.Dir(), "data", "notes.db"), notes.Settings["database"])

	kinds := filepath.Join(connectors.WorkspaceDir(w.Dir()), "kinds")
	require.NoError(t, os.MkdirAll(kinds, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(kinds, "pantry.yaml"), []byte("id: pantry\ncategory: databases\nname: Pantry\nbackend: toolbox\nsource: sqlite\ncapabilities: [test]\nfields:\n  - {key: database, type: path, required: true}\n"), 0o600))
	_, problems, err = w.SaveConnector(ctx, connectors.Connection{Name: "pantry", Category: "databases", Kind: "pantry", Settings: map[string]string{"database": kitchenDB(t)}}, nil, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	_, problems, err = w.MakeConnectorGlobal(ctx, "databases", "pantry")
	require.NoError(t, err)
	assert.Contains(t, problems[0], "this workspace's own")
}

// Settings' global connections need no workspace: saved, renamed (whether
// each workspace has it on goes with it), tested, read and drafted for,
// and deleted.
func TestGlobalConnectors(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	llm := &promptLLM{reply: `{"name": "slow", "statement": "SELECT name FROM recipes WHERE minutes > 60"}`}
	g := GlobalConnectors{Config: w.cfg, NewModel: func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }}

	c := kitchen(t)
	c.Scope = connectors.ScopeWorkspace // Settings' are global whatever it says
	saved, problems, err := g.Save(c, nil, "")
	require.NoError(t, err)
	require.Empty(t, problems)
	assert.Equal(t, connectors.ScopeGlobal, saved.Scope)
	assert.True(t, saved.Trusted)
	assert.Contains(t, saved.Path, filepath.Join("connectors", "databases"))
	require.NoError(t, w.ReloadConnectors(ctx))
	assert.Equal(t, []string{"kitchen_count"}, readOnlyTools(w), "every workspace has it")

	require.NoError(t, w.SetConnectorOn(ctx, "databases", connectors.ScopeGlobal, "kitchen", false))
	saved.Name = "pantry"
	saved, _, err = g.Save(saved, nil, "kitchen")
	require.NoError(t, err)
	require.Len(t, g.List(), 1)
	assert.Equal(t, "pantry", g.List()[0].Name)
	require.NoError(t, w.ReloadConnectors(ctx))
	assert.Empty(t, readOnlyTools(w), "still off in the workspace after the rename")

	tools, err := g.Test(ctx, saved, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, tools)
	out, _, err := g.Try(ctx, saved, nil, "count", nil)
	require.NoError(t, err)
	assert.Contains(t, out, "2")
	tables, err := g.ReadSchema(ctx, saved, nil)
	require.NoError(t, err)
	assert.Len(t, tables, 1)
	d, err := g.DraftQuery(ctx, saved, nil, "the slow recipes")
	require.NoError(t, err)
	assert.Empty(t, d.Problem)
	assert.Equal(t, "slow", d.Query.Name)
	assert.Contains(t, llm.prompts[0], "pantry (SQLite)")
	assert.NotEmpty(t, g.Catalog().Kinds)

	require.NoError(t, g.Delete("databases", "pantry"))
	assert.Empty(t, g.List())
	assert.Empty(t, connectorScope(w.cfg).Off(w.Dir()), "forgotten everywhere")
	assert.ErrorIs(t, g.Delete("databases", "pantry"), connectors.ErrUnknown)
}
