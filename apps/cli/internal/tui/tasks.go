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

package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// Background tasks in the REPL (spec_background_agents_032 BGA-30).

// TaskLine is a task on one line: "[task-3] qa · running · 1m20s · $0.02 —
// review the cart".
func TaskLine(t api.TaskInfo) string {
	state := i18n.T("tasks.state." + t.State)
	cost := ""
	if t.Usage.CostUSD > 0 {
		cost = fmt.Sprintf(" · $%.2f", t.Usage.CostUSD)
	}
	return fmt.Sprintf("[%s] %s · %s · %s%s — %s", t.ID, t.Agent, state, t.Runtime().Round(time.Second), cost, textutil.Ellipsize(t.Prompt, 60))
}

// cmdTasks is /tasks: the list, one task's events and result (show), or
// stopping one (stop).
func cmdTasks(args []string, app *App) {
	b := app.Workspace
	if len(args) == 0 {
		tasks := b.ListTasks()
		if len(tasks) == 0 {
			fmt.Printf("%s%s%s\n", Dim, i18n.T("tasks.none"), Reset)
			return
		}
		fmt.Printf("\n%s%s%s\n", Bold, i18n.T("tasks.title"), Reset)
		for _, t := range tasks {
			fmt.Printf("  %s\n", safe(TaskLine(t)))
		}
		fmt.Println()
		return
	}
	if len(args) != 2 || args[0] != "show" && args[0] != "stop" {
		fmt.Printf("%s%s%s\n", Red, i18n.T("tasks.usage"), Reset)
		return
	}
	if args[0] == "stop" {
		t, err := b.StopTask(args[1])
		if err != nil {
			fmt.Printf("%s%s%s\n", Red, err, Reset)
			return
		}
		fmt.Println(safe(TaskLine(t)))
		return
	}
	t, events, err := b.Task(args[1])
	if err != nil {
		fmt.Printf("%s%s%s\n", Red, err, Reset)
		return
	}
	fmt.Printf("\n%s%s%s\n", Bold, safe(TaskLine(t)), Reset)
	if t.Worktree != "" {
		fmt.Printf("  %s%s%s\n", Dim, safe(i18n.T("tasks.worktree", "path", t.Worktree, "branch", t.Branch)), Reset)
	}
	for _, e := range events {
		fmt.Printf("  %s%s%s\n", Dim, safe(e), Reset)
	}
	switch {
	case t.Result != "":
		fmt.Printf("\n%s\n", safe(t.Result))
	case t.Error != "":
		fmt.Printf("\n%s%s%s\n", Red, safe(t.Error), Reset)
	}
	fmt.Println()
}

// announceTasks says, at the prompt, which background tasks have ended
// since it last looked: a line each, dim.
func announceTasks(app *App) {
	tasks := app.Workspace.ListTasks()
	if len(tasks) == 0 {
		return
	}
	if app.announced == nil {
		app.announced = map[string]bool{}
	}
	for _, t := range tasks {
		if t.Active() || app.announced[t.ID] {
			continue
		}
		app.announced[t.ID] = true
		summary := t.Error
		if t.State == api.TaskDone {
			summary, _, _ = strings.Cut(t.Result, "\n")
		}
		fmt.Printf("%s%s%s\n", Dim, safe(i18n.T("tasks.ended", "id", t.ID, "agent", t.Agent, "state", i18n.T("tasks.state."+t.State), "took", t.Runtime().Round(time.Second).String(), "summary", textutil.Ellipsize(summary, 100))), Reset)
	}
}

// answerTasks asks, at the prompt, the approval requests and questions
// background tasks are waiting on, each labelled with who asks, so a
// turn's own questions are never interrupted (BGA-41). Unanswered ones
// (Ctrl+C) stay waiting; /tasks shows them.
func answerTasks(ctx context.Context, app *App) {
	if app.Input == nil {
		return
	}
	approve, ask := NewApprover(app.Input, app.DiffLines), NewUserPrompter(app.Input)
	for _, r := range app.Workspace.PendingTaskRequests() {
		fmt.Printf("\n%s%s%s\n", Cyan+Bold, safe(i18n.T("tasks.asks", "who", r.Label())), Reset)
		var err error
		if r.Approval != nil {
			var d api.Decision
			if d, err = approve(ctx, *r.Approval); err == nil {
				err = app.Workspace.AnswerTaskRequest(r.ID, d, "")
			}
		} else {
			var answer string
			if answer, err = ask(ctx, r.Question, r.Options); err == nil {
				err = app.Workspace.AnswerTaskRequest(r.ID, 0, answer)
			}
		}
		if err != nil {
			fmt.Printf("%s%s%s\n", Dim, safe(err.Error()), Reset)
			return
		}
	}
}
