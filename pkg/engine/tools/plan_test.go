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
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"

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
	if len(items) != 3 || items[0].Status != TaskDone || items[1].Status != TaskInProgress || items[2].Status != TaskPending || items[2].Content != "add a test" {
		t.Fatalf("items %+v", items)
	}
	if fmt.Sprint(out["done"], "/", out["total"]) != "1/3" || out["note"] != nil {
		t.Errorf("counts %v", out)
	}
	two := runTool(t, td, map[string]any{"items": []any{
		map[string]any{"content": "a", "status": "in_progress"}, map[string]any{"content": "b", "status": "in_progress"},
	}})
	if two["note"] == nil {
		t.Error("two items in progress without a note")
	}
	for _, bad := range []map[string]any{
		{"items": []any{map[string]any{"content": "", "status": "done"}}},
		{"items": []any{map[string]any{"content": "x", "status": "someday"}}},
	} {
		if errOf(runTool(t, td, bad)) == "" {
			t.Errorf("accepted %v", bad)
		}
	}
	if TodoItems(map[string]any{"answer": "x"}) != nil {
		t.Error("items read from another tool's result")
	}
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
	if out, _ := exit.Run(planContext(context.Background(), NewPlanGate(false)), plan); errOf(out) == "" {
		t.Fatalf("outside plan mode: %v", out)
	}

	// Approved: saved, and the gate says how to carry it out.
	g := NewPlanGate(true)
	out, _ := exit.Run(planContext(context.Background(), g), plan)
	if out["approved"] != true || out["plan_file"] != ".blitz/plans/s_1-1.md" || !strings.Contains(asked, "2. Test it") {
		t.Fatalf("approve: %v (asked %q)", out, asked)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".blitz", "plans", "s_1-1.md")); !strings.Contains(string(b), "1. Add the flag") {
		t.Errorf("saved plan %q", b)
	}
	if g.Planning() {
		t.Error("still planning after approval")
	}
	if _, path, mode, ok := g.Take(); !ok || mode != api.ModeDefault || path != ".blitz/plans/s_1-1.md" {
		t.Errorf("take: %v %v %v", path, mode, ok)
	}
	if _, _, _, ok := g.Take(); ok {
		t.Error("an approval was taken twice")
	}

	answer = "edits"
	g = NewPlanGate(true)
	exit.Run(planContext(context.Background(), g), plan)
	if _, path, mode, _ := g.Take(); mode != api.ModeAcceptEdits || path != ".blitz/plans/s_1-2.md" {
		t.Errorf("accept-edits: %v %v", mode, path)
	}

	answer = "keep"
	g = NewPlanGate(true)
	if out, _ := exit.Run(planContext(context.Background(), g), plan); out["approved"] != false || !g.Planning() {
		t.Errorf("keep planning: %v", out)
	}

	answer = "skip step 2"
	if out, _ := exit.Run(planContext(context.Background(), g), plan); out["feedback"] != "skip step 2" || !g.Planning() {
		t.Errorf("feedback: %v", out)
	}

	// Nobody to review: the agent answers with the plan.
	quiet := toolOf(t)(NewExitPlanModeTool(ws, NewHooks(Policy{})))
	if out, _ := quiet.Run(planContext(context.Background(), NewPlanGate(true)), plan); out["approved"] != false || !strings.Contains(out["message"].(string), "No one can review") {
		t.Errorf("no reviewer: %v", out)
	}
}

func TestEnterPlanMode(t *testing.T) {
	hooks := NewHooks(Policy{})
	enter := toolOf(t)(NewEnterPlanModeTool(hooks))
	g := NewPlanGate(false)
	if out, _ := enter.Run(planContext(context.Background(), g), map[string]any{}); errOf(out) == "" || g.Planning() {
		t.Fatalf("without a reviewer: %v", out)
	}
	hooks.SetUserPrompter(func(context.Context, string, []string) (string, error) { return "", nil })
	if out, _ := enter.Run(planContext(context.Background(), g), map[string]any{"reason": "risky"}); errOf(out) != "" || !g.Planning() {
		t.Fatalf("enter: %v", out)
	}
}
