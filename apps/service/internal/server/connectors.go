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
	"fmt"

	"connectrpc.com/connect"

	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/databases"
	"github.com/retail-cortex/blitz/pkg/redact"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Studio's connectors (spec_connectors_047).

// Reasons: a connection that isn't there, a global one a workspace was
// asked to change (ConfigService changes those), and one the person
// hasn't trusted.
const (
	errNoConnector        = "CONNECTOR_NOT_FOUND"
	errGlobalConnector    = "CONNECTOR_GLOBAL"
	errUntrustedConnector = "CONNECTOR_NOT_TRUSTED"
)

func queryMsg(q connectors.Query) *pb.ConnectorQuery {
	m := &pb.ConnectorQuery{Name: q.Name, Description: q.Description, Statement: q.Statement, ReadsOnly: databases.ReadsOnly(q.Statement)}
	for _, p := range q.Params {
		m.Params = append(m.Params, &pb.ConnectorQueryParam{Name: p.Name, Type: p.Type, Description: p.Description})
	}
	return m
}

// connectorMsg is c for the API; hasSecret says which of its secret
// fields have a value.
func connectorMsg(hasSecret func(connectors.Connection, string) bool, cat connectors.Catalog, c connectors.Connection) *pb.Connector {
	m := &pb.Connector{
		Name: c.Name, Category: c.Category, Scope: c.Scope, Path: c.Path, Kind: c.Kind, Description: c.Description, Settings: c.Settings,
		ReadOnly: c.ReadOnly, Tools: c.Tools, Custom: c.Custom, Text: c.Text, Problem: c.Problem, Trusted: c.Trusted, Enabled: c.On,
	}
	for _, q := range c.Queries {
		m.Queries = append(m.Queries, queryMsg(q))
	}
	if kind, ok := cat.Kind(c.Kind); ok && !c.Custom {
		for _, f := range kind.Fields {
			if f.Secret() && hasSecret(c, f.Key) {
				m.SecretsSet = append(m.SecretsSet, f.Key)
			}
		}
	}
	return m
}

func connectorOf(m *pb.Connector) connectors.Connection {
	c := connectors.Connection{
		Name: m.GetName(), Category: m.GetCategory(), Scope: m.GetScope(), Kind: m.GetKind(), Description: m.GetDescription(), Settings: m.GetSettings(),
		ReadOnly: m.GetReadOnly(), Tools: m.GetTools(), Custom: m.GetCustom(), Text: m.GetText(),
	}
	for _, q := range m.GetQueries() {
		dq := connectors.Query{Name: q.GetName(), Description: q.GetDescription(), Statement: q.GetStatement()}
		for _, p := range q.GetParams() {
			dq.Params = append(dq.Params, connectors.Param{Name: p.GetName(), Type: p.GetType(), Description: p.GetDescription()})
		}
		c.Queries = append(c.Queries, dq)
	}
	return c
}

func kindMsg(k connectors.Kind) *pb.ConnectorKind {
	m := &pb.ConnectorKind{
		Id: k.ID, Category: k.Category, Name: k.Name, Hint: k.Hint, Backend: k.Backend, CanReadOnly: k.ReadOnly != nil,
		Origin: k.Origin, Path: k.Path, Problem: k.Problem,
	}
	for _, c := range k.Capabilities {
		m.Capabilities = append(m.Capabilities, string(c))
	}
	for _, f := range k.Fields {
		m.Fields = append(m.Fields, &pb.ConnectorField{Key: f.Key, Label: f.Label, Help: f.Help, Type: f.Type, Required: f.Required, DefaultValue: f.Default})
	}
	for _, t := range k.Tools {
		m.Tools = append(m.Tools, &pb.ConnectorReadyTool{Id: t.ID, About: t.About, Writes: t.Writes, On: t.On})
	}
	if k.ReadOnly != nil {
		m.ReadOnlyHint = k.ReadOnly.Hint
	}
	if q := k.Queries; q != nil {
		m.QueriesHint, m.QueriesExample, m.Placeholders = q.Hint, q.Example, q.Placeholders
	}
	return m
}

