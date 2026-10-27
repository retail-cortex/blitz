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
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// A skill's trigger phrases and scenarios over the API: saved and read
// back, tried, run as the eval "skill:<name>" (spec_skills_013 SK-110–
// SK-112).
func TestSkillScenariosOverTheAPI(t *testing.T) {
	c, _ := serve(t, nil, genai.NewContentFromText("Hello.", genai.RoleModel), genai.NewContentFromText("## What's new", genai.RoleModel))
	w := c.workspaces
	ctx := context.Background()
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	made, err := w.SaveSkill(ctx, connect.NewRequest(&pb.SaveSkillRequest{Workspace: dir, Skill: &pb.SkillSpec{Name: "notes", Description: "Write release notes", Instructions: "Read the log.", Triggers: []string{" write the release notes ", ""}}}))
	require.NoError(t, err)
	require.Empty(t, made.Msg.Problems)
	assert.Equal(t, []string{"write the release notes"}, made.Msg.File.Spec.Triggers, "tidied")
	assert.Equal(t, []string{"write the release notes"}, made.Msg.File.Skill.Triggers)

	sc := &pb.SkillScenario{Name: "since-tag", Prompt: "Write the notes for v2", Executes: true, Threshold: 0.8, Outcome: "## What's new"}
	saved, err := w.SaveSkillScenario(ctx, connect.NewRequest(&pb.SaveSkillScenarioRequest{Workspace: dir, Skill: "notes", Scenario: sc}))
	require.NoError(t, err)
	require.Empty(t, saved.Msg.Problems)
	assert.Equal(t, filepath.Join(dir, ".agents", "skills", "notes", "scenarios", "since-tag.md"), saved.Msg.Scenario.Path)
	bad, err := w.SaveSkillScenario(ctx, connect.NewRequest(&pb.SaveSkillScenarioRequest{Workspace: dir, Skill: "notes", Scenario: &pb.SkillScenario{Name: "x"}}))
	require.NoError(t, err)
	assert.NotEmpty(t, bad.Msg.Problems)
	assert.Equal(t, "SCENARIO_NOT_FOUND", reason(t, unary(w.SaveSkillScenario, &pb.SaveSkillScenarioRequest{Workspace: dir, Skill: "notes", PreviousName: "gone", Scenario: sc})))
	assert.Equal(t, "SKILL_NOT_FOUND", reason(t, unary(w.SaveSkillScenario, &pb.SaveSkillScenarioRequest{Workspace: dir, Skill: "nope", Scenario: sc})))
	list, err := w.ListSkillScenarios(ctx, connect.NewRequest(&pb.ListSkillScenariosRequest{Workspace: dir, Skill: "notes"}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Scenarios, 1)
	assert.True(t, list.Msg.Scenarios[0].Executes)
	skills, err := w.ListSkills(ctx, connect.NewRequest(&pb.ListSkillsRequest{Workspace: dir}))
	require.NoError(t, err)
	for _, s := range skills.Msg.Skills {
		if s.Name == "notes" {
			assert.Equal(t, int32(1), s.Scenarios)
		}
	}

	tried, err := w.TrySkill(ctx, connect.NewRequest(&pb.TrySkillRequest{Workspace: dir, Skill: "notes", Prompt: "Say hello"}))
	require.NoError(t, err)
	assert.False(t, tried.Msg.Triggered)
	assert.Equal(t, "Hello.", tried.Msg.Output)
	assert.Equal(t, "INVALID_ARGUMENT", reasonCode(t, unary(w.TrySkill, &pb.TrySkillRequest{Workspace: dir, Skill: "notes"})))

	// The scenario runs as an eval: the agent didn't use the skill.
	stream, err := w.RunEval(ctx, connect.NewRequest(&pb.RunEvalRequest{Workspace: dir, Name: "skill:notes"}))
	require.NoError(t, err)
	var finished *pb.EvalRun
	for stream.Receive() {
		if f := stream.Msg().GetFinished(); f != nil {
			finished = f
		}
	}
	require.NoError(t, stream.Err())
	require.NotNil(t, finished)
	assert.Equal(t, "skill:notes", finished.Eval)
	assert.Equal(t, "failed", finished.Status)
	runs, err := w.ListEvalRuns(ctx, connect.NewRequest(&pb.ListEvalRunsRequest{Workspace: dir, Name: "skill:notes"}))
	require.NoError(t, err)
	assert.Len(t, runs.Msg.Runs, 1)

	require.NoError(t, unary(w.DeleteSkillScenario, &pb.DeleteSkillScenarioRequest{Workspace: dir, Skill: "notes", Name: "since-tag"}))
	assert.Equal(t, "SCENARIO_NOT_FOUND", reason(t, unary(w.DeleteSkillScenario, &pb.DeleteSkillScenarioRequest{Workspace: dir, Skill: "notes", Name: "since-tag"})))
}

// reasonCode is a failed call's Connect code, written as its name.
func reasonCode(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	return map[connect.Code]string{connect.CodeInvalidArgument: "INVALID_ARGUMENT"}[connect.CodeOf(err)]
}
