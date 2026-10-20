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
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/secrets"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Studio's databases over the API: the kinds offered, a SQLite one saved,
// listed, tested and tried; a Postgres one with its password kept as a
// secret; renamed, deleted (spec_databases_046).
func TestDatabasesOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	w := c.workspaces
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	list, err := w.ListDatabases(ctx, connect.NewRequest(&pb.ListDatabasesRequest{Workspace: dir}))
	require.NoError(t, err)
	assert.Empty(t, list.Msg.Connections)
	assert.Empty(t, list.Msg.State)
	assert.Equal(t, filepath.Join(dir, ".agents", "databases"), list.Msg.WorkspaceDir)
	var kinds []string
	for _, k := range list.Msg.Kinds {
		kinds = append(kinds, k.Type)
	}
	assert.Equal(t, []string{"postgres", "alloydb-postgres", "sqlite", "bigquery"}, kinds)
	assert.Equal(t, "password", list.Msg.Kinds[0].Fields[4].Key)
	assert.True(t, list.Msg.Kinds[0].Fields[4].Secret)

	db := filepath.Join(t.TempDir(), "kitchen.db")
	conn, err := sql.Open("sqlite3", db)
	require.NoError(t, err)
	_, err = conn.Exec(`CREATE TABLE recipes(name TEXT); INSERT INTO recipes VALUES ('soup'), ('bread')`)
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	kitchen := &pb.DatabaseConnection{Name: "kitchen", Scope: "workspace", Type: "sqlite", Settings: map[string]string{"database": db}, ReadOnly: true, Tools: []string{"list_tables"},
		Queries: []*pb.DatabaseQuery{{Name: "count", Description: "How many recipes", Statement: "SELECT count(*) AS n FROM recipes"}}}
	tested, err := w.TestDatabase(ctx, connect.NewRequest(&pb.TestDatabaseRequest{Workspace: dir, Connection: kitchen}))
	require.NoError(t, err)
	require.Empty(t, tested.Msg.Problem)
	require.Len(t, tested.Msg.Tools, 2)
	assert.True(t, tested.Msg.Tools[0].ReadOnly)

	tried, err := w.TryDatabaseTool(ctx, connect.NewRequest(&pb.TryDatabaseToolRequest{Workspace: dir, Connection: kitchen, Tool: "count", ArgsJson: "{}"}))
	require.NoError(t, err)
	assert.Empty(t, tried.Msg.Problem)
	assert.False(t, tried.Msg.IsError)
	assert.JSONEq(t, `{"n":2}`, tried.Msg.Output)
	tried, err = w.TryDatabaseTool(ctx, connect.NewRequest(&pb.TryDatabaseToolRequest{Workspace: dir, Connection: kitchen, Tool: "count", ArgsJson: "["}))
	require.NoError(t, err)
	assert.Contains(t, tried.Msg.Problem, "JSON")

	saved, err := w.SaveDatabase(ctx, connect.NewRequest(&pb.SaveDatabaseRequest{Workspace: dir, Connection: kitchen}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	assert.Equal(t, filepath.Join(dir, ".agents", "databases", "kitchen.yaml"), saved.Msg.Connection.Path)
	assert.True(t, saved.Msg.Connection.Queries[0].ReadsOnly)

	shop := &pb.DatabaseConnection{Name: "shop", Scope: "workspace", Type: "postgres", Settings: map[string]string{"host": "127.0.0.1", "port": "1", "database": "shop", "user": "reader"}, ReadOnly: true, Tools: []string{"execute_sql"}}
	bad, err := w.SaveDatabase(ctx, connect.NewRequest(&pb.SaveDatabaseRequest{Workspace: dir, Connection: shop}))
	require.NoError(t, err)
	assert.Contains(t, bad.Msg.Problems, "password is required")
	saved, err = w.SaveDatabase(ctx, connect.NewRequest(&pb.SaveDatabaseRequest{Workspace: dir, Connection: shop, SecretValues: map[string]string{"password": "pg-secret-pw"}}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	assert.Equal(t, []string{"password"}, saved.Msg.Connection.SecretsSet)
	assert.Equal(t, "${DB_SHOP_PASSWORD}", saved.Msg.Connection.Settings["password"])
	text, err := os.ReadFile(saved.Msg.Connection.Path)
	require.NoError(t, err)
	assert.NotContains(t, string(text), "pg-secret-pw")

	tested, err = w.TestDatabase(ctx, connect.NewRequest(&pb.TestDatabaseRequest{Workspace: dir, Connection: shop}))
	require.NoError(t, err)
	assert.NotEmpty(t, tested.Msg.Problem, "nothing listens on port 1")
	assert.NotContains(t, tested.Msg.Problem, "pg-secret-pw")

	list, err = w.ListDatabases(ctx, connect.NewRequest(&pb.ListDatabasesRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Connections, 2)
	assert.Equal(t, "idle", list.Msg.State)

	shop.Name = "store"
	moved, err := w.SaveDatabase(ctx, connect.NewRequest(&pb.SaveDatabaseRequest{Workspace: dir, Connection: shop, PreviousName: "shop"}))
	require.NoError(t, err)
	require.Empty(t, moved.Msg.Problems, "the password moves with it")
	assert.Equal(t, []string{"password"}, moved.Msg.Connection.SecretsSet)
	_, err = w.SaveDatabase(ctx, connect.NewRequest(&pb.SaveDatabaseRequest{Workspace: dir, Connection: shop, PreviousName: "gone"}))
	assert.Equal(t, "DATABASE_NOT_FOUND", reason(t, err))

	_, err = w.DeleteDatabase(ctx, connect.NewRequest(&pb.DeleteDatabaseRequest{Workspace: dir, Scope: "workspace", Name: "store"}))
	require.NoError(t, err)
	_, err = w.DeleteDatabase(ctx, connect.NewRequest(&pb.DeleteDatabaseRequest{Workspace: dir, Scope: "workspace", Name: "store"}))
	assert.Equal(t, "DATABASE_NOT_FOUND", reason(t, err))

	_, err = w.SaveDatabase(ctx, connect.NewRequest(&pb.SaveDatabaseRequest{Workspace: dir}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	custom, err := w.SaveDatabase(ctx, connect.NewRequest(&pb.SaveDatabaseRequest{Workspace: dir, Connection: &pb.DatabaseConnection{Name: "raw", Custom: true, Text: "kind: ["}}))
	require.NoError(t, err)
	assert.True(t, strings.Contains(strings.Join(custom.Msg.Problems, " "), "YAML"))
}
