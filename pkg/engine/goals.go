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
	"errors"
	"fmt"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// Goals (spec_parity_027 PAR-SES-30): a session may have a condition to
// work toward. After each turn a judge model reads the end of the
// conversation; while the goal isn't met, and isn't judged impossible, the
// agent is sent on, up to blitz.goal_max_continues times, within the turn's
// cost and time limits. Goals live as long as the workspace is open.

// SetGoal gives the active session a goal (replacing its goal, if any).
func (w *Workspace) SetGoal(condition string) (api.Goal, error) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return api.Goal{}, errors.New("the goal is empty")
	}
	id, err := w.activeID()
	if err != nil {
		return api.Goal{}, err
	}
	g := api.Goal{Condition: condition, Max: w.goalMax()}
	w.goalMu.Lock()
	defer w.goalMu.Unlock()
	if w.goals == nil {
		w.goals = map[string]api.Goal{}
	}
	w.goals[id] = g
	return g, nil
}

// Goal is the active session's goal (api.ErrNoGoal).
func (w *Workspace) Goal() (api.Goal, error) {
	id, err := w.activeID()
	if err != nil {
		return api.Goal{}, err
	}
	g, ok := w.goalOf(id)
	if !ok {
		return api.Goal{}, api.ErrNoGoal
	}
	return g, nil
}

// ClearGoal removes the active session's goal.
func (w *Workspace) ClearGoal() error {
	id, err := w.activeID()
	if err != nil {
		return err
	}
	w.goalMu.Lock()
	defer w.goalMu.Unlock()
	if _, ok := w.goals[id]; !ok {
		return api.ErrNoGoal
	}
	delete(w.goals, id)
	return nil
}

func (w *Workspace) goalMax() int {
	if n := w.cfg.Blitz.GoalMaxContinues; n > 0 {
		return n
	}
	return 20
}

func (w *Workspace) goalOf(id string) (api.Goal, bool) {
	w.goalMu.Lock()
	defer w.goalMu.Unlock()
	g, ok := w.goals[id]
	return g, ok
}

// goalStep judges the session's goal after a turn: the prompt to send the
// agent on with, or "" when it stops (met, impossible, at its limit, or no
// goal). What happened goes to on as a notice.
func (w *Workspace) goalStep(ctx context.Context, id string, on func(api.Event)) string {
	g, ok := w.goalOf(id)
	if !ok {
		return ""
	}
	notice := func(text string, isErr bool) {
		if on != nil {
			on(api.Event{Notice: &api.Notice{Text: text, Error: isErr}})
		}
	}
	v, err := w.engine.JudgeGoal(ctx, id, g.Condition)
	if err != nil {
		notice(i18n.T("goal.judge_failed", "error", err.Error()), true)
		return ""
	}
	w.goalMu.Lock()
	defer w.goalMu.Unlock()
	if cur, ok := w.goals[id]; !ok || cur.Condition != g.Condition {
		return "" // cleared or replaced meanwhile
	}
	switch {
	case v.Met:
		delete(w.goals, id)
		notice(i18n.T("goal.met", "reason", v.Reason), false)
		return ""
	case v.Impossible:
		delete(w.goals, id)
		notice(i18n.T("goal.impossible", "reason", v.Reason), true)
		return ""
	case g.Continues >= g.Max:
		delete(w.goals, id)
		notice(i18n.T("goal.limit", "n", g.Max), true)
		return ""
	}
	g.Continues++
	g.Last = v.Reason
	w.goals[id] = g
	notice(i18n.T("goal.continuing", "n", g.Continues, "max", g.Max, "reason", v.Reason), false)
	next := v.Next
	if next == "" {
		next = "Keep going."
	}
	return fmt.Sprintf("The goal isn't met yet: %s\n\nThe goal: %s\n\n%s", v.Reason, g.Condition, next)
}

// goalKind is how goal continuations are recorded in the transcript.
const goalKind = session.KindHook
