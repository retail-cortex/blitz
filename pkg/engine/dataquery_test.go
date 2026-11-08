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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/datatable"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// The CLI's databases: the connections a chat may use with their saved
// queries, and a query run read-only on one by name (spec_059_data
// DAT-45).
func TestDataConnectionsAndQueries(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()

	conns, err := w.DataConnections(ctx)
	require.NoError(t, err)
	assert.Empty(t, conns)

	c := kitchen(t)
	c.Queries = append(c.Queries, connectors.Query{Name: "longer", Description: "Longer than", Statement: "SELECT name FROM recipes WHERE minutes > ? ORDER BY name",
		Params: []connectors.Param{{Name: "m", Type: "integer", Description: "Minutes"}}})
	_, probs, err := w.SaveConnector(ctx, c, nil, "")
	require.NoError(t, err)
	require.Empty(t, probs)
	other := kitchen(t)
	other.Name, other.ReadOnly, other.Queries = "attic", false, nil
	_, probs, err = w.SaveConnector(ctx, other, nil, "")
	require.NoError(t, err)
	require.Empty(t, probs)

	conns, err = w.DataConnections(ctx)
	require.NoError(t, err)
	require.Len(t, conns, 2)
	assert.Equal(t, "attic", conns[0].Name, "by name")
	k := conns[1]
	assert.Equal(t, api.DataConnection{Name: "kitchen", Kind: "sqlite", ReadOnly: true, On: true, Trusted: true, Queries: []api.DataQueryInfo{
		{Name: "count", Description: "How many recipes there are"},
		{Name: "longer", Description: "Longer than", Params: []string{"m"}},
	}}, k)
	assert.True(t, k.Usable())

	for _, tc := range []struct {
		name string
		q    api.DataQuery
		rows [][]any
		cut  bool
		err  string
	}{
		{"SQL", api.DataQuery{Connection: "kitchen", Statement: "SELECT name, minutes FROM recipes ORDER BY name"},
			[][]any{{"bread", json.Number("90")}, {"soup", json.Number("30")}}, false, ""},
		{"SQL with parameters", api.DataQuery{Connection: "kitchen", Statement: "SELECT name FROM recipes WHERE minutes < ?", Params: []any{json.Number("60")}},
			[][]any{{"soup"}}, false, ""},
		{"a saved query", api.DataQuery{Connection: "kitchen", Query: "count"}, [][]any{{json.Number("2")}}, false, ""},
		{"a saved query's parameters", api.DataQuery{Connection: "kitchen", Query: "longer", Named: map[string]any{"m": 60}}, [][]any{{"bread"}}, false, ""},
		{"cut at the limit", api.DataQuery{Connection: "kitchen", Statement: "SELECT name FROM recipes ORDER BY name", MaxRows: 1}, [][]any{{"bread"}}, true, ""},
		{"nothing to run", api.DataQuery{Connection: "kitchen"}, nil, false, "give a statement"},
		{"no such connection", api.DataQuery{Connection: "gone", Statement: "SELECT 1"}, nil, false, `no database connection "gone"`},
		{"read-only, whatever the connection allows", api.DataQuery{Connection: "attic", Statement: "DELETE FROM recipes"}, nil, false, "read-only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := w.RunDataQuery(ctx, tc.q)
			if tc.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.rows, got.Rows)
			assert.Equal(t, len(tc.rows), got.Total)
			assert.Equal(t, tc.cut, got.Truncated)
			assert.NotEmpty(t, got.Markdown)
		})
	}

	require.NoError(t, w.SetConnectorOn(ctx, "databases", connectors.ScopeWorkspace, "attic", false))
	conns, err = w.DataConnections(ctx)
	require.NoError(t, err)
	assert.False(t, conns[0].On)
	assert.False(t, conns[0].Usable())
	_, err = w.RunDataQuery(ctx, api.DataQuery{Connection: "attic", Statement: "SELECT 1"})
	assert.ErrorContains(t, err, "is off")
}

// A table as the API carries it: its columns as they are, its values
// masked, an empty one with no rows.
func TestDataTable(t *testing.T) {
	n := int64(80)
	redact.Remember("kitchen-door-code-4417")
	got, err := DataTable(datatable.New([]datatable.Column{{Name: "key", DBType: "varchar", Length: &n}},
		[][]any{{"kitchen-door-code-4417"}}, false))
	require.NoError(t, err)
	assert.Equal(t, []api.DataColumn{{Name: "key", Type: "string", Length: &n, DBType: "varchar"}}, got.Columns)
	assert.NotContains(t, got.Rows[0][0], "door-code", "a secret's value is masked")
	assert.NotContains(t, got.Markdown, "door-code")

	got, err = DataTable(datatable.Table{})
	require.NoError(t, err)
	assert.Equal(t, [][]any{}, got.Rows)
}
