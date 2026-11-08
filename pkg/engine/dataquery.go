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
	"errors"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/datatable"
	"github.com/retail-cortex/blitz/pkg/redact"
)

// DataConnections are the workspace's database connections and their
// saved queries, by name, one of each name (the workspace's over a global
// one), as a chat finds them (spec_059_data DAT-45).
func (w *Workspace) DataConnections(context.Context) ([]api.DataConnection, error) {
	conns := w.Connectors()
	var out []api.DataConnection
	seen := map[string]bool{}
	for _, c := range conns {
		if c.Category != dataCategory || seen[c.Name] {
			continue
		}
		c, _ = dataConnection(conns, c.Name)
		seen[c.Name] = true
		dc := api.DataConnection{Name: c.Name, Kind: c.Kind, ReadOnly: c.ReadOnly, On: c.On, Trusted: c.Trusted, Problem: c.Problem}
		for _, q := range c.Queries {
			qi := api.DataQueryInfo{Name: q.Name, Description: q.Description}
			for _, p := range q.Params {
				qi.Params = append(qi.Params, p.Name)
			}
			dc.Queries = append(dc.Queries, qi)
		}
		out = append(out, dc)
	}
	slices.SortFunc(out, func(a, b api.DataConnection) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// errNoStatement is a data query with neither a saved query nor SQL.
var errNoStatement = errors.New("give a statement (SQL) or a saved query's name")

// RunDataQuery runs q read-only on the workspace's connection it names,
// found as a chat finds it: on, trusted and readable (DAT-44, DAT-45).
// Error messages are masked.
func (w *Workspace) RunDataQuery(ctx context.Context, q api.DataQuery) (api.DataTable, error) {
	if q.Query == "" && q.Statement == "" {
		return api.DataTable{}, errNoStatement
	}
	c, err := chatConnection(w.Connectors(), q.Connection)
	if err != nil {
		return api.DataTable{}, err
	}
	t, err := w.RunConnectorQuery(ctx, c, nil, connectors.QueryRun{Query: q.Query, Named: q.Named, Statement: q.Statement, Params: q.Params,
		MaxRows: q.MaxRows, ReadOnly: true})
	if err != nil {
		return api.DataTable{}, errors.New(w.secretRedactor().String(err.Error()))
	}
	return DataTable(t)
}

// DataTable is t as the API carries it, its values and Markdown masked
// as the service masks them (DAT-25).
func DataTable(t datatable.Table) (api.DataTable, error) {
	rows, err := json.Marshal(t.Rows)
	if err != nil {
		return api.DataTable{}, err
	}
	cols := make([]api.DataColumn, len(t.Columns))
	for i, c := range t.Columns {
		cols[i] = api.DataColumn(c)
	}
	return api.DataTableFrom(cols, redact.Mask(string(rows)), t.Total, t.Truncated, redact.Mask(t.Markdown(0)))
}
