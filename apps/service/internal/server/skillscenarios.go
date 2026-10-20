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
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

// A skill's scenarios and trying it (spec_skills_013 SK-111, SK-112).
// Scenarios run through RunEval, as the eval "skill:<name>".

func scenarioMsg(sc skills.Scenario) *pb.SkillScenario {
	return &pb.SkillScenario{Name: sc.Name, Description: sc.Description, Prompt: sc.Prompt, Executes: sc.Executes, ExpectedSkills: sc.ExpectedSkills, Threshold: sc.Threshold, Outcome: sc.Outcome, Path: sc.Path, Problem: sc.Problem}
}

func scenarioOf(m *pb.SkillScenario) skills.Scenario {
	return skills.Scenario{Name: m.GetName(), Description: m.GetDescription(), Prompt: m.GetPrompt(), Executes: m.GetExecutes(), ExpectedSkills: m.GetExpectedSkills(), Threshold: m.GetThreshold(), Outcome: m.GetOutcome()}
}

// scenarioErr maps a scenario call's errors.
func scenarioErr(err error) error {
	switch {
	case errors.Is(err, skills.ErrUnknownScenario):
		return apiError(connect.CodeNotFound, "SCENARIO_NOT_FOUND", err)
	case errors.Is(err, engine.ErrNoPrompt):
		return invalid(err)
	case errors.Is(err, skills.ErrUnknownSkill):
		return skillErr(err)
	}
	return err
}

func (h workspaceService) ListSkillScenarios(ctx context.Context, r req[pb.ListSkillScenariosRequest]) (*connect.Response[pb.ListSkillScenariosResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	list, err := w.SkillScenarios(r.Msg.Skill)
	if err != nil {
		return nil, scenarioErr(err)
	}
	out := &pb.ListSkillScenariosResponse{}
	for _, sc := range list {
		out.Scenarios = append(out.Scenarios, scenarioMsg(sc))
	}
	return ok(out)
}

func (h workspaceService) SaveSkillScenario(ctx context.Context, r req[pb.SaveSkillScenarioRequest]) (*connect.Response[pb.SaveSkillScenarioResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if r.Msg.Scenario == nil {
		return nil, invalid(errors.New("scenario is required"))
	}
	saved, problems, err := w.SaveSkillScenario(r.Msg.Skill, scenarioOf(r.Msg.Scenario), r.Msg.PreviousName)
	if err != nil {
		return nil, scenarioErr(err)
	}
	if len(problems) > 0 {
		return ok(&pb.SaveSkillScenarioResponse{Problems: problems})
	}
	return ok(&pb.SaveSkillScenarioResponse{Scenario: scenarioMsg(saved)})
}

func (h workspaceService) DeleteSkillScenario(ctx context.Context, r req[pb.DeleteSkillScenarioRequest]) (*connect.Response[pb.DeleteSkillScenarioResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := w.DeleteSkillScenario(r.Msg.Skill, r.Msg.Name); err != nil {
		return nil, scenarioErr(err)
	}
	return ok(&pb.DeleteSkillScenarioResponse{})
}

func (h workspaceService) TrySkill(ctx context.Context, r req[pb.TrySkillRequest]) (*connect.Response[pb.TrySkillResponse], error) {
	w, err := h.s.workspace(ctx, r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	t, err := w.TrySkill(ctx, r.Msg.Skill, r.Msg.Agent, r.Msg.Prompt, r.Msg.Force)
	if err != nil {
		return nil, scenarioErr(err)
	}
	return ok(&pb.TrySkillResponse{Agent: t.Agent, Output: t.Output, SkillsUsed: t.SkillsUsed, Triggered: t.Triggered, Forced: t.Forced, Refusals: t.Refusals, CostUsd: t.CostUSD, Duration: durationpb.New(t.Duration)})
}
