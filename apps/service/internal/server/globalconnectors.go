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
	cats, kinds, conns := listed(g, nil)
	return ok(&pb.ListGlobalConnectorsResponse{Categories: cats, Kinds: kinds, Connections: conns, GlobalDir: connectors.GlobalDir(g.Config.Dir)})
}

func (h configService) SaveGlobalConnector(ctx context.Context, r req[pb.SaveGlobalConnectorRequest]) (*connect.Response[pb.SaveGlobalConnectorResponse], error) {
	if r.Msg.Connection == nil {
		return nil, invalid(errors.New("connection is required"))
	}
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	saved, problems, err := saveIn(ctx, g, r.Msg.Connection, r.Msg.SecretValues, r.Msg.PreviousName)
	if err != nil {
		return nil, err
	}
	if saved != nil {
		h.s.reloadConnectors(ctx, "")
	}
	return ok(&pb.SaveGlobalConnectorResponse{Connection: saved, Problems: problems})
}

func (h configService) DeleteGlobalConnector(ctx context.Context, r req[pb.DeleteGlobalConnectorRequest]) (*connect.Response[pb.DeleteGlobalConnectorResponse], error) {
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	if err := g.Delete(ctx, connectors.Ref{Category: r.Msg.Category, Name: r.Msg.Name}); err != nil {
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
	tools, problem := testIn(ctx, g, r.Msg.Connection, r.Msg.SecretValues)
	return ok(&pb.TestGlobalConnectorResponse{Tools: tools, Problem: problem})
}

func (h configService) TryGlobalConnectorTool(ctx context.Context, r req[pb.TryGlobalConnectorToolRequest]) (*connect.Response[pb.TryGlobalConnectorToolResponse], error) {
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	out, isErr, problem := tryIn(ctx, g, r.Msg.Connection, r.Msg.SecretValues, r.Msg.Tool, r.Msg.ArgsJson)
	return ok(&pb.TryGlobalConnectorToolResponse{Output: out, IsError: isErr, Problem: problem})
}

func (h configService) ReadGlobalConnectorSchema(ctx context.Context, r req[pb.ReadGlobalConnectorSchemaRequest]) (*connect.Response[pb.ReadGlobalConnectorSchemaResponse], error) {
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	tables, problem := readIn(ctx, g, r.Msg.Connection, r.Msg.SecretValues)
	return ok(&pb.ReadGlobalConnectorSchemaResponse{Tables: tables, Problem: problem})
}

func (h configService) DraftGlobalConnectorQuery(ctx context.Context, r req[pb.DraftGlobalConnectorQueryRequest]) (*connect.Response[pb.DraftGlobalConnectorQueryResponse], error) {
	if r.Msg.Connection == nil {
		return nil, invalid(errors.New("connection is required"))
	}
	g, err := h.globalConnectors()
	if err != nil {
		return nil, err
	}
	d, err := draftIn(ctx, g, r.Msg.Connection, r.Msg.SecretValues, r.Msg.Request)
	if err != nil {
		return nil, err
	}
	return ok(&pb.DraftGlobalConnectorQueryResponse{Query: d.Query, Notes: d.Notes, Tables: d.Tables, Problem: d.Problem})
}
