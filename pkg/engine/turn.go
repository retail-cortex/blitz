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
	"log/slog"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/google/uuid"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/retail-cortex/blitz/pkg/textutil"
	adksession "google.golang.org/adk/v2/session"
)

// Run sends one prompt to the agent in the session and passes its events to
// on as they happen. It runs prompt_submit hooks, audits the prompt, starts
// a checkpoint for /undo and records both sides in the transcript. A
// refused prompt is a *BlockedError. A turn that fails part way still
// returns what it produced.
func (w *Workspace) Run(ctx context.Context, sessionID string, t api.Turn, on func(api.Event)) (api.TurnResult, error) {
	var opts []runtime.ExecOption
	if t.Command {
		var err error
		if opts, err = w.expandCommand(ctx, &t); err != nil {
			return api.TurnResult{}, err
		}
	}
	// Plan permission mode, and plan_review = always, plan every prompt
	// first; side questions and read-only turns are read-only already.
	planEvery := w.tools.Hooks().Mode() == api.ModePlan || w.cfg.Blitz.PlanReview == config.PlanReviewAlways
	return w.run(ctx, sessionID, turn{Turn: t, planMode: planEvery && !t.Plan && !t.Aside && t.ReadOnly == ""}, on, w.storage, opts...)
}

// turn is a turn as the engine runs it.
type turn struct {
	api.Turn
	// planMode: the workspace is in plan permission mode, so the turn is
	// planned like Plan but the transcript records the text as typed.
	planMode bool
}

