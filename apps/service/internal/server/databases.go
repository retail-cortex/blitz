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
	"encoding/json"
	"errors"

	"connectrpc.com/connect"

	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/databases"
	"github.com/retail-cortex/blitz/pkg/redact"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/embed"
)

// Studio's databases (spec_databases_046).

func databaseMsg(w *engine.Workspace, c databases.Connection) *pb.DatabaseConnection {
	m := &pb.DatabaseConnection{
		Name: c.Name, Scope: c.Scope, Path: c.Path, Type: c.Type, Description: c.Description, Settings: c.Settings,
		ReadOnly: c.ReadOnly, Tools: c.Tools, Custom: c.Custom, Text: c.Text, Problem: c.Problem,
	}
	for _, q := range c.Queries {
		qm := &pb.DatabaseQuery{Name: q.Name, Description: q.Description, Statement: q.Statement, ReadsOnly: embed.ReadsOnly(q.Statement)}
		for _, p := range q.Params {
			qm.Params = append(qm.Params, &pb.DatabaseQueryParam{Name: p.Name, Type: p.Type, Description: p.Description})
		}
		m.Queries = append(m.Queries, qm)
	}
	if kind, ok := databases.KindOf(c.Type); ok && !c.Custom {
		for _, f := range kind.Fields {
			if f.Secret && w.HasDatabaseSecret(c, f.Key) {
				m.SecretsSet = append(m.SecretsSet, f.Key)
			}
		}
	}
	return m
}

func databaseOf(m *pb.DatabaseConnection) databases.Connection {
	c := databases.Connection{
		Name: m.GetName(), Scope: m.GetScope(), Type: m.GetType(), Description: m.GetDescription(), Settings: m.GetSettings(),
		ReadOnly: m.GetReadOnly(), Tools: m.GetTools(), Custom: m.GetCustom(), Text: m.GetText(),
	}
	for _, q := range m.GetQueries() {
		dq := databases.Query{Name: q.GetName(), Description: q.GetDescription(), Statement: q.GetStatement()}
		for _, p := range q.GetParams() {
			dq.Params = append(dq.Params, databases.Param{Name: p.GetName(), Type: p.GetType(), Description: p.GetDescription()})
		}
		c.Queries = append(c.Queries, dq)
	}
	return c
}

func (h workspaceService) ListDatabases(ctx context.Context, r req[pb.ListDatabasesRequest]) (*connect.Response[pb.ListDatabasesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out := &pb.ListDatabasesResponse{WorkspaceDir: databases.WorkspaceDir(w.Dir()), GlobalDir: databases.GlobalDir(w.Config().Dir)}
	for _, c := range w.Databases() {
		out.Connections = append(out.Connections, databaseMsg(w.Workspace, c))
	}
	for _, k := range databases.Kinds {
		km := &pb.DatabaseKind{Type: k.Type}
		for _, f := range k.Fields {
			km.Fields = append(km.Fields, &pb.DatabaseField{Key: f.Key, Secret: f.Secret, Required: f.Required, DefaultValue: f.Default})
		}
		for _, t := range k.Tools {
			km.Tools = append(km.Tools, &pb.DatabaseReadyTool{Id: t.ID, Writes: t.Writes, On: t.On})
		}
		out.Kinds = append(out.Kinds, km)
	}
	st := w.DatabaseState()
	out.State, out.Error, out.ToolCount = st.State, st.Error, int32(st.Tools)
	return ok(out)
}

func (h workspaceService) SaveDatabase(ctx context.Context, r req[pb.SaveDatabaseRequest]) (*connect.Response[pb.SaveDatabaseResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if r.Msg.Connection == nil {
		return nil, invalid(errors.New("connection is required"))
	}
	saved, problems, err := w.SaveDatabase(ctx, databaseOf(r.Msg.Connection), r.Msg.SecretValues, r.Msg.PreviousName)
	if errors.Is(err, databases.ErrUnknown) {
		return nil, apiError(connect.CodeNotFound, "DATABASE_NOT_FOUND", err)
	} else if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return ok(&pb.SaveDatabaseResponse{Problems: problems})
	}
	return ok(&pb.SaveDatabaseResponse{Connection: databaseMsg(w.Workspace, saved)})
}

func (h workspaceService) DeleteDatabase(ctx context.Context, r req[pb.DeleteDatabaseRequest]) (*connect.Response[pb.DeleteDatabaseResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.DeleteDatabase(ctx, r.Msg.Scope, r.Msg.Name); errors.Is(err, databases.ErrUnknown) {
		return nil, apiError(connect.CodeNotFound, "DATABASE_NOT_FOUND", err)
	} else if err != nil {
		return nil, err
	}
	return ok(&pb.DeleteDatabaseResponse{})
}

func (h workspaceService) TestDatabase(ctx context.Context, r req[pb.TestDatabaseRequest]) (*connect.Response[pb.TestDatabaseResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	tools, err := w.TestDatabase(ctx, databaseOf(r.Msg.Connection), r.Msg.SecretValues)
	if err != nil {
		return ok(&pb.TestDatabaseResponse{Problem: redact.Mask(err.Error())})
	}
	out := &pb.TestDatabaseResponse{}
	for _, t := range tools {
		out.Tools = append(out.Tools, &pb.DatabaseTool{Name: t.Name, Description: t.Description, ReadOnly: t.ReadOnly})
	}
	return ok(out)
}

func (h workspaceService) TryDatabaseTool(ctx context.Context, r req[pb.TryDatabaseToolRequest]) (*connect.Response[pb.TryDatabaseToolResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	var args map[string]any
	if r.Msg.ArgsJson != "" {
		if err := json.Unmarshal([]byte(r.Msg.ArgsJson), &args); err != nil {
			return ok(&pb.TryDatabaseToolResponse{Problem: "the arguments aren't a JSON object: " + err.Error()})
		}
	}
	out, isErr, err := w.TryDatabaseTool(ctx, databaseOf(r.Msg.Connection), r.Msg.SecretValues, r.Msg.Tool, args)
	if err != nil {
		return ok(&pb.TryDatabaseToolResponse{Problem: redact.Mask(err.Error())})
	}
	return ok(&pb.TryDatabaseToolResponse{Output: redact.Mask(out), IsError: isErr})
}
