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
	"os"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// ExitPrompt configures ConfirmExit.
type ExitPrompt struct {
	// CanPrompt is false when stdin can't answer (EOF, not a terminal); running
	// processes are then killed without asking.
	CanPrompt bool
	// AllowCancel offers "cancel" to return to the REPL instead of exiting.
	AllowCancel bool
	// Tasks are the background tasks to account for too (nil: none).
	Tasks TaskControl
}

// TaskControl lists and stops background tasks: the workspace
// (api.Backend).
type TaskControl interface {
	ListTasks() []api.TaskInfo
	StopTask(id string) (api.TaskInfo, error)
}

// runningTasks are tc's tasks still running.
func runningTasks(tc TaskControl) []api.TaskInfo {
	if tc == nil {
		return nil
	}
	var out []api.TaskInfo
	for _, t := range tc.ListTasks() {
		if t.Active() {
			out = append(out, t)
		}
	}
	return out
}

// ConfirmExit decides whether Blitz may exit while background processes
// or tasks are running. Nothing is left running either way: the user chooses to kill
// them now or wait for them to finish, and a further Ctrl+C (or EOF) at the
// prompt or while waiting force-quits, killing them. Returns false only when
// the user cancels the exit.
func ConfirmExit(ctx context.Context, in Input, pm api.Processes, interrupts <-chan os.Signal, opts ExitPrompt) bool {
	var running []api.ProcessInfo
	if pm != nil {
		running = pm.Running()
	}
	tasks := runningTasks(opts.Tasks)
	if len(running) == 0 && len(tasks) == 0 {
		return true
	}
	all := work{pm, opts.Tasks}

	if len(running) > 0 {
		fmt.Printf("\n%s!  %s%s\n", Yellow+Bold, i18n.N("exit.running", len(running)), Reset)
		for _, p := range running {
			fmt.Printf("   [%d] %s %s(%ds)%s\n", p.ID, safe(textutil.Ellipsize(p.Command, 70)), Dim, p.RuntimeMs/1000, Reset)
		}
	}
	if len(tasks) > 0 {
		fmt.Printf("\n%s!  %s%s\n", Yellow+Bold, i18n.N("exit.tasks_running", len(tasks)), Reset)
		for _, t := range tasks {
			fmt.Printf("   [%s] %s: %s %s(%ds)%s\n", t.ID, safe(t.Agent), safe(textutil.Ellipsize(t.Prompt, 60)), Dim, int(t.Runtime().Seconds()), Reset)
		}
	}
	if !opts.CanPrompt {
		all.kill()
		return true
	}

	choices := i18n.T("exit.choices")
	if opts.AllowCancel {
		choices += i18n.T("exit.choices_cancel")
	}
	askCtx, stopAsk := cancelOnSignal(ctx, interrupts)
	answer, err := in.Ask(askCtx, fmt.Sprintf("   %s? %s(%s)%s ", choices, Dim, i18n.T("exit.force_hint"), Reset))
	stopAsk()
	if err != nil {
		fmt.Printf("\n%s✗ %s%s\n", Red, i18n.T("exit.force_quit"), Reset)
		all.kill()
		return true
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "k", "kill":
		all.kill()
		return true
	case "w", "wait":
		fmt.Printf("%s%s%s\n", Cyan, i18n.T("exit.waiting"), Reset)
		waitCtx, stopWait := cancelOnSignal(ctx, interrupts)
		err := all.wait(waitCtx)
		stopWait()
		if err != nil {
			fmt.Printf("\n%s✗ %s%s\n", Red, i18n.T("exit.force_quit"), Reset)
			all.kill()
		}
		return true
	default:
		if opts.AllowCancel {
			fmt.Println("   " + i18n.T("exit.cancelled"))
			return false
		}
		all.kill()
		return true
	}
}

// work is what runs in the background: processes and tasks.
type work struct {
	pm    api.Processes
	tasks TaskControl
}

// kill stops them all and says how many.
func (w work) kill() {
	if w.pm != nil {
		n := len(w.pm.Running())
		w.pm.Shutdown()
		if n > 0 {
			fmt.Printf("%s%s%s\n", Yellow, i18n.N("exit.stopped", n), Reset)
		}
	}
	if running := runningTasks(w.tasks); len(running) > 0 {
		for _, t := range running {
			_, _ = w.tasks.StopTask(t.ID)
		}
		fmt.Printf("%s%s%s\n", Yellow, i18n.N("exit.tasks_stopped", len(running)), Reset)
	}
}

// taskPoll is how often wait asks whether the tasks have ended.
var taskPoll = 500 * time.Millisecond

// wait waits until they have all ended, or ctx ends.
func (w work) wait(ctx context.Context) error {
	if w.pm != nil {
		if err := w.pm.WaitAll(ctx); err != nil {
			return err
		}
	}
	for len(runningTasks(w.tasks)) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(taskPoll):
		}
	}
	return nil
}

// StdinIsTerminal reports whether stdin is interactive.
func StdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
