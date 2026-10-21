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
	"fmt"
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

// A connection's trust goes with its file: a rename or a delete forgets
// the old path, so a file written there later isn't trusted by it.
func TestConnectorTrustForgotten(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	saved, _, err := w.SaveConnector(ctx, kitchen(t), nil, "")
	require.NoError(t, err)
	trust := connectorTrust(w.cfg)
	require.True(t, trust.Reviewed(saved.Path, connectors.Hash(saved.Path)))

	renamed := saved
	renamed.Name = "pantry"
	moved, _, err := w.SaveConnector(ctx, renamed, nil, "kitchen")
	require.NoError(t, err)
	assert.True(t, trust.Reviewed(moved.Path, connectors.Hash(moved.Path)))
	write := func(path, text string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	}
	write(saved.Path, "x")
	assert.False(t, trust.Reviewed(saved.Path, connectors.Hash(saved.Path)), "the old path's trust went with the rename")

	require.NoError(t, w.DeleteConnector(ctx, "databases", connectors.ScopeWorkspace, "pantry"))
	assert.ErrorIs(t, w.DeleteConnector(ctx, "databases", connectors.ScopeWorkspace, "pantry"), connectors.ErrUnknown)
	write(moved.Path, "x")
	assert.False(t, trust.Reviewed(moved.Path, connectors.Hash(moved.Path)), "deleted: forgotten")
}

// The MCP settings stay as written (a secret's placeholder), and what
// runs has the secret filled in: a changed secret is used at the next
// reload, and the redactor still masks the value.
func TestMCPWrittenAndLive(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	require.NoError(t, config.Vault("").Set(secrets.GlobalScope, "GH_TOKEN", "first-token-value"))
	cfg := *w.settings()
	cfg.MCP.Servers = []config.MCPServerConfig{{Name: "gh", URL: "http://127.0.0.1:1/mcp", Headers: map[string]string{"Authorization": "Bearer {secrets:GH_TOKEN}"}}}
	require.NoError(t, w.ReloadMCP(ctx, &cfg))
	assert.Equal(t, "Bearer {secrets:GH_TOKEN}", w.mcpConfig().Servers[0].Headers["Authorization"], "as written")
	assert.Equal(t, "Bearer first-token-value", w.liveMCP()[0].Headers["Authorization"], "as it runs")
	assert.NotContains(t, w.secretRedactor().String("first-token-value"), "first-token-value")

	require.NoError(t, config.Vault("").Set(secrets.GlobalScope, "GH_TOKEN", "second-token-value"))
	require.NoError(t, w.ReloadConnectors(ctx))
	assert.Equal(t, "Bearer second-token-value", w.liveMCP()[0].Headers["Authorization"], "the changed secret, at the next reload")
}

// Drafts in a row read the schema once (SchemaTTL); a connection that
// changed reads it again; a kind that isn't there is ErrUnknown.
func TestDraftReadsTheSchemaOnce(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	llm := &promptLLM{reply: `{"name": "q", "statement": "SELECT 1"}`}
	reads := 0
	qb := func(c connectors.Connection) queryBuilder {
		return queryBuilder{
			cfg: w.settings(), catalog: w.ConnectorCatalog(),
			model:  func(context.Context) (model.LLM, error) { return llm, nil },
			redact: func(s string) string { return s },
			skill:  builtinSkill(SQLSkill),
			schema: func(context.Context) ([]connectors.Table, error) {
				reads++
				return []connectors.Table{{Name: "t"}}, nil
			},
			schemaKey: schemaKey(w.Dir(), c, nil),
		}
	}
	c := kitchen(t)
	for range 3 {
		d, err := draftQuery(ctx, qb(c), c, "anything")
		require.NoError(t, err)
		require.Empty(t, d.Problem)
	}
	assert.Equal(t, 1, reads, "read once for three drafts")
	c.Description = "changed"
	_, err := draftQuery(ctx, qb(c), c, "anything")
	require.NoError(t, err)
	assert.Equal(t, 2, reads, "a changed connection reads it again")

	_, err = w.DraftConnectorQuery(ctx, connectors.Connection{Name: "x", Category: "databases", Kind: "nope"}, nil, "x")
	assert.ErrorIs(t, err, connectors.ErrUnknown)
	_, _, err = kindWith(w.ConnectorCatalog(), connectors.Connection{Category: "databases", Kind: "sqlite"}, connectors.Capability("teleport"))
	assert.ErrorIs(t, err, ErrNoCapability)
	assert.NotErrorIs(t, err, ErrNoQueryBuilder)
}

