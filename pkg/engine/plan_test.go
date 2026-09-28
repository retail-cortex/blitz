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

package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// planWorkspace opens a workspace whose user answers plan reviews with the
// option at index choice (or with choice's text when it is a string).
func planWorkspace(t *testing.T, mutate func(*config.Config), choice any, replies ...*genai.Content) (*Workspace, string) {
	t.Helper()
	w, _ := openTestWith(t, mutate, replies...)
	w.SetUI(func(context.Context, api.ApprovalRequest) (api.Decision, error) { return api.DecisionOnce, nil },
		func(_ context.Context, _ string, options []string) (string, error) {
			if i, ok := choice.(int); ok {
				return options[i], nil
			}
			return choice.(string), nil
		})
	s, err := w.NewSession()
	require.NoError(t, err)
	return w, s.ID
}

func exitPlan(plan string) *genai.Content {
	return toolCall("exit_plan_mode", map[string]any{"plan": plan})
}

// run collects a turn's tool results by name and its task lists.
func runCollect(t *testing.T, w *Workspace, id string, turn api.Turn) (map[string][]map[string]any, [][]api.Task) {
	t.Helper()
	results := map[string][]map[string]any{}
	var tasks [][]api.Task
	_, err := w.Run(context.Background(), id, turn, func(e api.Event) {
		if e.ToolResult != nil {
			results[e.ToolResult.Name] = append(results[e.ToolResult.Name], e.ToolResult.Result)
		}
		if e.Tasks != nil {
			tasks = append(tasks, e.Tasks)
		}
	})
	require.NoError(t, err)
	return results, tasks
}

func exists(w *Workspace, name string) bool {
	_, err := os.Stat(filepath.Join(w.Dir(), name))
	return err == nil
}

func TestApprovedPlanIsCarriedOut(t *testing.T) {
	w, id := planWorkspace(t, nil, 0,
		toolCall("create_file", map[string]any{"path": "early.txt", "content": "x"}), // refused while planning
		exitPlan("1. Create notes.txt"), text("Plan approved."),
		toolCall("todo", map[string]any{"items": []any{map[string]any{"content": "create notes.txt", "status": "in_progress"}}}),
		toolCall("create_file", map[string]any{"path": "notes.txt", "content": "hi\n"}), text("done"))
	results, tasks := runCollect(t, w, id, api.Turn{Text: "add notes", Plan: true})
	msg, _ := results["create_file"][0]["error"].(string)
	assert.Contains(t, msg, "plan mode", "a write went through while planning: %v", results["create_file"][0])
	assert.False(t, exists(w, "early.txt"), "a write went through while planning: %v", results["create_file"][0])
	require.True(t, exists(w, "notes.txt"), "the approved plan wasn't carried out: %v", results)
	assert.Len(t, tasks, 1, "tasks %+v", tasks)
	assert.Equal(t, "in_progress", tasks[0][0].Status, "tasks %+v", tasks)
	b, _ := os.ReadFile(filepath.Join(w.Dir(), ".blitz", "plans", id+"-1.md"))
	assert.Contains(t, string(b), "Create notes.txt", "the plan wasn't saved: %q", b)
	var kinds []string
	for _, m := range w.storage.Active().Messages {
		kinds = append(kinds, m.Kind)
		assert.False(t, m.Kind == session.KindPlan && !strings.Contains(m.Content, ".blitz/plans/"), "go-ahead %q", m.Content)
	}
	assert.Equal(t, ",plan,", strings.Join(kinds, ","), "transcript kinds %v", kinds)
	out := w.storage.Active().Messages[2].Content
	assert.Equal(t, "Plan approved.\n\ndone", out, "the runs' output isn't separated: %q", out)
	// The go-ahead isn't a prompt to rewind to.
	points, _ := w.RewindPoints()
	assert.Len(t, points, 1, "rewind points %+v", points)
}

// In plan permission mode, approving leaves plan mode for the mode chosen.
func TestApprovingInPlanModeSwitchesMode(t *testing.T) {
	w, id := planWorkspace(t, func(c *config.Config) { c.Blitz.PermissionMode = "plan" }, 1,
		exitPlan("1. Create a.txt"), text("ok"),
		toolCall("create_file", map[string]any{"path": "a.txt", "content": "a"}), text("done"))
	runCollect(t, w, id, api.Turn{Text: "make a"})
	got := w.Settings().PermissionMode
	require.Equal(t, "accept-edits", got, "mode %q, a.txt %v", got, exists(w, "a.txt"))
	require.True(t, exists(w, "a.txt"), "mode %q, a.txt %v", got, exists(w, "a.txt"))
}

func TestKeepPlanningAndRevising(t *testing.T) {
	w, id := planWorkspace(t, nil, 2, exitPlan("1. Something"), text("Waiting."))
	results, _ := runCollect(t, w, id, api.Turn{Text: "think", Plan: true})
	require.Equal(t, false, results["exit_plan_mode"][0]["approved"], "keep planning: %v", results)
	require.Len(t, w.storage.Active().Messages, 2, "keep planning: %v", results)
	w2, id2 := planWorkspace(t, nil, "use tabs", exitPlan("1. Spaces"), exitPlan("1. Tabs"), text("?"))
	results, _ = runCollect(t, w2, id2, api.Turn{Text: "format", Plan: true})
	got := results["exit_plan_mode"]
	require.Len(t, got, 2, "revise: %v", got)
	require.Equal(t, "use tabs", got[0]["feedback"], "revise: %v", got)
}

func TestPlanReviewPolicies(t *testing.T) {
	always := func(c *config.Config) { c.Blitz.PlanReview = config.PlanReviewAlways }
	w, id := planWorkspace(t, always, 0,
		toolCall("create_file", map[string]any{"path": "x.txt", "content": "x"}),
		exitPlan("1. Create x.txt"), text("ok"),
		toolCall("create_file", map[string]any{"path": "x.txt", "content": "x"}), text("done"))
	results, _ := runCollect(t, w, id, api.Turn{Text: "make x"})
	msg, _ := results["create_file"][0]["error"].(string)
	require.NotEqual(t, "", msg, "always: %v", results["create_file"])
	require.True(t, exists(w, "x.txt"), "always: %v", results["create_file"])

	// agent-decides (the default): the agent may enter plan mode itself.
	w2, id2 := planWorkspace(t, nil, 0,
		toolCall("enter_plan_mode", map[string]any{"reason": "risky"}),
		toolCall("create_file", map[string]any{"path": "y.txt", "content": "y"}),
		exitPlan("1. Create y.txt"), text("ok"),
		toolCall("create_file", map[string]any{"path": "y.txt", "content": "y"}), text("done"))
	results, _ = runCollect(t, w2, id2, api.Turn{Text: "make y"})
	msg, _ = results["create_file"][0]["error"].(string)
	require.NotEqual(t, "", msg, "agent-decides: %v", results["create_file"])
	require.True(t, exists(w2, "y.txt"), "agent-decides: %v", results["create_file"])

	// never: no enter_plan_mode tool.
	w3, _ := planWorkspace(t, func(c *config.Config) { c.Blitz.PlanReview = config.PlanReviewNever }, 0)
	for _, tl := range w3.tools.WorkflowTools() {
		assert.NotEqual(t, "enter_plan_mode", tl.Name(), "enter_plan_mode offered with plan_review = never")
	}
}