// categoryMsg is c for the API, with its folders (workspace "": the
// global one alone).
func categoryMsg(c connectors.Category, prefix, workspace string) *pb.ConnectorCategory {
	return &pb.ConnectorCategory{
		Id: c.ID, Name: c.Name, Hint: c.Hint, Icon: c.Icon, Server: c.Server, Prefix: c.Prefix,
		WorkspaceDir: connectors.ConnectionDir(prefix, workspace, connectors.ScopeWorkspace, c.ID),
		GlobalDir:    connectors.ConnectionDir(prefix, workspace, connectors.ScopeGlobal, c.ID),
		Origin:       c.Origin, Path: c.Path, Problem: c.Problem,
	}
}

// toolsMsg, tablesMsg and draftMsg are a test's tools, a schema and a
// query draft for the API.
func toolsMsg(tools []connectors.ToolInfo) []*pb.ConnectorTool {
	var out []*pb.ConnectorTool
	for _, t := range tools {
		out = append(out, &pb.ConnectorTool{Name: t.Name, Description: t.Description, ReadOnly: t.ReadOnly})
	}
	return out
}

func tablesMsg(tables []connectors.Table) []*pb.ConnectorTable {
	var out []*pb.ConnectorTable
	for _, t := range tables {
		tm := &pb.ConnectorTable{Name: t.Name, About: t.About}
		for _, c := range t.Columns {
			tm.Columns = append(tm.Columns, &pb.ConnectorColumn{Name: c.Name, Type: c.Type, About: c.About})
		}
		out = append(out, tm)
	}
	return out
}

func draftMsg(d engine.QueryDraft) *pb.DraftConnectorQueryResponse {
	out := &pb.DraftConnectorQueryResponse{Notes: d.Notes, Tables: int32(d.Tables), Problem: d.Problem}
	if d.Problem == "" {
		out.Query = queryMsg(d.Query)
	}
	return out
}

// argsOf is Try it's JSON arguments.
func argsOf(text string) (map[string]any, error) {
	var args map[string]any
	if text == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(text), &args); err != nil {
		return nil, fmt.Errorf("the arguments aren't a JSON object: %w", err)
	}
	return args, nil
}

// connectorError is an engine error for the API: a connection that isn't
// there, a global one asked of a workspace, or one not trusted.
func connectorError(err error) error {
	switch {
	case errors.Is(err, connectors.ErrUnknown):
		return apiError(connect.CodeNotFound, errNoConnector, err)
	case errors.Is(err, engine.ErrGlobalConnector):
		return apiError(connect.CodeFailedPrecondition, errGlobalConnector, err)
	case errors.Is(err, engine.ErrUntrustedConnector):
		return apiError(connect.CodeFailedPrecondition, errUntrustedConnector, err)
	}
	return err
}