// run is Run recording the transcript in st, which holds the session as
// its active one (a worker run has its own). extra are further engine
// options for a non-aside turn (a worker's agent and model).
func (w *Workspace) run(ctx context.Context, sessionID string, t turn, on func(api.Event), st *session.Storage, extra ...runtime.ExecOption) (api.TurnResult, error) {
	// A model that couldn't be built may work now (a sign-in since).
	w.RetryModel(ctx)
	ctx = tools.WithPromptID(ctx, uuid.NewString())
	// The turn's plan state: the plan tools report the user's decision here.
	gate := tools.NewPlanGate(t.Plan || t.planMode)
	ctx = tools.WithPlanGate(ctx, gate)
	var hookContext string
	if !t.Accepted {
		var err error
		if hookContext, err = w.accept(ctx, sessionID, t.Text); err != nil {
			return api.TurnResult{}, err
		}
	}

	prompt, recorded := t.Text, t.Text
	if t.Prompt != "" {
		prompt = t.Prompt
	}
	switch {
	case t.Plan:
		prompt, recorded = runtime.PlanPrompt(t.Text), "/plan "+t.Text
	case t.planMode:
		prompt = runtime.PlanPrompt(prompt)
	}
	if !t.Aside {
		w.activate(st, sessionID)
		prompt := -1 // the prompt's index in the transcript, for /rewind
		if a := st.Active(); a != nil {
			prompt = a.MessageCount
		}
		w.tools.Checkpoints().BeginTurn(sessionID, prompt, textutil.Ellipsize(strings.Join(strings.Fields(recorded), " "), 60))
		if !t.Accepted {
			// Where the conversation stood, so /rewind can cut it here.
			events := w.engine.EventCount(ctx, sessionID)
			w.appendIn(st, sessionID, on, session.Message{Role: "user", Content: recorded + AttachmentNote(t.Images), Events: &events})
		}
		w.turnStarted(sessionID)
		defer w.turnEnded(sessionID)
	}
	// After recording: a front end may take steer messages from here on,
	// and they must follow the prompt in the transcript.
	if t.OnAccepted != nil {
		t.OnAccepted()
	}

	res := api.TurnResult{Before: w.engine.Usage(sessionID)}

	// Cost and time limits cancel the turn with their reason as the cause.
	limited := ctx
	if t.MaxCostUSD > 0 || t.Timeout > 0 {
		var cancel context.CancelCauseFunc
		limited, cancel = context.WithCancelCause(ctx)
		defer cancel(nil)
		if t.Timeout > 0 {
			var stop context.CancelFunc
			limited, stop = context.WithTimeoutCause(limited, t.Timeout, fmt.Errorf("%w (%s)", api.ErrTimeLimit, t.Timeout))
			defer stop()
		}
		if t.MaxCostUSD > 0 {
			inner := on
			on = func(e api.Event) {
				inner(e)
				if w.engine.Usage(sessionID).CostUSD-res.Before.CostUSD > t.MaxCostUSD {
					cancel(fmt.Errorf("%w ($%.2f)", api.ErrCostLimit, t.MaxCostUSD))
				}
			}
		}
	}
	ctx = limited

	r := &relay{on: on}
	handler := r.handle
	// Notices the engine raises during the turn (a fallback model taking
	// over) are part of the conversation.
	ctx = runtime.WithTurnNotices(ctx, func(msg string) { on(api.Event{Notice: &api.Notice{Text: msg}}) })

	if len(t.FetchGrants) > 0 {
		ctx = tools.WithFetchGrants(ctx, t.FetchGrants)
	}
	// Context from the user's hooks goes to the agent with the prompt: a
	// session_start hook's with the session's first real prompt.
	if !t.Aside {
		prompt = withHookContext(prompt, "session_start", w.takeSessionContext(sessionID))
		prompt = withUserEdits(prompt, w.takeUserEdits())
	}
	prompt = withHookContext(prompt, "prompt_submit", hookContext)
	prompt = w.withMentions(prompt, t.Text)
	var err error
	if t.Aside {
		err = w.engine.Aside(ctx, sessionID, prompt, handler)
	} else {
		base := append([]runtime.ExecOption(nil), extra...)
		if t.MaxTurns > 0 {
			base = append(base, runtime.WithMaxTurns(t.MaxTurns))
		}
		if t.ReadOnly != "" {
			base = append(base, runtime.WithReadOnly(t.ReadOnly))
		}
		carryOut := slices.Clone(base) // for a plan once approved
		if t.Plan || t.planMode {
			base = append(base, runtime.WithPlanOnly())
		}
		opts := slices.Clone(base)
		for _, img := range t.Images {
			opts = append(opts, runtime.WithAttachments(images.Part(img)))
		}
		err = w.engine.Execute(ctx, sessionID, prompt, handler, opts...)
		// An approved plan is carried out in the same turn (a few times at
		// most, should the agent plan again).
		for i := 0; err == nil && i < maxPlanRounds; i++ {
			_, path, mode, ok := gate.Take()
			if !ok {
				break
			}
			w.planApproved(mode)
			goAhead := runtime.CarryOutPrompt(path)
			w.appendIn(st, sessionID, on, session.Message{Role: "user", Content: "(plan approved) " + goAhead, Kind: session.KindPlan})
			base = carryOut
			r.paragraph()
			err = w.engine.Execute(ctx, sessionID, goAhead, handler, carryOut...)
		}
		// stop hooks may ask the agent to keep going, a few times at most.
		for i := 0; err == nil && i < maxStopContinues; i++ {
			out := w.tools.ScriptHooks().Run(ctx, "stop", "", tools.HookEvent{SessionID: sessionID, StopHookActive: i > 0})
			if !out.Blocked && !out.Continue {
				break
			}
			reason := strings.TrimSpace(out.Reason)
			if reason == "" {
				reason = "A stop hook asked you to continue."
			}
			w.appendIn(st, sessionID, on, session.Message{Role: "user", Content: "(stop hook) " + reason, Kind: session.KindHook})
			r.paragraph()
			err = w.engine.Execute(ctx, sessionID, reason, handler, base...)
		}
	}
	if cause := context.Cause(ctx); err != nil && (errors.Is(cause, api.ErrCostLimit) || errors.Is(cause, api.ErrTimeLimit)) {
		err = cause
	}
	if t.OnFinished != nil {
		t.OnFinished()
	}
	res.After = w.engine.Usage(sessionID)
	res.Output = r.output.String()

	if !t.Aside {
		if res.Output != "" {
			w.recordIn(st, sessionID, on, "model", res.Output)
		}
		res.Leftover = w.engine.TakeSteers(sessionID)
	}
	return res, err
}

// Steer queues a message for the agent while a turn runs in the session; the
// agent reads it with its next tool result. prompt_submit hooks apply, as to
// any prompt (a refusal is a *BlockedError), and the transcript records it.
func (w *Workspace) Steer(ctx context.Context, sessionID, text string) error {
	hookContext, err := w.accept(ctx, sessionID, text)
	if err != nil {
		return err
	}
	w.appendIn(w.storage, sessionID, nil, session.Message{Role: "user", Content: text, Kind: session.KindSteer})
	w.engine.Steer(sessionID, withHookContext(text, "prompt_submit", hookContext))
	return nil
}

