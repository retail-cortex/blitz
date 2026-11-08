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
	"encoding/json"
	"errors"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// DataConnections are the workspace's database connections and their
// saved queries (WorkspaceService.ListDataConnections, spec_059_data
// DAT-45).
func (r *Remote) DataConnections(ctx context.Context) ([]api.DataConnection, error) {
	res, err := r.workspaces.ListDataConnections(ctx, connect.NewRequest(&pb.ListDataConnectionsRequest{Workspace: r.dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]api.DataConnection, len(res.Msg.Connections))
	for i, c := range res.Msg.Connections {
		dc := api.DataConnection{Name: c.Name, Kind: c.Kind, ReadOnly: c.ReadOnly, On: c.On, Trusted: c.Trusted, Problem: c.Problem}
		for _, q := range c.Queries {
			dc.Queries = append(dc.Queries, api.DataQueryInfo{Name: q.Name, Description: q.Description, Params: q.Params})
		}
		out[i] = dc
	}
	return out, nil
}

// RunDataQuery runs q read-only on the workspace's connection it names
// (WorkspaceService.RunConnectorQuery with connection_name, DAT-45); the
// service's problem is the error.
func (r *Remote) RunDataQuery(ctx context.Context, q api.DataQuery) (api.DataTable, error) {
	var params any
	switch {
	case q.Query != "" && len(q.Named) > 0:
		params = q.Named
	case q.Query == "" && len(q.Params) > 0:
		params = q.Params
	}
	paramsJSON := ""
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return api.DataTable{}, err
		}
		paramsJSON = string(b)
	}
	res, err := r.workspaces.RunConnectorQuery(ctx, connect.NewRequest(&pb.RunConnectorQueryRequest{Workspace: r.dir, ConnectionName: q.Connection,
		Query: q.Query, Statement: q.Statement, ParamsJson: paramsJSON, MaxRows: int32(q.MaxRows), ReadOnly: true}))
	if err != nil {
		return api.DataTable{}, fromAPI(err)
	}
	if res.Msg.Problem != "" {
		return api.DataTable{}, errors.New(res.Msg.Problem)
	}
	t := res.Msg.Table
	if t == nil {
		return api.DataTable{}, errors.New("the service answered without a table")
	}
	cols := make([]api.DataColumn, len(t.Columns))
	for i, c := range t.Columns {
		cols[i] = api.DataColumn{Name: c.Name, Description: c.Description, Type: c.Type, Length: c.Length, DBType: c.DbType, Nullable: c.Nullable}
	}
	return api.DataTableFrom(cols, t.RowsJson, int(t.Total), t.Truncated, t.Markdown)
}
