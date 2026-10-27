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

// Studio's rules (spec_gates_041).

func ruleMsg(r gates.Rule, dir string) *pb.RuleFile {
	return &pb.RuleFile{
		Name: r.Name, Path: r.Path, When: r.When, Action: r.Action, Order: int32(r.Order), Agents: r.Agents,
		Description: r.Description, Paths: r.Paths, Text: r.Text, Problem: r.Problem,
		Editable: gates.ValidName(r.Name) && filepath.Dir(r.Path) == dir,
	}
}

func ruleSpec(s *pb.RuleSpec) engine.RuleSpec {
	return engine.RuleSpec{Name: s.GetName(), When: s.GetWhen(), Action: s.GetAction(), Order: int(s.GetOrder()), Agents: s.GetAgents(), Description: s.GetDescription(), Paths: s.GetPaths(), Text: s.GetText()}
}

// ruleErr is a rule edit's error as the API's.
func ruleErr(err error) error {
	if errors.Is(err, gates.ErrUnknown) {
		return apiError(connect.CodeNotFound, "RULE_NOT_FOUND", err)
	}
	return err
}

func (h workspaceService) ListRules(ctx context.Context, r req[pb.ListRulesRequest]) (*connect.Response[pb.ListRulesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	dir := w.ProjectRulesDir()
	out := &pb.ListRulesResponse{Dir: dir}
	for _, rule := range w.Rules() {
		out.Rules = append(out.Rules, ruleMsg(rule, dir))
	}
	return ok(out)
}

func (h workspaceService) SaveRule(ctx context.Context, r req[pb.SaveRuleRequest]) (*connect.Response[pb.SaveRuleResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if r.Msg.Rule == nil {
		return nil, invalid(errors.New("rule is required"))
	}
	saved, problems, err := w.SaveRule(ruleSpec(r.Msg.Rule), r.Msg.PreviousName)
	if err != nil {
		return nil, ruleErr(err)
	}
	if len(problems) > 0 {
		return ok(&pb.SaveRuleResponse{Problems: problems})
	}
	return ok(&pb.SaveRuleResponse{Rule: ruleMsg(saved, w.ProjectRulesDir())})
}

func (h workspaceService) DeleteRule(ctx context.Context, r req[pb.DeleteRuleRequest]) (*connect.Response[pb.DeleteRuleResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.DeleteRule(r.Msg.Name); err != nil {
		return nil, ruleErr(err)
	}
	return ok(&pb.DeleteRuleResponse{})
}

func (h workspaceService) ReorderRules(ctx context.Context, r req[pb.ReorderRulesRequest]) (*connect.Response[pb.ReorderRulesResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.ReorderRules(r.Msg.Names); err != nil {
		return nil, ruleErr(err)
	}
	return ok(&pb.ReorderRulesResponse{})
}

func (h workspaceService) TryRule(ctx context.Context, r req[pb.TryRuleRequest]) (*connect.Response[pb.TryRuleResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	s := ruleSpec(r.Msg.Rule)
	if s.When == "" {
		return nil, invalid(errors.New("only a rule with when: (before_agent or after_agent) is judged"))
	}
	v, err := w.JudgeRule(ctx, gates.Rule{Name: s.Name, When: s.When, Action: s.Action, Text: s.Text}, r.Msg.Sample)
	if err != nil {
		return ok(&pb.TryRuleResponse{Problem: err.Error()})
	}
	return ok(&pb.TryRuleResponse{Pass: v.Pass, Reason: v.Reason})
}
