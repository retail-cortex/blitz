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

package server

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/secrets"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Studio's connectors over the API: the catalog (categories and kinds),
// a SQLite connection saved, listed, tested, tried and its schema read; a
// Postgres one with its password kept as a secret; renamed, deleted; the
// query builder's refusals (spec_connectors_047).
func TestConnectorsOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	w := c.workspaces
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	list, err := w.ListConnectors(ctx, connect.NewRequest(&pb.ListConnectorsRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Empty(t, list.Msg.Connections)
	assert.Equal(t, filepath.Join(dir, ".agents", "connectors"), list.Msg.WorkspaceDir)
	require.Len(t, list.Msg.Categories, 1)
	db := list.Msg.Categories[0]
	assert.Equal(t, "databases", db.Id)
	assert.Equal(t, "Bases de datos", db.Name["es"])
	assert.Equal(t, "db", db.Prefix)
	assert.Equal(t, filepath.Join(dir, ".agents", "connectors", "databases"), db.WorkspaceDir)
	assert.Empty(t, db.State)
	var kinds []string
	for _, k := range list.Msg.Kinds {
		kinds = append(kinds, k.Id)
		assert.Equal(t, []string{"test", "try", "queries", "schema", "query-builder"}, k.Capabilities, k.Id)
		assert.True(t, k.CanReadOnly)
		assert.NotEmpty(t, k.ReadOnlyHint["fr-CA"])
	}
	assert.Equal(t, []string{"alloydb-postgres", "bigquery", "postgres", "sqlite"}, kinds)
	pg := list.Msg.Kinds[2]
	assert.Equal(t, "password", pg.Fields[4].Key)
	assert.Equal(t, "secret", pg.Fields[4].Type)
	assert.Equal(t, "$1", pg.Placeholders)
	assert.NotEmpty(t, pg.Tools[0].About["en"])

	file := filepath.Join(t.TempDir(), "kitchen.db")
	conn, err := sql.Open("sqlite3", file)
	require.NoError(t, err)
	_, err = conn.Exec(`CREATE TABLE recipes(name TEXT); INSERT INTO recipes VALUES ('soup'), ('bread')`)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	kitchen := &pb.Connector{Name: "kitchen", Category: "databases", Scope: "workspace", Kind: "sqlite", Settings: map[string]string{"database": file}, ReadOnly: true, Tools: []string{"list_tables"},
		Queries: []*pb.ConnectorQuery{{Name: "count", Description: "How many recipes", Statement: "SELECT count(*) AS n FROM recipes"}}}
	tested, err := w.TestConnector(ctx, connect.NewRequest(&pb.TestConnectorRequest{Workspace: dir, Connection: kitchen}))
	require.NoError(t, err)
	require.Empty(t, tested.Msg.Problem)
	require.Len(t, tested.Msg.Tools, 2)
	assert.True(t, tested.Msg.Tools[0].ReadOnly)

	tried, err := w.TryConnectorTool(ctx, connect.NewRequest(&pb.TryConnectorToolRequest{Workspace: dir, Connection: kitchen, Tool: "count", ArgsJson: "{}"}))
	require.NoError(t, err)
	assert.Empty(t, tried.Msg.Problem)
	assert.False(t, tried.Msg.IsError)
	assert.JSONEq(t, `{"n":2}`, tried.Msg.Output)
	tried, err = w.TryConnectorTool(ctx, connect.NewRequest(&pb.TryConnectorToolRequest{Workspace: dir, Connection: kitchen, Tool: "count", ArgsJson: "["}))
	require.NoError(t, err)
	assert.Contains(t, tried.Msg.Problem, "JSON")

	schema, err := w.ReadConnectorSchema(ctx, connect.NewRequest(&pb.ReadConnectorSchemaRequest{Workspace: dir, Connection: kitchen}))
	require.NoError(t, err)
	require.Empty(t, schema.Msg.Problem)
	require.Len(t, schema.Msg.Tables, 1)
	assert.Equal(t, "recipes", schema.Msg.Tables[0].Name)
	assert.Equal(t, "name", schema.Msg.Tables[0].Columns[0].Name)

	saved, err := w.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: dir, Connection: kitchen}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	assert.Equal(t, filepath.Join(dir, ".agents", "connectors", "databases", "kitchen.yaml"), saved.Msg.Connection.Path)
	assert.True(t, saved.Msg.Connection.Queries[0].ReadsOnly)
	assert.True(t, saved.Msg.Connection.Trusted)

	shop := &pb.Connector{Name: "shop", Category: "databases", Scope: "workspace", Kind: "postgres", Settings: map[string]string{"host": "127.0.0.1", "port": "1", "database": "shop", "user": "reader"}, ReadOnly: true, Tools: []string{"execute_sql"}}
	bad, err := w.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: dir, Connection: shop}))
	require.NoError(t, err)
	assert.Contains(t, bad.Msg.Problems, "password is required")
	saved, err = w.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: dir, Connection: shop, SecretValues: map[string]string{"password": "pg-secret-pw"}}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	assert.Equal(t, []string{"password"}, saved.Msg.Connection.SecretsSet)
	assert.Equal(t, "${DB_SHOP_PASSWORD}", saved.Msg.Connection.Settings["password"])
	text, err := os.ReadFile(saved.Msg.Connection.Path)
	require.NoError(t, err)
	assert.NotContains(t, string(text), "pg-secret-pw")

	tested, err = w.TestConnector(ctx, connect.NewRequest(&pb.TestConnectorRequest{Workspace: dir, Connection: shop}))
	require.NoError(t, err)
	assert.NotEmpty(t, tested.Msg.Problem, "nothing listens on port 1")
	assert.NotContains(t, tested.Msg.Problem, "pg-secret-pw")
	schema, err = w.ReadConnectorSchema(ctx, connect.NewRequest(&pb.ReadConnectorSchemaRequest{Workspace: dir, Connection: shop}))
	require.NoError(t, err)
	assert.Contains(t, schema.Msg.Problem, "no tables")
	drafted, err := w.DraftConnectorQuery(ctx, connect.NewRequest(&pb.DraftConnectorQueryRequest{Workspace: dir, Connection: shop, Request: "the top customers"}))
	require.NoError(t, err)
	assert.Contains(t, drafted.Msg.Problem, "no tables", "no schema read: no draft")
	assert.Nil(t, drafted.Msg.Query)
	_, err = w.DraftConnectorQuery(ctx, connect.NewRequest(&pb.DraftConnectorQueryRequest{Workspace: dir, Connection: shop}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "say what you're looking for")
	_, err = w.DraftConnectorQuery(ctx, connect.NewRequest(&pb.DraftConnectorQueryRequest{Workspace: dir}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	list, err = w.ListConnectors(ctx, connect.NewRequest(&pb.ListConnectorsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Connections, 2)
	assert.Equal(t, "idle", list.Msg.Categories[0].State)

	shop.Name = "store"
	moved, err := w.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: dir, Connection: shop, PreviousName: "shop"}))
	require.NoError(t, err)
	require.Empty(t, moved.Msg.Problems, "the password moves with it")
	assert.Equal(t, []string{"password"}, moved.Msg.Connection.SecretsSet)
	_, err = w.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: dir, Connection: shop, PreviousName: "gone"}))
	assert.Equal(t, "CONNECTOR_NOT_FOUND", reason(t, err))

	assert.True(t, moved.Msg.Connection.Enabled, "on until turned off")
	_, err = w.SetConnectorEnabled(ctx, connect.NewRequest(&pb.SetConnectorEnabledRequest{Workspace: dir, Category: "databases", Scope: "workspace", Name: "store"}))
	require.NoError(t, err)
	list, err = w.ListConnectors(ctx, connect.NewRequest(&pb.ListConnectorsRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.True(t, slices.ContainsFunc(list.Msg.Connections, func(c *pb.Connector) bool { return c.Name == "store" && !c.Enabled }), "off in this workspace")
	_, err = w.SetConnectorEnabled(ctx, connect.NewRequest(&pb.SetConnectorEnabledRequest{Workspace: dir, Category: "databases", Scope: "global", Name: "store", Enabled: true}))
	assert.Equal(t, "CONNECTOR_NOT_FOUND", reason(t, err))

	_, err = w.DeleteConnector(ctx, connect.NewRequest(&pb.DeleteConnectorRequest{Workspace: dir, Category: "databases", Scope: "workspace", Name: "store"}))
	require.NoError(t, err)
	_, err = w.DeleteConnector(ctx, connect.NewRequest(&pb.DeleteConnectorRequest{Workspace: dir, Category: "databases", Scope: "workspace", Name: "store"}))
	assert.Equal(t, "CONNECTOR_NOT_FOUND", reason(t, err))

	_, err = w.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: dir}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	custom, err := w.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: dir, Connection: &pb.Connector{Name: "raw", Category: "databases", Custom: true, Text: "kind: ["}}))
	require.NoError(t, err)
	assert.True(t, strings.Contains(strings.Join(custom.Msg.Problems, " "), "YAML"))
}