func (h workspaceService) ListConnectors(ctx context.Context, r req[pb.ListConnectorsRequest]) (*connect.Response[pb.ListConnectorsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	prefix := w.Config().Dir
	out := &pb.ListConnectorsResponse{WorkspaceDir: connectors.WorkspaceDir(w.Dir()), GlobalDir: connectors.GlobalDir(prefix)}
	cat := w.ConnectorCatalog()
	for _, c := range cat.Categories {
		m := categoryMsg(c, prefix, w.Dir())
		if c.Problem == "" {
			st := w.ConnectorState(c.ID)
			m.State, m.Error, m.ToolCount = st.State, st.Error, int32(st.Tools)
		}
		out.Categories = append(out.Categories, m)
	}
	for _, k := range cat.Kinds {
		out.Kinds = append(out.Kinds, kindMsg(k))
	}
	for _, c := range w.Connectors() {
		out.Connections = append(out.Connections, connectorMsg(w.HasConnectorSecret, cat, c))
	}
	return ok(out)
}

func (h workspaceService) SaveConnector(ctx context.Context, r req[pb.SaveConnectorRequest]) (*connect.Response[pb.SaveConnectorResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if r.Msg.Connection == nil {
		return nil, invalid(errors.New("connection is required"))
	}
	saved, problems, err := w.SaveConnector(ctx, connectorOf(r.Msg.Connection), r.Msg.SecretValues, r.Msg.PreviousName)
	if err != nil {
		return nil, connectorError(err)
	}
	if len(problems) > 0 {
		return ok(&pb.SaveConnectorResponse{Problems: problems})
	}
	return ok(&pb.SaveConnectorResponse{Connection: connectorMsg(w.HasConnectorSecret, w.ConnectorCatalog(), saved)})
}

func (h workspaceService) DeleteConnector(ctx context.Context, r req[pb.DeleteConnectorRequest]) (*connect.Response[pb.DeleteConnectorResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.DeleteConnector(ctx, r.Msg.Category, r.Msg.Scope, r.Msg.Name); err != nil {
		return nil, connectorError(err)
	}
	return ok(&pb.DeleteConnectorResponse{})
}

func (h workspaceService) MakeConnectorGlobal(ctx context.Context, r req[pb.MakeConnectorGlobalRequest]) (*connect.Response[pb.MakeConnectorGlobalResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	moved, problems, err := w.MakeConnectorGlobal(ctx, r.Msg.Category, r.Msg.Name)
	if err != nil {
		return nil, connectorError(err)
	}
	if len(problems) > 0 {
		return ok(&pb.MakeConnectorGlobalResponse{Problems: problems})
	}
	h.s.reloadConnectors(ctx, w.Dir())
	return ok(&pb.MakeConnectorGlobalResponse{Connection: connectorMsg(w.HasConnectorSecret, w.ConnectorCatalog(), moved)})
}

func (h workspaceService) SetConnectorEnabled(ctx context.Context, r req[pb.SetConnectorEnabledRequest]) (*connect.Response[pb.SetConnectorEnabledResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.SetConnectorOn(ctx, r.Msg.Category, r.Msg.Scope, r.Msg.Name, r.Msg.Enabled); err != nil {
		return nil, connectorError(err)
	}
	return ok(&pb.SetConnectorEnabledResponse{})
}

func (h workspaceService) TestConnector(ctx context.Context, r req[pb.TestConnectorRequest]) (*connect.Response[pb.TestConnectorResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	tools, err := w.TestConnector(ctx, connectorOf(r.Msg.Connection), r.Msg.SecretValues)
	if err != nil {
		return ok(&pb.TestConnectorResponse{Problem: redact.Mask(err.Error())})
	}
	return ok(&pb.TestConnectorResponse{Tools: toolsMsg(tools)})
}

func (h workspaceService) TryConnectorTool(ctx context.Context, r req[pb.TryConnectorToolRequest]) (*connect.Response[pb.TryConnectorToolResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	args, err := argsOf(r.Msg.ArgsJson)
	if err != nil {
		return ok(&pb.TryConnectorToolResponse{Problem: err.Error()})
	}
	out, isErr, err := w.TryConnectorTool(ctx, connectorOf(r.Msg.Connection), r.Msg.SecretValues, r.Msg.Tool, args)
	if err != nil {
		return ok(&pb.TryConnectorToolResponse{Problem: redact.Mask(err.Error())})
	}
	return ok(&pb.TryConnectorToolResponse{Output: redact.Mask(out), IsError: isErr})
}

func (h workspaceService) ReadConnectorSchema(ctx context.Context, r req[pb.ReadConnectorSchemaRequest]) (*connect.Response[pb.ReadConnectorSchemaResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	tables, err := w.ReadConnectorSchema(ctx, connectorOf(r.Msg.Connection), r.Msg.SecretValues)
	if err != nil {
		return ok(&pb.ReadConnectorSchemaResponse{Problem: redact.Mask(err.Error())})
	}
	return ok(&pb.ReadConnectorSchemaResponse{Tables: tablesMsg(tables)})
}

func (h workspaceService) DraftConnectorQuery(ctx context.Context, r req[pb.DraftConnectorQueryRequest]) (*connect.Response[pb.DraftConnectorQueryResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if r.Msg.Connection == nil {
		return nil, invalid(errors.New("connection is required"))
	}
	d, err := w.DraftConnectorQuery(ctx, connectorOf(r.Msg.Connection), r.Msg.SecretValues, r.Msg.Request)
	if err != nil { // no query builder, no request, no such category
		return nil, invalid(err)
	}
	return ok(draftMsg(d))
}
