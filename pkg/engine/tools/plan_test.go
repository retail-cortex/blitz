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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
)

// stubSession is a session with only an ID.
type stubSession struct {
	session.Session
	id string
}

func (s stubSession) ID() string { return s.id }

type sessionIC struct {
	*mockIC
	sess session.Session
}

func (s sessionIC) Session() session.Session { return s.sess }

// planContext is a tool context in session s with the plan state g.
func planContext(ctx context.Context, g *PlanGate) agent.Context {
	ic := sessionIC{mockIC: &mockIC{ctx: WithPlanGate(ctx, g)}, sess: stubSession{id: "s/1"}}
	return agent.NewToolContext(ic, "call", &session.EventActions{}, nil)
}

func TestTodoTool(t *testing.T) {
	td := toolOf(t)(NewTodoTool())
	out := runTool(t, td, map[string]any{"items": []any{
		map[string]any{"content": "read the code", "status": "completed"},
		map[string]any{"content": "write the fix", "status": "in-progress"},
		map[string]any{"content": " add a test ", "status": ""},
	}})
	items := TodoItems(out)
	require.Len(t, items, 3, "items %+v", items)
	require.Equal(t, TaskDone, items[0].Status, "items %+v", items)
	require.Equal(t, TaskInProgress, items[1].Status, "items %+v", items)
	require.Equal(t, TaskPending, items[2].Status, "items %+v", items)
	require.Equal(t, "add a test", items[2].Content, "items %+v", items)
	assert.Equal(t, "1/3", fmt.Sprint(out["done"], "/", out["total"]), "counts %v", out)
	assert.Nil(t, out["note"], "counts %v", out)
	two := runTool(t, td, map[string]any{"items": []any{
		map[string]any{"content": "a", "status": "in_progress"}, map[string]any{"content": "b", "status": "in_progress"},
	}})
	assert.NotNil(t, two["note"], "two items in progress without a note")
	for _, bad := range []map[string]any{
		{"items": []any{map[string]any{"content": "", "status": "done"}}},
		{"items": []any{map[string]any{"content": "x", "status": "someday"}}},
	} {
		assert.NotEqual(t, "", errOf(runTool(t, td, bad)), "accepted %v", bad)
	}
	assert.Nil(t, TodoItems(map[string]any{"answer": "x"}), "items read from another tool's result")
}

func TestExitPlanMode(t *testing.T) {
	ws, dir := newTestWorkspace(t)
	hooks := NewHooks(Policy{})
	var asked string
	answer := ""
	hooks.SetUserPrompter(func(_ context.Context, q string, options []string) (string, error) {
		asked = q
		if answer == "" {
			return options[0], nil
		}
		if answer == "edits" {
			return options[1], nil
		}
		if answer == "keep" {
			return options[2], nil
		}
		return answer, nil
	})
	exit := toolOf(t)(NewExitPlanModeTool(ws, hooks))
	plan := map[string]any{"plan": "1. Add the flag\n2. Test it"}

	// Not planning: refused.
	out, _ := exit.Run(planContext(context.Background(), NewPlanGate(false)), plan)
	require.NotEqual(t, "", errOf(out), "outside plan mode: %v", out)

	// Approved: saved, and the gate says how to carry it out.
	g := NewPlanGate(true)
	out, _ = exit.Run(planContext(context.Background(), g), plan)
	require.Equal(t, true, out["approved"], "approve: %v (asked %q)", out, asked)
	require.Equal(t, ".blitz/plans/s_1-1.md", out["plan_file"], "approve: %v (asked %q)", out, asked)
	require.Contains(t, asked, "2. Test it", "approve: %v (asked %q)", out, asked)
	b, _ := os.ReadFile(filepath.Join(dir, ".blitz", "plans", "s_1-1.md"))
	assert.Contains(t, string(b), "1. Add the flag", "saved plan %q", b)
	assert.False(t, g.Planning(), "still planning after approval")
	_, path, mode, ok := g.Take()
	assert.True(t, ok, "take: %v %v %v", path, mode, ok)
	assert.Equal(t, api.ModeDefault, mode, "take: %v %v %v", path, mode, ok)
	assert.Equal(t, ".blitz/plans/s_1-1.md", path, "take: %v %v %v", path, mode, ok)
	_, _, _, ok = g.Take()
	assert.False(t, ok, "an approval was taken twice")

	answer = "edits"
	g = NewPlanGate(true)
	exit.Run(planContext(context.Background(), g), plan)
	_, path, mode, _ = g.Take()
	assert.Equal(t, api.ModeAcceptEdits, mode, "accept-edits: %v %v", mode, path)
	assert.Equal(t, ".blitz/plans/s_1-2.md", path, "accept-edits: %v %v", mode, path)

	answer = "keep"
	g = NewPlanGate(true)
	out, _ = exit.Run(planContext(context.Background(), g), plan)
	assert.Equal(t, false, out["approved"], "keep planning: %v", out)
	assert.True(t, g.Planning(), "keep planning: %v", out)

	answer = "skip step 2"
	out, _ = exit.Run(planContext(context.Background(), g), plan)
	assert.Equal(t, "skip step 2", out["feedback"], "feedback: %v", out)
	assert.True(t, g.Planning(), "feedback: %v", out)

	// Nobody to review: the agent answers with the plan.
	quiet := toolOf(t)(NewExitPlanModeTool(ws, NewHooks(Policy{})))
	out, _ = quiet.Run(planContext(context.Background(), NewPlanGate(true)), plan)
	assert.Equal(t, false, out["approved"], "no reviewer: %v", out)
	assert.Contains(t, out["message"].(string), "No one can review", "no reviewer: %v", out)
}

func TestEnterPlanMode(t *testing.T) {
	hooks := NewHooks(Policy{})
	enter := toolOf(t)(NewEnterPlanModeTool(hooks))
	g := NewPlanGate(false)
	out, _ := enter.Run(planContext(context.Background(), g), map[string]any{})
	require.NotEqual(t, "", errOf(out), "without a reviewer: %v", out)
	require.False(t, g.Planning(), "without a reviewer: %v", out)
	hooks.SetUserPrompter(func(context.Context, string, []string) (string, error) { return "", nil })
	out, _ = enter.Run(planContext(context.Background(), g), map[string]any{"reason": "risky"})
	require.Equal(t, "", errOf(out), "enter: %v", out)
	require.True(t, g.Planning(), "enter: %v", out)
}
