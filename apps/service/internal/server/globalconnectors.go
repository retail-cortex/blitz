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
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/redact"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Settings' global connectors (spec_connectors_047 CON-18): the person's
// own, every workspace's, changed here and never by a workspace's Studio.

// globalConnectors are the global connections with the global settings.
func (h configService) globalConnectors() (engine.GlobalConnectors, error) {
	cfg, err := config.Load(h.s.configDir)
	if err != nil {
		return engine.GlobalConnectors{}, err
	}
	return engine.GlobalConnectors{Config: cfg}, nil
}

// reloadConnectors serves the connector files again in every open
// workspace but skip (one that did already), so a global change is the
// next turn's everywhere without waiting for the settings watch.
func (s *Server) reloadConnectors(ctx context.Context, skip string) {
	s.mu.Lock()
	var ws []*workspace
	for _, w := range s.workspaces {
		if w.Dir() != skip {
			ws = append(ws, w)
		}
	}
	s.mu.Unlock()
	for _, w := range ws {
		if err := w.ReloadConnectors(ctx); err != nil {
			slog.Warn("connectors not reloaded", "workspace", w.Dir(), "error", err)
		}
	}
}

func (h configService) ListGlobalConnectors(ctx context.Context, _ req[pb.ListGlobalConnectorsRequest]) (*connect.Response[pb.ListGlobalConnectorsResponse], error) {
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	prefix := g.Config.Dir
	out := &pb.ListGlobalConnectorsResponse{GlobalDir: connectors.GlobalDir(prefix)}
	cat := g.Catalog()
	for _, c := range cat.Categories {
		out.Categories = append(out.Categories, categoryMsg(c, prefix, ""))
	}
	for _, k := range cat.Kinds {
		out.Kinds = append(out.Kinds, kindMsg(k))
	}
	for _, c := range g.List() {
		out.Connections = append(out.Connections, connectorMsg(g.HasSecret, cat, c))
	}
	return ok(out)
}

func (h configService) SaveGlobalConnector(ctx context.Context, r req[pb.SaveGlobalConnectorRequest]) (*connect.Response[pb.SaveGlobalConnectorResponse], error) {
	if r.Msg.Connection == nil {
		return nil, invalid(errors.New("connection is required"))
	}
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	saved, problems, err := g.Save(connectorOf(r.Msg.Connection), r.Msg.SecretValues, r.Msg.PreviousName)
	if err != nil {
		return nil, connectorError(err)
	}
	if len(problems) > 0 {
		return ok(&pb.SaveGlobalConnectorResponse{Problems: problems})
	}
	h.s.reloadConnectors(ctx, "")
	return ok(&pb.SaveGlobalConnectorResponse{Connection: connectorMsg(g.HasSecret, g.Catalog(), saved)})
}

func (h configService) DeleteGlobalConnector(ctx context.Context, r req[pb.DeleteGlobalConnectorRequest]) (*connect.Response[pb.DeleteGlobalConnectorResponse], error) {
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	if err := g.Delete(r.Msg.Category, r.Msg.Name); err != nil {
		return nil, connectorError(err)
	}
	h.s.reloadConnectors(ctx, "")
	return ok(&pb.DeleteGlobalConnectorResponse{})
}

func (h configService) TestGlobalConnector(ctx context.Context, r req[pb.TestGlobalConnectorRequest]) (*connect.Response[pb.TestGlobalConnectorResponse], error) {
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	tools, err := g.Test(ctx, connectorOf(r.Msg.Connection), r.Msg.SecretValues)
	if err != nil {
		return ok(&pb.TestGlobalConnectorResponse{Problem: redact.Mask(err.Error())})
	}
	return ok(&pb.TestGlobalConnectorResponse{Tools: toolsMsg(tools)})
}

func (h configService) TryGlobalConnectorTool(ctx context.Context, r req[pb.TryGlobalConnectorToolRequest]) (*connect.Response[pb.TryGlobalConnectorToolResponse], error) {
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	args, err := argsOf(r.Msg.ArgsJson)
	if err != nil {
		return ok(&pb.TryGlobalConnectorToolResponse{Problem: err.Error()})
	}
	out, isErr, err := g.Try(ctx, connectorOf(r.Msg.Connection), r.Msg.SecretValues, r.Msg.Tool, args)
	if err != nil {
		return ok(&pb.TryGlobalConnectorToolResponse{Problem: redact.Mask(err.Error())})
	}
	return ok(&pb.TryGlobalConnectorToolResponse{Output: redact.Mask(out), IsError: isErr})
}

func (h configService) ReadGlobalConnectorSchema(ctx context.Context, r req[pb.ReadGlobalConnectorSchemaRequest]) (*connect.Response[pb.ReadGlobalConnectorSchemaResponse], error) {
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	tables, err := g.ReadSchema(ctx, connectorOf(r.Msg.Connection), r.Msg.SecretValues)
	if err != nil {
		return ok(&pb.ReadGlobalConnectorSchemaResponse{Problem: redact.Mask(err.Error())})
	}
	return ok(&pb.ReadGlobalConnectorSchemaResponse{Tables: tablesMsg(tables)})
}

func (h configService) DraftGlobalConnectorQuery(ctx context.Context, r req[pb.DraftGlobalConnectorQueryRequest]) (*connect.Response[pb.DraftGlobalConnectorQueryResponse], error) {
	if r.Msg.Connection == nil {
		return nil, invalid(errors.New("connection is required"))
	}
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	d, err := g.DraftQuery(ctx, connectorOf(r.Msg.Connection), r.Msg.SecretValues, r.Msg.Request)
	if err != nil { // no query builder, no request, no such category
		return nil, invalid(err)
	}
	m := draftMsg(d)
	return ok(&pb.DraftGlobalConnectorQueryResponse{Query: m.Query, Notes: m.Notes, Tables: m.Tables, Problem: m.Problem})
}
