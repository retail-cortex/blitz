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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/secrets"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Settings' global connections over ConfigService: saved, listed in
// every workspace's Studio (which can't change them), tested, tried, read,
// renamed and deleted; Studio's Make Global moves one of the workspace's
// there (spec_connectors_047 CON-18, CON-19).
func TestGlobalConnectorsOverTheAPI(t *testing.T) {
	c, s := serve(t, nil)
	secrets.SetDefault(&secrets.Memory{})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	w := c.workspaces
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	file := filepath.Join(t.TempDir(), "kitchen.db")
	conn, err := sql.Open("sqlite3", file)
	require.NoError(t, err)
	_, err = conn.Exec(`CREATE TABLE recipes(name TEXT); INSERT INTO recipes VALUES ('soup')`)
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	kitchen := &pb.Connector{Name: "kitchen", Category: "databases", Kind: "sqlite", Settings: map[string]string{"database": file}, ReadOnly: true, Tools: []string{"list_tables"},
		Queries: []*pb.ConnectorQuery{{Name: "count", Description: "How many recipes", Statement: "SELECT count(*) AS n FROM recipes"}}}

	list, err := cfg.ListGlobalConnectors(ctx, connect.NewRequest(&pb.ListGlobalConnectorsRequest{}))
	require.NoError(t, err)
	assert.Empty(t, list.Msg.Connections)
	require.Len(t, list.Msg.Categories, 1)
	assert.Empty(t, list.Msg.Categories[0].WorkspaceDir, "no workspace")
	assert.Len(t, list.Msg.Kinds, 4)

	_, err = cfg.SaveGlobalConnector(ctx, connect.NewRequest(&pb.SaveGlobalConnectorRequest{}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	saved, err := cfg.SaveGlobalConnector(ctx, connect.NewRequest(&pb.SaveGlobalConnectorRequest{Connection: kitchen}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	assert.Equal(t, "global", saved.Msg.Connection.Scope)
	bad, err := cfg.SaveGlobalConnector(ctx, connect.NewRequest(&pb.SaveGlobalConnectorRequest{Connection: &pb.Connector{Name: "Bad", Category: "databases", Kind: "sqlite"}}))
	require.NoError(t, err)
	assert.NotEmpty(t, bad.Msg.Problems)

	studio, err := w.ListConnectors(ctx, connect.NewRequest(&pb.ListConnectorsRequest{Workspace: dir}))
	require.NoError(t, err)
	require.Len(t, studio.Msg.Connections, 1, "every workspace lists it")
	g := studio.Msg.Connections[0]
	assert.Equal(t, "global", g.Scope)
	assert.True(t, g.Enabled)
	_, err = w.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: dir, Connection: g, PreviousName: "kitchen"}))
	assert.Equal(t, "CONNECTOR_GLOBAL", reason(t, err), "Studio can't change it")
	_, err = w.DeleteConnector(ctx, connect.NewRequest(&pb.DeleteConnectorRequest{Workspace: dir, Category: "databases", Scope: "global", Name: "kitchen"}))
	assert.Equal(t, "CONNECTOR_GLOBAL", reason(t, err))

	tested, err := cfg.TestGlobalConnector(ctx, connect.NewRequest(&pb.TestGlobalConnectorRequest{Connection: kitchen}))
	require.NoError(t, err)
	assert.Empty(t, tested.Msg.Problem)
	assert.Len(t, tested.Msg.Tools, 2)
	tested, err = cfg.TestGlobalConnector(ctx, connect.NewRequest(&pb.TestGlobalConnectorRequest{Connection: &pb.Connector{Name: "x", Category: "email"}}))
	require.NoError(t, err)
	assert.NotEmpty(t, tested.Msg.Problem)
	tried, err := cfg.TryGlobalConnectorTool(ctx, connect.NewRequest(&pb.TryGlobalConnectorToolRequest{Connection: kitchen, Tool: "count"}))
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":1}`, tried.Msg.Output)
	tried, err = cfg.TryGlobalConnectorTool(ctx, connect.NewRequest(&pb.TryGlobalConnectorToolRequest{Connection: kitchen, Tool: "count", ArgsJson: "["}))
	require.NoError(t, err)
	assert.Contains(t, tried.Msg.Problem, "JSON")
	tried, err = cfg.TryGlobalConnectorTool(ctx, connect.NewRequest(&pb.TryGlobalConnectorToolRequest{Connection: kitchen, Tool: "nope"}))
	require.NoError(t, err)
	assert.NotEmpty(t, tried.Msg.Problem)
	schema, err := cfg.ReadGlobalConnectorSchema(ctx, connect.NewRequest(&pb.ReadGlobalConnectorSchemaRequest{Connection: kitchen}))
	require.NoError(t, err)
	require.Len(t, schema.Msg.Tables, 1)
	empty := &pb.Connector{Name: "empty", Category: "databases", Kind: "sqlite", Settings: map[string]string{"database": filepath.Join(t.TempDir(), "none.db")}}
	schema, err = cfg.ReadGlobalConnectorSchema(ctx, connect.NewRequest(&pb.ReadGlobalConnectorSchemaRequest{Connection: empty}))
	require.NoError(t, err)
	assert.Contains(t, schema.Msg.Problem, "no tables")
	drafted, err := cfg.DraftGlobalConnectorQuery(ctx, connect.NewRequest(&pb.DraftGlobalConnectorQueryRequest{Connection: empty, Request: "anything"}))
	require.NoError(t, err)
	assert.Contains(t, drafted.Msg.Problem, "no tables", "no model is asked")
	_, err = cfg.DraftGlobalConnectorQuery(ctx, connect.NewRequest(&pb.DraftGlobalConnectorQueryRequest{Connection: kitchen}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "say what you're looking for")
	_, err = cfg.DraftGlobalConnectorQuery(ctx, connect.NewRequest(&pb.DraftGlobalConnectorQueryRequest{}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	moved := &pb.Connector{Name: "pantry", Category: "databases", Kind: "sqlite", Settings: map[string]string{"database": file}}
	_, err = cfg.SaveGlobalConnector(ctx, connect.NewRequest(&pb.SaveGlobalConnectorRequest{Connection: moved, PreviousName: "gone"}))
	assert.Equal(t, "CONNECTOR_NOT_FOUND", reason(t, err))
	_, err = cfg.SaveGlobalConnector(ctx, connect.NewRequest(&pb.SaveGlobalConnectorRequest{Connection: moved, PreviousName: "kitchen"}))
	require.NoError(t, err)
	_, err = cfg.DeleteGlobalConnector(ctx, connect.NewRequest(&pb.DeleteGlobalConnectorRequest{Category: "databases", Name: "pantry"}))
	require.NoError(t, err)
	_, err = cfg.DeleteGlobalConnector(ctx, connect.NewRequest(&pb.DeleteGlobalConnectorRequest{Category: "databases", Name: "pantry"}))
	assert.Equal(t, "CONNECTOR_NOT_FOUND", reason(t, err))

	// Make Global, from Studio.
	ws, err := w.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: dir, Connection: kitchen}))
	require.NoError(t, err)
	assert.Equal(t, "workspace", ws.Msg.Connection.Scope, "Studio's are the workspace's")
	made, err := w.MakeConnectorGlobal(ctx, connect.NewRequest(&pb.MakeConnectorGlobalRequest{Workspace: dir, Category: "databases", Name: "kitchen"}))
	require.NoError(t, err)
	require.Empty(t, made.Msg.Problems)
	assert.Equal(t, "global", made.Msg.Connection.Scope)
	list, err = cfg.ListGlobalConnectors(ctx, connect.NewRequest(&pb.ListGlobalConnectorsRequest{}))
	require.NoError(t, err)
	assert.Len(t, list.Msg.Connections, 1)
	_, err = w.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: dir, Connection: kitchen}))
	require.NoError(t, err)
	made, err = w.MakeConnectorGlobal(ctx, connect.NewRequest(&pb.MakeConnectorGlobalRequest{Workspace: dir, Category: "databases", Name: "kitchen"}))
	require.NoError(t, err)
	assert.NotEmpty(t, made.Msg.Problems, "a global one has its name")
	_, err = w.MakeConnectorGlobal(ctx, connect.NewRequest(&pb.MakeConnectorGlobalRequest{Workspace: dir, Category: "databases", Name: "gone"}))
	assert.Equal(t, "CONNECTOR_NOT_FOUND", reason(t, err))

	// A query's parameters go both ways; Make global refuses a file the
	// person hasn't trusted; Try it says why it can't run.
	withParams := &pb.Connector{Name: "menu", Category: "databases", Kind: "sqlite", Settings: map[string]string{"database": file}, ReadOnly: true,
		Queries: []*pb.ConnectorQuery{{Name: "quick", Description: "Quick ones", Statement: "SELECT name FROM recipes WHERE length(name) < $1", Params: []*pb.ConnectorQueryParam{{Name: "n", Type: "integer", Description: "Letters"}}}}}
	menu, err := w.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: dir, Connection: withParams}))
	require.NoError(t, err)
	require.Empty(t, menu.Msg.Problems)
	require.Len(t, menu.Msg.Connection.Queries, 1)
	assert.Equal(t, "n", menu.Msg.Connection.Queries[0].Params[0].Name)
	assert.True(t, menu.Msg.Connection.Queries[0].ReadsOnly)
	text, err := os.ReadFile(menu.Msg.Connection.Path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(menu.Msg.Connection.Path, append(text, '\n'), 0o600))
	_, err = w.MakeConnectorGlobal(ctx, connect.NewRequest(&pb.MakeConnectorGlobalRequest{Workspace: dir, Category: "databases", Name: "menu"}))
	assert.Equal(t, "CONNECTOR_NOT_TRUSTED", reason(t, err))
	wsTried, err := w.TryConnectorTool(ctx, connect.NewRequest(&pb.TryConnectorToolRequest{Workspace: dir, Connection: &pb.Connector{Name: "x", Category: "email"}, Tool: "count"}))
	require.NoError(t, err)
	assert.NotEmpty(t, wsTried.Msg.Problem)

	// Settings that can't be read: each global call says so.
	home, _ := os.UserHomeDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[[[ not toml"), 0o600))
	for name, call := range map[string]func() error{
		"list": func() error {
			_, err := cfg.ListGlobalConnectors(ctx, connect.NewRequest(&pb.ListGlobalConnectorsRequest{}))
			return err
		},
		"save": func() error {
			_, err := cfg.SaveGlobalConnector(ctx, connect.NewRequest(&pb.SaveGlobalConnectorRequest{Connection: kitchen}))
			return err
		},
		"delete": func() error {
			_, err := cfg.DeleteGlobalConnector(ctx, connect.NewRequest(&pb.DeleteGlobalConnectorRequest{Category: "databases", Name: "kitchen"}))
			return err
		},
		"test": func() error {
			_, err := cfg.TestGlobalConnector(ctx, connect.NewRequest(&pb.TestGlobalConnectorRequest{Connection: kitchen}))
			return err
		},
		"try": func() error {
			_, err := cfg.TryGlobalConnectorTool(ctx, connect.NewRequest(&pb.TryGlobalConnectorToolRequest{Connection: kitchen, Tool: "count"}))
			return err
		},
		"schema": func() error {
			_, err := cfg.ReadGlobalConnectorSchema(ctx, connect.NewRequest(&pb.ReadGlobalConnectorSchemaRequest{Connection: kitchen}))
			return err
		},
		"draft": func() error {
			_, err := cfg.DraftGlobalConnectorQuery(ctx, connect.NewRequest(&pb.DraftGlobalConnectorQueryRequest{Connection: kitchen, Request: "x"}))
			return err
		},
	} {
		assert.Error(t, call(), name)
	}
}
