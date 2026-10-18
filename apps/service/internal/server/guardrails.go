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
	"path/filepath"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// Studio's guardrails (spec_gates_041).

func guardrailMsg(g gates.Guardrail, dir string) *pb.GuardrailFile {
	l := g.Limits
	return &pb.GuardrailFile{
		Name: g.Name, Path: g.Path, Builtin: g.Builtin, Stage: g.Stage, Action: g.Action, Agents: g.Agents, Tools: g.Tools,
		Description: g.Description, Pii: g.PII, Judge: g.Judge, Text: g.Text, Problem: g.Problem,
		Limits:   &pb.GuardrailLimits{MaxCostUsd: l.MaxCostUSD, MaxTurns: int32(l.MaxTurns), MaxFilesChanged: int32(l.MaxFilesChanged), AllowedDomains: l.AllowedDomains, Commands: l.Commands},
		Editable: gates.ValidName(g.Name) && filepath.Dir(g.Path) == dir,
	}
}

func guardrailSpec(s *pb.GuardrailSpec) engine.GuardrailSpec {
	l := s.GetLimits()
	return engine.GuardrailSpec{
		Name: s.GetName(), Builtin: s.GetBuiltin(), Stage: s.GetStage(), Action: s.GetAction(), Agents: s.GetAgents(), Tools: s.GetTools(),
		Description: s.GetDescription(), PII: s.GetPii(), Judge: s.GetJudge(), Text: s.GetText(),
		Limits: gates.Limits{MaxCostUSD: l.GetMaxCostUsd(), MaxTurns: int(l.GetMaxTurns()), MaxFilesChanged: int(l.GetMaxFilesChanged()), AllowedDomains: l.GetAllowedDomains(), Commands: l.GetCommands()},
	}
}

func (h workspaceService) ListGuardrails(ctx context.Context, r req[pb.ListGuardrailsRequest]) (*connect.Response[pb.ListGuardrailsResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	dir := w.ProjectGuardrailsDir()
	all := w.Guardrails()
	out := &pb.ListGuardrailsResponse{Dir: dir}
	for _, b := range gates.Builtins {
		bg := &pb.BuiltinGuardrail{Id: b.ID, Stage: b.Stage, DefaultAction: b.Action, Actions: b.Actions}
		for _, g := range all {
			if g.Builtin == b.ID {
				bg.File = guardrailMsg(g, dir)
				break
			}
		}
		out.Builtins = append(out.Builtins, bg)
	}
	for _, g := range all {
		if g.Builtin == "" {
			out.Guardrails = append(out.Guardrails, guardrailMsg(g, dir))
		}
	}
	return ok(out)
}

func (h workspaceService) SaveGuardrail(ctx context.Context, r req[pb.SaveGuardrailRequest]) (*connect.Response[pb.SaveGuardrailResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if r.Msg.Guardrail == nil {
		return nil, invalid(errors.New("guardrail is required"))
	}
	saved, problems, err := w.SaveGuardrail(guardrailSpec(r.Msg.Guardrail), r.Msg.PreviousName)
	if errors.Is(err, gates.ErrUnknown) {
		return nil, apiError(connect.CodeNotFound, "GUARDRAIL_NOT_FOUND", err)
	} else if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return ok(&pb.SaveGuardrailResponse{Problems: problems})
	}
	return ok(&pb.SaveGuardrailResponse{Guardrail: guardrailMsg(saved, w.ProjectGuardrailsDir())})
}

func (h workspaceService) DeleteGuardrail(ctx context.Context, r req[pb.DeleteGuardrailRequest]) (*connect.Response[pb.DeleteGuardrailResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.DeleteGuardrail(r.Msg.Name); err != nil {
		return nil, apiError(connect.CodeNotFound, "GUARDRAIL_NOT_FOUND", err)
	}
	return ok(&pb.DeleteGuardrailResponse{})
}

func (h workspaceService) TryGuardrail(ctx context.Context, r req[pb.TryGuardrailRequest]) (*connect.Response[pb.TryGuardrailResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s := guardrailSpec(r.Msg.Guardrail)
	g := gates.Guardrail{Name: s.Name, Builtin: s.Builtin, Stage: s.Stage, Action: s.Action, PII: s.PII, Judge: s.Judge, Text: s.Text}
	if g.Builtin != "" {
		g.Name = g.Builtin
	}
	v, err := w.TryGuardrail(ctx, g, r.Msg.Sample)
	if err != nil {
		return ok(&pb.TryGuardrailResponse{Problem: err.Error()})
	}
	return ok(&pb.TryGuardrailResponse{Pass: v.Pass, Reason: v.Reason})
}
