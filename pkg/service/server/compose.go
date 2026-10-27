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

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/compose"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The composer over the API (spec_composer_045).

func composeMsg(s compose.Session) *pb.ComposeSession {
	m := &pb.ComposeSession{Id: s.ID, Kind: s.Kind, Asking: s.Asking(), CostUsd: s.CostUSD}
	for _, t := range s.Turns {
		m.Turns = append(m.Turns, &pb.ComposeTurn{Role: t.Role, Text: t.Text, Options: t.Options})
	}
	if d := s.Draft; d != nil {
		m.Draft = &pb.ComposeDraft{Name: d.Name, Summary: d.Summary, File: d.File, Problems: d.Problems}
		switch s.Kind {
		case "workflow":
			// What the canvas opens, as written even with problems.
			m.Draft.Workflow = workflowMsg(flows.Parse(d.Name, "", []byte(d.File)))
		case "job":
			// What the New Job form opens (WK-74).
			m.Draft.Worker = workerDefinition(workers.ParseSpec(d.Name, []byte(d.File)))
		}
	}
	return m
}

func composeError(err error) error {
	switch {
	case errors.Is(err, compose.ErrNoSession):
		return apiError(connect.CodeNotFound, "COMPOSE_NOT_FOUND", err)
	case errors.Is(err, engine.ErrNoRecipe):
		return invalid(err)
	}
	// Not Unavailable: that reads as the service gone, and the page says so.
	return apiError(connect.CodeFailedPrecondition, "COMPOSE_FAILED", err)
}

func (h workspaceService) StartCompose(ctx context.Context, r req[pb.StartComposeRequest]) (*connect.Response[pb.StartComposeResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, err := w.StartCompose(ctx, r.Msg.Kind, r.Msg.Request, r.Msg.Current)
	if err != nil {
		return nil, composeError(err)
	}
	return ok(&pb.StartComposeResponse{Session: composeMsg(s)})
}

func (h workspaceService) AnswerCompose(ctx context.Context, r req[pb.AnswerComposeRequest]) (*connect.Response[pb.AnswerComposeResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s, err := w.AnswerCompose(ctx, r.Msg.Id, r.Msg.Answer)
	if err != nil {
		return nil, composeError(err)
	}
	return ok(&pb.AnswerComposeResponse{Session: composeMsg(s)})
}

func (h workspaceService) DropCompose(ctx context.Context, r req[pb.DropComposeRequest]) (*connect.Response[pb.DropComposeResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	w.DropCompose(r.Msg.Id)
	return ok(&pb.DropComposeResponse{})
}