// maxStopContinues bounds how often stop hooks can make one prompt go on.
const maxStopContinues = 5

// maxPlanRounds bounds how many approved plans one prompt carries out.
const maxPlanRounds = 3

// planApproved leaves plan mode for the mode the user chose to carry out
// a plan in. Outside plan mode, only a choice of accept-edits changes it.
func (w *Workspace) planApproved(mode api.PermissionMode) {
	current := w.tools.Hooks().Mode()
	if current != api.ModePlan && mode != api.ModeAcceptEdits || current == mode {
		return
	}
	if _, err := w.SetPermissionMode(string(mode)); err != nil {
		w.warn(err.Error())
	}
}

// withHookContext adds a hook's context for the agent to a prompt.
func withHookContext(prompt, event, context string) string {
	if strings.TrimSpace(context) == "" {
		return prompt
	}
	return prompt + "\n\n<" + event + "-hook-context>\n" + strings.TrimSpace(context) + "\n</" + event + "-hook-context>"
}

// accept runs prompt_submit hooks and audits the prompt.
// It returns what the hooks gave as context for the agent.
func (w *Workspace) accept(ctx context.Context, sessionID, text string) (string, error) {
	out := w.tools.ScriptHooks().PromptSubmitContext(ctx, sessionID, text)
	if out.Blocked {
		return "", &api.BlockedError{Reason: out.Reason}
	}
	w.tools.Hooks().Audit().Log(audit.Entry{Kind: audit.KindPrompt, Session: sessionID, Detail: text})
	return out.Context, nil
}

// activate makes session id st's active session when it isn't: a client
// may run a turn in a session another client (or the service before a
// restart) opened. Rewind points, renaming and the like act on the
// active session. A session st doesn't have is left to the transcript
// write to report.
func (w *Workspace) activate(st *session.Storage, id string) {
	if a := st.Active(); a != nil && a.ID == id {
		return
	}
	if _, err := st.Load(id); err != nil {
		slog.Debug("session not activated", "session", id, "error", err)
	}
}

// recordIn adds a message to session id in st. A failure is reported,
// not returned: the conversation goes on without it.
func (w *Workspace) recordIn(st *session.Storage, id string, on func(api.Event), role, text string) {
	w.appendIn(st, id, on, session.Message{Role: role, Content: text})
}

// appendIn adds m to session id in st (the session the turn runs in, not
// whichever is active). A failure goes to the log and, during a turn, to
// the user as a notice: the conversation goes on, but the chat won't show
// the message when it's opened again.
func (w *Workspace) appendIn(st *session.Storage, id string, on func(api.Event), m session.Message) {
	err := st.AppendTo(id, m)
	if err == nil {
		return
	}
	slog.Warn("session save failed", "session", id, "error", err)
	msg := i18n.T("session.save_failed", "error", err)
	w.warn(msg)
	if on != nil {
		on(api.Event{Notice: &api.Notice{Text: msg, Error: true}})
	}
}

// AttachmentNote is what the transcript records for images sent with a
// prompt: their names.
func AttachmentNote(imgs []*images.Image) string {
	if len(imgs) == 0 {
		return ""
	}
	names := make([]string, len(imgs))
	for i, img := range imgs {
		names[i] = img.Name
	}
	return "\n[images: " + strings.Join(names, ", ") + "]"
}

// relay passes a turn's ADK events on as Events, marking final text that
// repeats streamed chunks and collecting the transcript's model text.
type relay struct {
	on       func(api.Event)
	streamed bool // answer text arrived in partial chunks since the last final event
	output   strings.Builder
}

// paragraph separates the output of a run that follows another in the same
// turn (a stop hook's or an approved plan's), in the result and on screen.
func (r *relay) paragraph() {
	s := r.output.String()
	if s == "" || strings.HasSuffix(s, "\n\n") {
		return
	}
	sep := strings.Repeat("\n", 2-min(2, len(s)-len(strings.TrimRight(s, "\n"))))
	r.output.WriteString(sep)
	r.on(api.Event{Text: &api.Text{Text: sep}})
}

func (r *relay) handle(ev *adksession.Event) error {
	for _, e := range events(ev) {
		if t := e.Text; t != nil && !t.Thought {
			if t.Partial {
				r.streamed = true
			} else {
				t.Repeat = r.streamed
				r.output.WriteString(t.Text)
			}
		}
		r.on(e)
	}
	if !ev.Partial {
		r.streamed = false
	}
	return nil
}
