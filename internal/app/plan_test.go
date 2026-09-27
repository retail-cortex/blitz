package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/internal/config"
	"github.com/retail-cortex/blitz/internal/session"
	"github.com/retail-cortex/blitz/internal/tools"
	"google.golang.org/genai"
)

// planWorkspace opens a workspace whose user answers plan reviews with the
// option at index choice (or with choice's text when it is a string).
func planWorkspace(t *testing.T, mutate func(*config.Config), choice any, replies ...*genai.Content) (*Workspace, string) {
	t.Helper()
	w, _ := openTestWith(t, mutate, replies...)
	w.SetUI(func(context.Context, tools.ApprovalRequest) (tools.Decision, error) { return tools.DecisionOnce, nil },
		func(_ context.Context, _ string, options []string) (string, error) {
			if i, ok := choice.(int); ok {
				return options[i], nil
			}
			return choice.(string), nil
		})
	s, err := w.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	return w, s.ID
}

func exitPlan(plan string) *genai.Content {
	return toolCall("exit_plan_mode", map[string]any{"plan": plan})
}

// run collects a turn's tool results by name and its task lists.
func runCollect(t *testing.T, w *Workspace, id string, turn Turn) (map[string][]map[string]any, [][]Task) {
	t.Helper()
	results := map[string][]map[string]any{}
	var tasks [][]Task
	if _, err := w.Run(context.Background(), id, turn, func(e Event) {
		if e.ToolResult != nil {
			results[e.ToolResult.Name] = append(results[e.ToolResult.Name], e.ToolResult.Result)
		}
		if e.Tasks != nil {
			tasks = append(tasks, e.Tasks)
		}
	}); err != nil {
		t.Fatal(err)
	}
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
	results, tasks := runCollect(t, w, id, Turn{Text: "add notes", Plan: true})
	if msg, _ := results["create_file"][0]["error"].(string); !strings.Contains(msg, "plan mode") || exists(w, "early.txt") {
		t.Errorf("a write went through while planning: %v", results["create_file"][0])
	}
	if !exists(w, "notes.txt") {
		t.Fatalf("the approved plan wasn't carried out: %v", results)
	}
	if len(tasks) != 1 || tasks[0][0].Status != "in_progress" {
		t.Errorf("tasks %+v", tasks)
	}
	if b, _ := os.ReadFile(filepath.Join(w.Dir(), ".blitz", "plans", id+"-1.md")); !strings.Contains(string(b), "Create notes.txt") {
		t.Errorf("the plan wasn't saved: %q", b)
	}
	var kinds []string
	for _, m := range w.storage.Active().Messages {
		kinds = append(kinds, m.Kind)
		if m.Kind == session.KindPlan && !strings.Contains(m.Content, ".blitz/plans/") {
			t.Errorf("go-ahead %q", m.Content)
		}
	}
	if strings.Join(kinds, ",") != ",plan," {
		t.Errorf("transcript kinds %v", kinds)
	}
	if out := w.storage.Active().Messages[2].Content; out != "Plan approved.\n\ndone" {
		t.Errorf("the runs' output isn't separated: %q", out)
	}
	// The go-ahead isn't a prompt to rewind to.
	if points, _ := w.RewindPoints(); len(points) != 1 {
		t.Errorf("rewind points %+v", points)
	}
}

// In plan permission mode, approving leaves plan mode for the mode chosen.
func TestApprovingInPlanModeSwitchesMode(t *testing.T) {
	w, id := planWorkspace(t, func(c *config.Config) { c.Blitz.PermissionMode = "plan" }, 1,
		exitPlan("1. Create a.txt"), text("ok"),
		toolCall("create_file", map[string]any{"path": "a.txt", "content": "a"}), text("done"))
	runCollect(t, w, id, Turn{Text: "make a"})
	if got := w.Settings().PermissionMode; got != "accept-edits" || !exists(w, "a.txt") {
		t.Fatalf("mode %q, a.txt %v", got, exists(w, "a.txt"))
	}
}

func TestKeepPlanningAndRevising(t *testing.T) {
	w, id := planWorkspace(t, nil, 2, exitPlan("1. Something"), text("Waiting."))
	results, _ := runCollect(t, w, id, Turn{Text: "think", Plan: true})
	if results["exit_plan_mode"][0]["approved"] != false || len(w.storage.Active().Messages) != 2 {
		t.Fatalf("keep planning: %v", results)
	}
	w2, id2 := planWorkspace(t, nil, "use tabs", exitPlan("1. Spaces"), exitPlan("1. Tabs"), text("?"))
	results, _ = runCollect(t, w2, id2, Turn{Text: "format", Plan: true})
	if got := results["exit_plan_mode"]; len(got) != 2 || got[0]["feedback"] != "use tabs" {
		t.Fatalf("revise: %v", got)
	}
}

func TestPlanReviewPolicies(t *testing.T) {
	always := func(c *config.Config) { c.Blitz.PlanReview = config.PlanReviewAlways }
	w, id := planWorkspace(t, always, 0,
		toolCall("create_file", map[string]any{"path": "x.txt", "content": "x"}),
		exitPlan("1. Create x.txt"), text("ok"),
		toolCall("create_file", map[string]any{"path": "x.txt", "content": "x"}), text("done"))
	results, _ := runCollect(t, w, id, Turn{Text: "make x"})
	if msg, _ := results["create_file"][0]["error"].(string); msg == "" || !exists(w, "x.txt") {
		t.Fatalf("always: %v", results["create_file"])
	}

	// agent-decides (the default): the agent may enter plan mode itself.
	w2, id2 := planWorkspace(t, nil, 0,
		toolCall("enter_plan_mode", map[string]any{"reason": "risky"}),
		toolCall("create_file", map[string]any{"path": "y.txt", "content": "y"}),
		exitPlan("1. Create y.txt"), text("ok"),
		toolCall("create_file", map[string]any{"path": "y.txt", "content": "y"}), text("done"))
	results, _ = runCollect(t, w2, id2, Turn{Text: "make y"})
	if msg, _ := results["create_file"][0]["error"].(string); msg == "" || !exists(w2, "y.txt") {
		t.Fatalf("agent-decides: %v", results["create_file"])
	}

	// never: no enter_plan_mode tool.
	w3, _ := planWorkspace(t, func(c *config.Config) { c.Blitz.PlanReview = config.PlanReviewNever }, 0)
	for _, tl := range w3.tools.WorkflowTools() {
		if tl.Name() == "enter_plan_mode" {
			t.Error("enter_plan_mode offered with plan_review = never")
		}
	}
}
