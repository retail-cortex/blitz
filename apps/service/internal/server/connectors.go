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

// connectorMsg is c for the API, with the secret fields that have a
// value (kept by d).
func connectorMsg(d connectors.Dirs, cat connectors.Catalog, c connectors.Connection) *pb.Connector {
	m := &pb.Connector{
		Name: c.Name, Category: c.Category, Scope: c.Scope, Path: c.Path, Kind: c.Kind, Description: c.Description, Settings: c.Settings,
		ReadOnly: c.ReadOnly, Tools: c.Tools, Custom: c.Custom, Text: c.Text, Problem: c.Problem, Trusted: c.Trusted, Enabled: c.On,
	}
	for _, q := range c.Queries {
		m.Queries = append(m.Queries, queryMsg(q))
	}
	category, okCat := cat.Category(c.Category)
	if kind, ok := cat.Kind(c.Kind); ok && okCat && !c.Custom {
		for _, f := range kind.SecretFields() {
			if connectors.HasSecret(d, category, c, f.Key) {
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
		WorkspaceDir: connectors.Dirs{Prefix: prefix, Workspace: workspace}.ConnectionDir(connectors.ScopeWorkspace, c.ID),
		GlobalDir:    connectors.Dirs{Prefix: prefix}.ConnectionDir(connectors.ScopeGlobal, c.ID),
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

// argsOf is the JSON arguments Try it sends.
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

// listed is set listed for the API: its categories (with how each
// category's server is doing, by state; nil: not said), kinds and
// connections, the catalog read once.
func listed(set engine.ConnectorSet, state func(connectors.Category) (string, string, int)) ([]*pb.ConnectorCategory, []*pb.ConnectorKind, []*pb.Connector) {
	d, cat := set.Dirs(), set.Catalog()
	var cats []*pb.ConnectorCategory
	for _, c := range cat.Categories {
		m := categoryMsg(c, d.Prefix, d.Workspace)
		if c.Problem == "" && state != nil {
			st, why, n := state(c)
			m.State, m.Error, m.ToolCount = st, why, int32(n)
		}
		cats = append(cats, m)
	}
	var kinds []*pb.ConnectorKind
	for _, k := range cat.Kinds {
		kinds = append(kinds, kindMsg(k))
	}
	var conns []*pb.Connector
	for _, c := range set.List() {
		conns = append(conns, connectorMsg(d, cat, c))
	}
	return cats, kinds, conns
}

// saveIn saves m in set (problems, with nothing written, when it can't
// be), the saved connection for the API.
func saveIn(ctx context.Context, set engine.ConnectorSet, m *pb.Connector, values map[string]string, previous string) (*pb.Connector, []string, error) {
	if m == nil {
		return nil, nil, invalid(errors.New("connection is required"))
	}
	saved, problems, err := set.Save(ctx, connectorOf(m), values, previous)
	if err != nil {
		return nil, nil, connectorError(err)
	}
	if len(problems) > 0 {
		return nil, problems, nil
	}
	return connectorMsg(set.Dirs(), set.Catalog(), saved), nil, nil
}

// testIn tests m in set: its tools, or why it can't be reached.
func testIn(ctx context.Context, set engine.ConnectorSet, m *pb.Connector, values map[string]string) ([]*pb.ConnectorTool, string) {
	tools, err := set.Test(ctx, connectorOf(m), values)
	if err != nil {
		return nil, redact.Mask(err.Error())
	}
	return toolsMsg(tools), ""
}

// tryIn runs one of m's tools in set: what it answered and whether
// that's its error, or why it couldn't run.
func tryIn(ctx context.Context, set engine.ConnectorSet, m *pb.Connector, values map[string]string, tool, argsJSON string) (string, bool, string) {
	args, err := argsOf(argsJSON)
	if err != nil {
		return "", false, err.Error()
	}
	out, isErr, err := set.Try(ctx, connectorOf(m), values, tool, args)
	if err != nil {
		return "", false, redact.Mask(err.Error())
	}
	return redact.Mask(out), isErr, ""
}

// readIn reads m's schema in set: its tables, or why it read none.
func readIn(ctx context.Context, set engine.ConnectorSet, m *pb.Connector, values map[string]string) ([]*pb.ConnectorTable, string) {
	tables, err := set.ReadSchema(ctx, connectorOf(m), values)
	if err != nil {
		return nil, redact.Mask(err.Error())
	}
	return tablesMsg(tables), ""
}

// draftIn drafts a query for m in set: the draft, or why there's none
// (a request it can't take is invalid; a connection that isn't there,
// not found).
func draftIn(ctx context.Context, set engine.ConnectorSet, m *pb.Connector, values map[string]string, request string) (*pb.DraftConnectorQueryResponse, error) {
	if m == nil {
		return nil, invalid(errors.New("connection is required"))
	}
	d, err := set.DraftQuery(ctx, connectorOf(m), values, request)
	if errors.Is(err, connectors.ErrUnknown) {
		return nil, connectorError(err)
	} else if err != nil {
		return nil, invalid(err)
	}
	return draftMsg(d), nil
}

func (h workspaceService) ListConnectors(ctx context.Context, r req[pb.ListConnectorsRequest]) (*connect.Response[pb.ListConnectorsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	cats, kinds, conns := listed(w.ConnectorSet(), func(c connectors.Category) (string, string, int) {
		st := w.ConnectorState(c)
		return st.State, st.Error, st.Tools
	})
	return ok(&pb.ListConnectorsResponse{
		Categories: cats, Kinds: kinds, Connections: conns,
		WorkspaceDir: connectors.WorkspaceDir(w.Dir()), GlobalDir: connectors.GlobalDir(w.Config().Dir),
	})
}

func (h workspaceService) SaveConnector(ctx context.Context, r req[pb.SaveConnectorRequest]) (*connect.Response[pb.SaveConnectorResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	saved, problems, err := saveIn(ctx, w.ConnectorSet(), r.Msg.Connection, r.Msg.SecretValues, r.Msg.PreviousName)
	if err != nil {
		return nil, err
	}
	return ok(&pb.SaveConnectorResponse{Connection: saved, Problems: problems})
}

func (h workspaceService) DeleteConnector(ctx context.Context, r req[pb.DeleteConnectorRequest]) (*connect.Response[pb.DeleteConnectorResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.ConnectorSet().Delete(ctx, connectors.Ref{Scope: r.Msg.Scope, Category: r.Msg.Category, Name: r.Msg.Name}); err != nil {
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
	set := w.ConnectorSet()
	return ok(&pb.MakeConnectorGlobalResponse{Connection: connectorMsg(set.Dirs(), set.Catalog(), moved)})
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
	tools, problem := testIn(ctx, w.ConnectorSet(), r.Msg.Connection, r.Msg.SecretValues)
	return ok(&pb.TestConnectorResponse{Tools: tools, Problem: problem})
}

func (h workspaceService) TryConnectorTool(ctx context.Context, r req[pb.TryConnectorToolRequest]) (*connect.Response[pb.TryConnectorToolResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	out, isErr, problem := tryIn(ctx, w.ConnectorSet(), r.Msg.Connection, r.Msg.SecretValues, r.Msg.Tool, r.Msg.ArgsJson)
	return ok(&pb.TryConnectorToolResponse{Output: out, IsError: isErr, Problem: problem})
}

func (h workspaceService) ReadConnectorSchema(ctx context.Context, r req[pb.ReadConnectorSchemaRequest]) (*connect.Response[pb.ReadConnectorSchemaResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	tables, problem := readIn(ctx, w.ConnectorSet(), r.Msg.Connection, r.Msg.SecretValues)
	return ok(&pb.ReadConnectorSchemaResponse{Tables: tables, Problem: problem})
}

func (h workspaceService) DraftConnectorQuery(ctx context.Context, r req[pb.DraftConnectorQueryRequest]) (*connect.Response[pb.DraftConnectorQueryResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	d, err := draftIn(ctx, w.ConnectorSet(), r.Msg.Connection, r.Msg.SecretValues, r.Msg.Request)
	if err != nil {
		return nil, err
	}
	return ok(d)
}
