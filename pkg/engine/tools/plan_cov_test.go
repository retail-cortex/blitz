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

package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Outside a turn there is no plan state: not planning, nothing approved.
func TestNilPlanGate(t *testing.T) {
	var g *PlanGate
	assert.False(t, g.Planning())
	_, _, _, ok := g.Take()
	assert.False(t, ok)
}

// Entering plan mode twice says so; nobody can review in an unattended run.
func TestEnterPlanModeAgain(t *testing.T) {
	hooks := NewHooks(Policy{})
	hooks.SetUserPrompter(func(context.Context, string, []string) (string, error) { return "", nil })
	enter := toolOf(t)(NewEnterPlanModeTool(hooks))
	out, _ := enter.Run(planContext(context.Background(), NewPlanGate(true)), map[string]any{})
	assert.Contains(t, out["message"], "already in plan mode")
	unattended := Unattended(context.Background(), func(context.Context, api.ApprovalRequest) (api.Decision, error) {
		return api.DecisionDeny, nil
	})
	out, _ = enter.Run(planContext(unattended, NewPlanGate(false)), map[string]any{})
	assert.NotEmpty(t, errOf(out))
}

// exit_plan_mode refuses an empty plan, answers for itself when no one can
// review, reports a failed review, and still approves a plan it can't save.
func TestExitPlanModeEdges(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	reviewErr := errors.New("prompt closed")
	failing := NewHooks(Policy{})
	failing.SetUserPrompter(func(context.Context, string, []string) (string, error) { return "", reviewErr })
	plan := map[string]any{"plan": "1. do it"}

	out, _ := toolOf(t)(NewExitPlanModeTool(ws, failing)).Run(planContext(context.Background(), NewPlanGate(true)), map[string]any{"plan": "  "})
	assert.Equal(t, "the plan is empty", errOf(out))

	out, _ = toolOf(t)(NewExitPlanModeTool(ws, failing)).Run(planContext(context.Background(), NewPlanGate(true)), plan)
	assert.Contains(t, errOf(out), "prompt closed")

	dontAsk := NewHooks(Policy{Mode: api.ModeDontAsk})
	out, _ = toolOf(t)(NewExitPlanModeTool(ws, dontAsk)).Run(planContext(context.Background(), NewPlanGate(true)), plan)
	assert.Equal(t, false, out["approved"])
	assert.Contains(t, out["message"], "No one can review")

	// .blitz is a file: the plan can't be saved, but is approved.
	writeFile(t, filepath.Join(dir, ".blitz"), "x")
	approving := NewHooks(Policy{})
	approving.SetUserPrompter(func(_ context.Context, _ string, options []string) (string, error) { return options[0], nil })
	g := NewPlanGate(true)
	out, _ = toolOf(t)(NewExitPlanModeTool(ws, approving)).Run(planContext(context.Background(), g), plan)
	assert.Equal(t, true, out["approved"])
	assert.NotContains(t, out, "plan_file")
	_, _, _, ok := g.Take()
	assert.True(t, ok)
}

// A plan from a session without an id is saved as plan-<n>; a plans
// directory that can't be written to is an error.
func TestSavePlan(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	path, err := savePlan(ws, "", "p")
	require.NoError(t, err)
	assert.Equal(t, ".blitz/plans/plan-1.md", path)

	plans := filepath.Join(dir, ".blitz", "plans")
	require.NoError(t, os.Chmod(plans, 0o555))
	t.Cleanup(func() { os.Chmod(plans, 0o755) })
	_, err = savePlan(ws, "s", "p")
	assert.Error(t, err)
}

// The todo list is bounded.
func TestTodoToolTooManyItems(t *testing.T) {
	items := make([]any, maxTodoItems+1)
	for i := range items {
		items[i] = map[string]any{"content": "x", "status": "pending"}
	}
	out := runTool(t, toolOf(t)(NewTodoTool()), map[string]any{"items": items})
	assert.Contains(t, errOf(out), "at most")
}
