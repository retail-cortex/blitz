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
	"regexp"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// PlanGate is one turn's plan state, shared by the plan tools and the
// front end's workspace: whether the agent is planning (only reading), and
// what the user decided about its plan.
type PlanGate struct {
	mu       sync.Mutex
	planning bool
	approved bool
	mode     api.PermissionMode // the mode to carry the plan out in
	plan     string
	path     string // where the approved plan was saved
}

// NewPlanGate starts a turn's plan state; planning is whether it plans
// first.
func NewPlanGate(planning bool) *PlanGate { return &PlanGate{planning: planning} }

// Planning reports whether the agent is planning: tools that change
// anything are refused.
func (g *PlanGate) Planning() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.planning
}

// Take returns the approved plan, the file it was saved to and the mode to
// carry it out in (ok is false when none was approved), and clears the
// approval, so each plan is carried out once.
func (g *PlanGate) Take() (plan, path string, mode api.PermissionMode, ok bool) {
	if g == nil {
		return "", "", "", false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	plan, path, mode, ok = g.plan, g.path, g.mode, g.approved
	g.approved = false
	return plan, path, mode, ok
}

type planGateKey struct{}

// WithPlanGate gives a turn its plan state.
func WithPlanGate(ctx context.Context, g *PlanGate) context.Context {
	return context.WithValue(ctx, planGateKey{}, g)
}

// PlanGateFrom returns the turn's plan state (nil outside a turn).
func PlanGateFrom(ctx context.Context) *PlanGate {
	g, _ := ctx.Value(planGateKey{}).(*PlanGate)
	return g
}

// EnterPlanModeInput is enter_plan_mode's argument.
type EnterPlanModeInput struct {
	Reason string `json:"reason,omitempty" jsonschema:"Why this task needs a plan first"`
}

// NewEnterPlanModeTool lets the agent choose to plan before changing
// anything ([blitz] plan_review = "agent-decides").
func NewEnterPlanModeTool(hooks *Hooks) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name: "enter_plan_mode",
			Description: "Switch to plan mode before a non-trivial or risky change: from now on you may only read and search. " +
				"Investigate, then call exit_plan_mode with your plan for the user to approve. Skip this for small, clear tasks.",
		},
		func(ctx agent.Context, _ EnterPlanModeInput) (map[string]any, error) {
			g := PlanGateFrom(ctx)
			if g == nil || isUnattended(ctx) || hooks.Mode() == api.ModeDontAsk || hooks.userPrompter() == nil {
				return map[string]any{"error": "no one can review a plan now: go ahead with the task yourself"}, nil
			}
			if g.Planning() {
				return map[string]any{"message": "You are already in plan mode."}, nil
			}
			g.mu.Lock()
			g.planning = true
			g.mu.Unlock()
			return map[string]any{"message": "Plan mode is on: investigate, then call exit_plan_mode with the plan."}, nil
		},
	)
}

// ExitPlanModeInput is exit_plan_mode's argument.
type ExitPlanModeInput struct {
	Plan string `json:"plan" jsonschema:"The complete plan in Markdown: objective, numbered steps naming files and functions, risks, and how to verify"`
}

// NewExitPlanModeTool shows the agent's plan to the user, who carries it
// out (in the default or accept-edits mode), asks for changes, or keeps
// planning. An approved plan is saved under .blitz/plans in the workspace
// and ends the planning: the workspace then asks the agent to carry it out.
func NewExitPlanModeTool(ws *Workspace, hooks *Hooks) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name: "exit_plan_mode",
			Description: "In plan mode, present your finished plan for the user to approve. If they approve, end your turn " +
				"with one short sentence: you'll then be asked to carry it out. If they give feedback, revise the plan and call this again.",
		},
		func(ctx agent.Context, in ExitPlanModeInput) (map[string]any, error) {
			g := PlanGateFrom(ctx)
			if !g.Planning() {
				return map[string]any{"error": "you aren't in plan mode; go ahead with the task"}, nil
			}
			plan := strings.TrimSpace(in.Plan)
			if plan == "" {
				return map[string]any{"error": "the plan is empty"}, nil
			}
			noReview := map[string]any{"approved": false, "message": "No one can review the plan now: end your turn with the plan as your answer."}
			if isUnattended(ctx) || hooks.Mode() == api.ModeDontAsk {
				return noReview, nil
			}
			prompter := hooks.userPrompter()
			if prompter == nil {
				return noReview, nil
			}
			execute, executeEdits, keep := i18n.T("plan.review.execute"), i18n.T("plan.review.execute_edits"), i18n.T("plan.review.keep")
			hooks.Notify(ctx, "question", i18n.T("plan.review.title"))
			answer, err := prompter(ctx, i18n.T("plan.review.title")+"\n\n"+plan+"\n\n"+i18n.T("plan.review.ask"), []string{execute, executeEdits, keep})
			if err != nil {
				return map[string]any{"error": fmt.Sprintf("the review failed: %v", err)}, nil
			}
			switch answer = strings.TrimSpace(answer); answer {
			case execute, executeEdits:
				mode := api.ModeDefault
				if answer == executeEdits {
					mode = api.ModeAcceptEdits
				}
				path, err := savePlan(ws, ctx.SessionID(), plan)
				g.mu.Lock()
				g.approved, g.planning, g.mode, g.plan, g.path = true, false, mode, plan, path
				g.mu.Unlock()
				out := map[string]any{"approved": true, "message": "The user approved the plan. End your turn now with one short sentence; you'll be asked to carry it out next."}
				if err == nil {
					out["plan_file"] = path
				}
				return out, nil
			case keep, "":
				return map[string]any{"approved": false, "message": "The user wants to keep planning. End your turn and wait for their next message."}, nil
			default:
				return map[string]any{"approved": false, "feedback": answer, "message": "The user wants changes: revise the plan with this feedback, then call exit_plan_mode again."}, nil
			}
		},
	)
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// savePlan writes an approved plan to .blitz/plans/<session>-<n>.md in the
// workspace and returns its workspace-relative path.
func savePlan(ws *Workspace, session, plan string) (string, error) {
	dir := filepath.Join(ws.Dir(), ".blitz", "plans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := unsafeName.ReplaceAllString(session, "_")
	if base == "" {
		base = "plan"
	}
	for n := 1; ; n++ {
		name := fmt.Sprintf("%s-%d.md", base, n)
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.WriteString(plan + "\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(f.Name()) // no half-written plan to point at
			return "", err
		}
		return filepath.ToSlash(filepath.Join(".blitz", "plans", name)), nil
	}
}