// A trust decision that can't be written fails the save, saying so; a
// built-in skill that isn't there isn't found; the schema cache drops
// what's old and stays bounded.
func TestConnectorStoresAndCaches(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	settings := settingsDir(w.cfg)
	require.NoError(t, os.MkdirAll(settings, 0o755))
	require.NoError(t, os.Chmod(settings, 0o500))
	t.Cleanup(func() { _ = os.Chmod(settings, 0o755) })
	_, _, err := w.SaveConnector(ctx, kitchen(t), nil, "")
	assert.Error(t, err, "its trust can't be written")
	require.NoError(t, os.Chmod(settings, 0o755))

	_, ok := builtinSkill("no-such-skill")()
	assert.False(t, ok)
	_, ok = builtinSkill(SQLSkill)()
	assert.True(t, ok)

	old := SchemaTTL
	t.Cleanup(func() { SchemaTTL = old })
	read := func(context.Context) ([]connectors.Table, error) { return []connectors.Table{{Name: "t"}}, nil }
	for i := range maxSchemas + 3 {
		_, err := schemas.get(ctx, fmt.Sprint("k", i), read)
		require.NoError(t, err)
	}
	schemas.mu.Lock()
	assert.LessOrEqual(t, len(schemas.m), maxSchemas, "bounded")
	schemas.mu.Unlock()
	SchemaTTL = 0
	_, err = schemas.get(ctx, "fresh", read)
	require.NoError(t, err)
	schemas.mu.Lock()
	assert.Len(t, schemas.m, 1, "the old ones went")
	schemas.mu.Unlock()
}

// Once the connection is saved, a settings folder that can't be written
// fails what records a choice or forgets a trust: a rename, a delete, Make
// global and a global rename each say so.
func TestConnectorChoicesUnwritable(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	g := GlobalConnectors{Config: w.cfg}
	saved, _, err := w.SaveConnector(ctx, kitchen(t), nil, "")
	require.NoError(t, err)
	require.NoError(t, w.SetConnectorOn(ctx, "databases", connectors.ScopeWorkspace, "kitchen", false))
	pantry := kitchen(t)
	pantry.Name = "pantry"
	_, _, err = w.SaveConnector(ctx, pantry, nil, "")
	require.NoError(t, err)
	shelf := kitchen(t)
	shelf.Name = "shelf"
	globalShelf, _, err := g.Save(ctx, shelf, nil, "")
	require.NoError(t, err)
	require.NoError(t, w.SetConnectorOn(ctx, "databases", connectors.ScopeGlobal, "shelf", false))

	settings := settingsDir(w.cfg)
	require.NoError(t, os.Chmod(settings, 0o500))
	t.Cleanup(func() { _ = os.Chmod(settings, 0o755) })
	renamed := saved
	renamed.Name = "larder"
	_, _, err = w.SaveConnector(ctx, renamed, nil, "kitchen")
	assert.Error(t, err, "a rename: its trust can't be written")
	assert.Error(t, w.DeleteConnector(ctx, "databases", connectors.ScopeWorkspace, "pantry"), "a delete: its trust can't be forgotten")
	globalShelf.Name = "rack"
	_, _, err = g.Save(ctx, globalShelf, nil, "shelf")
	assert.Error(t, err, "a global rename: the choices can't be moved")
	require.NoError(t, os.Chmod(settings, 0o755))
	larder, err := w.connector(connectors.Ref{Scope: connectors.ScopeWorkspace, Category: "databases", Name: "larder"})
	require.NoError(t, err)
	require.NoError(t, os.Chmod(settings, 0o500))
	_, _, err = w.MakeConnectorGlobal(ctx, "databases", larder.Name)
	assert.Error(t, err, "Make global: the choice can't be moved")
}
