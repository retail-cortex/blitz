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

package client

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/secrets"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The CLI's databases through the service: the connections and their
// queries, and a query on one by name, its parameters sent as JSON
// (spec_059_data DAT-45).
func TestRemoteData(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	r := attach(t, nil)
	ctx := context.Background()

	conns, err := r.DataConnections(ctx)
	require.NoError(t, err)
	assert.Empty(t, conns)

	file := filepath.Join(t.TempDir(), "kitchen.db")
	db, err := sql.Open("sqlite3", file)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE recipes(name TEXT, minutes INTEGER); INSERT INTO recipes VALUES ('soup', 30), ('bread', 90)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	saved, err := r.workspaces.SaveConnector(ctx, connect.NewRequest(&pb.SaveConnectorRequest{Workspace: r.dir, Connection: &pb.Connector{
		Name: "kitchen", Category: "databases", Scope: "workspace", Kind: "sqlite", Settings: map[string]string{"database": file}, ReadOnly: true,
		Queries: []*pb.ConnectorQuery{{Name: "longer", Description: "Longer than", Statement: "SELECT name FROM recipes WHERE minutes > ?",
			Params: []*pb.ConnectorQueryParam{{Name: "m", Type: "integer", Description: "Minutes"}}}}}}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)

	conns, err = r.DataConnections(ctx)
	require.NoError(t, err)
	assert.Equal(t, []api.DataConnection{{Name: "kitchen", Kind: "sqlite", ReadOnly: true, On: true, Trusted: true,
		Queries: []api.DataQueryInfo{{Name: "longer", Description: "Longer than", Params: []string{"m"}}}}}, conns)

	for _, tc := range []struct {
		name string
		q    api.DataQuery
		rows [][]any
		err  string
	}{
		{"SQL with parameters", api.DataQuery{Connection: "kitchen", Statement: "SELECT name, minutes FROM recipes WHERE minutes < ?", Params: []any{60}},
			[][]any{{"soup", json.Number("30")}}, ""},
		{"a saved query by name", api.DataQuery{Connection: "kitchen", Query: "longer", Named: map[string]any{"m": 60}}, [][]any{{"bread"}}, ""},
		{"the service's problem", api.DataQuery{Connection: "gone", Statement: "SELECT 1"}, nil, `no database connection "gone"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.RunDataQuery(ctx, tc.q)
			if tc.err != "" {
				assert.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.rows, got.Rows)
			assert.Equal(t, len(tc.rows), got.Total)
			assert.NotEmpty(t, got.Columns)
		})
	}
}

// Without the service, the calls say so.
func TestRemoteDataWithoutTheService(t *testing.T) {
	r, _ := gone(t)
	ctx := context.Background()
	_, err := r.DataConnections(ctx)
	assert.Error(t, err)
	_, err = r.RunDataQuery(ctx, api.DataQuery{Connection: "kitchen", Statement: "SELECT 1"})
	assert.Error(t, err)
	_, err = r.RunDataQuery(ctx, api.DataQuery{Connection: "kitchen", Statement: "SELECT ?", Params: []any{math.NaN()}})
	assert.ErrorContains(t, err, "NaN", "parameters JSON can't carry, before anything is sent")
}

func TestRemotePicturesWithoutTheService(t *testing.T) {
	r, _ := gone(t)
	ctx := context.Background()
	_, err := r.PictureModels(ctx)
	assert.Error(t, err)
	_, err = r.MakePicture(ctx, &pb.MakePictureRequest{Prompt: "a lighthouse"})
	assert.Error(t, err)
	_, err = r.SavePicture(ctx, "s1", "p1", "")
	assert.Error(t, err)
	_, err = r.PictureSessions(ctx)
	assert.Error(t, err)
	_, err = r.PictureSession(ctx, "s1")
	assert.Error(t, err)
}
