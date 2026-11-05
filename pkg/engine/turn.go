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
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/google/uuid"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/images"
	"github.com/retail-cortex/blitz/pkg/observability"
	"github.com/retail-cortex/blitz/pkg/textutil"
	adksession "google.golang.org/adk/v2/session"
)

// Run sends one prompt to the agent in the session and passes its events to
// on as they happen. It runs prompt_submit hooks, audits the prompt, starts
// a checkpoint for /undo and records both sides in the transcript. A
// refused prompt is a *BlockedError. A turn that fails part way still
// returns what it produced.
func (w *Workspace) Run(ctx context.Context, sessionID string, t api.Turn, on func(api.Event)) (api.TurnResult, error) {
	tt, opts, f, err := w.prepareTurn(ctx, t)
	if err != nil {
		return api.TurnResult{}, err
	}
	if f != nil {
		// A workflow runs as this turn, step by step (spec_043_workflows).
		return w.runFlow(ctx, sessionID, f.flow, f.input, on)
	}
	start := time.Now()
	res, err := w.run(ctx, sessionID, tt, on, w.storage, opts...)
	if tt.Command && isReflect(tt.Text) && err == nil {
		// The next reflection's evidence starts where this one began
		// (spec_048_harness_learning HE-04).
		w.reflected(start)
	}
	if tt.Command && (isSetup(tt.Text) || isReflect(tt.Text)) {
		// The instructions and experience it wrote apply from the next
		// prompt.
		if _, rerr := w.ReloadMemory(context.WithoutCancel(ctx)); rerr != nil {
			w.warn(rerr.Error())
		}
	}
	if err == nil && !tt.Command && !tt.Aside && tools.Attended(ctx) {
		// Time to learn from the runs? Said in the chat (HE-63).
		w.offerReflect(on, time.Now())
	}
	observability.RecordTurn(ctx, w.engine.ActiveAgent(), turnOutcome(err))
	w.auditTurnEnd(sessionID, err)
	// A long turn's end is worth telling you about (spec_027_parity
	// PAR-UI-03): notification hooks hear of it.
	if after := w.cfg.UI.NotifyAfter; after > 0 && time.Since(start) >= time.Duration(after)*notifyUnit && !errors.Is(err, context.Canceled) {
		w.tools.Hooks().Notify(context.WithoutCancel(ctx), "turn_finished", i18n.T("notify.turn_finished", "seconds", int(time.Since(start).Seconds())))
	}
	return res, err
}

// isSetup reports whether a command turn's text runs /setup.
func isSetup(text string) bool {
	name, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(text), "/"), " ")
	return strings.EqualFold(name, "setup")
}

// RunDetached runs a turn in a new session of its own (blitz --bg): as a
// worker's does, the session doesn't become the workspace's active one, so
// the people working there aren't moved to it. started gets the session's
// ID before the turn begins.
func (w *Workspace) RunDetached(ctx context.Context, t api.Turn, started func(sessionID string), on func(api.Event)) (api.TurnResult, error) {
	st, err := session.NewStorage(w.cfg.Session.StorageDir)
	if err != nil {
		return api.TurnResult{}, err
	}
	st.SetWorkspace(w.Dir())
	defer st.Release()
	rec, err := st.CreateSession(session.NewSessionID(), "", w.engine.ActiveAgent())
	if err != nil {
		return api.TurnResult{}, err
	}
	rec.Origin = session.OriginBackground
	if started != nil {
		started(rec.ID)
	}
	w.sessionStarted(rec.ID, "background")
	// A workflow (spec_043_workflows WF-10) or a command runs as it would
	// in a chat.
	tt, opts, f, err := w.prepareTurn(ctx, t)
	if err != nil {
		return api.TurnResult{}, err
	}
	if f != nil {
		return w.runFlow(withFlowOrigin(withFlowStorage(ctx, st), "background"), rec.ID, f.flow, f.input, on)
	}
	res, err := w.run(ctx, rec.ID, tt, on, st, opts...)
	observability.RecordTurn(ctx, w.engine.ActiveAgent(), turnOutcome(err))
	w.auditTurnEnd(rec.ID, err)
	return res, err
}

// flowTurn is the workflow a command turn runs, with its input.
type flowTurn struct {
	flow  flows.Flow
	input string
}

// prepareTurn is t as the engine runs it, for Run and RunDetached alike:
// a command expanded (its options for the run) or the workflow it names
// (f, run instead of a turn), and planned first in plan permission mode
// or with plan_review = always (side questions and read-only turns are
// read-only already).
func (w *Workspace) prepareTurn(ctx context.Context, t api.Turn) (tt turn, opts []runtime.ExecOption, f *flowTurn, err error) {
	if t.Command {
		if fl, input, ok := w.flowCommand(t.Text); ok {
			return turn{}, nil, &flowTurn{flow: fl, input: input}, nil
		}
		if opts, err = w.expandCommand(ctx, &t); err != nil {
			return turn{}, nil, nil, err
		}
	}
	planEvery := w.tools.Hooks().Mode() == api.ModePlan || w.cfg.Blitz.PlanReview == config.PlanReviewAlways
	return turn{Turn: t, planMode: planEvery && !t.Plan && !t.Aside && t.ReadOnly == ""}, opts, nil, nil
}

// UsageOf returns a session's usage, whichever is active.
func (w *Workspace) UsageOf(sessionID string) api.Usage { return w.engine.Usage(sessionID) }

// notifyUnit is what ui.notify_after counts; tests make it small.
var notifyUnit = time.Second

// turnOutcome names how a turn ended, for metrics.
func turnOutcome(err error) string {
	var blocked *api.BlockedError
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &blocked):
		return "blocked"
	case api.IsLimit(err):
		return "limit"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	}
	return "error"
}

// auditTurnEnd records a turn that didn't finish, stopped or at a limit,
// for what agents learn from their runs (spec_048_harness_learning HE-02);
// one that finished, or failed otherwise, isn't recorded.
func (w *Workspace) auditTurnEnd(sessionID string, err error) {
	e := audit.Entry{Kind: audit.KindTurnEnd, Session: sessionID}
	switch {
	case errors.Is(err, api.ErrMaxTurns):
		e.Decision, e.Detail = audit.DecisionLimit, "steps"
	case errors.Is(err, api.ErrCostLimit):
		e.Decision, e.Detail = audit.DecisionLimit, "cost"
	case errors.Is(err, api.ErrTimeLimit):
		e.Decision, e.Detail = audit.DecisionLimit, "time"
	case errors.Is(err, context.Canceled):
		e.Decision = audit.DecisionStopped
	default:
		return
	}
	w.tools.Hooks().Audit().Log(e)
}

// turn is a turn as the engine runs it.
type turn struct {
	api.Turn
	// planMode means the workspace is in plan permission mode, so the turn is
	// planned like Plan but the transcript records the text as typed.
	planMode bool
	// run is the worker run the turn is: its file changes are the run's
	// own checkpoint (blitz workers undo), not the sessions' /undo.
	run string
	// limits say where its own MaxTurns and MaxCostUSD came from, for
	// the error when it stops at one.
	limits limitHints
	// agent is the agent it runs as when that isn't the active one (a
	// job's, a workflow step's, an eval's: the run's WithAgent).
	agent string
}

// run is Run recording the transcript in st, which holds the session as
// its active one (a worker run has its own). extra are further engine
// options for a non-aside turn (a worker's agent and model).
func (w *Workspace) run(ctx context.Context, sessionID string, t turn, on func(api.Event), st *session.Storage, extra ...runtime.ExecOption) (api.TurnResult, error) {
	// What's audited in the turn, its rules and guardrails as much as its
	// tools, records its session.
	ctx = audit.WithSession(ctx, sessionID)
	// Its background processes are left running once its turns stop for
	// long (spec_052_processes PR-21).
	defer w.tools.Processes().TurnStarted(sessionID)()
	// A model that couldn't be built may work now (a sign-in since), and
	// agents, skills, instruction files or rules may have changed.
	w.RetryModel(ctx)
	w.refreshAgentFiles(ctx)
	// Its guardrails, rules and \skill mentions are those of the agent it
	// runs as: a job's, a workflow step's or an eval's own, else the
	// active one.
	if t.agent != "" {
		ctx = withTurnAgent(ctx, t.agent)
	}
	// What before-agent rules said, and guardrails that can't run, shown
	// once the prompt is recorded.
	var ruleNotices []api.Notice
	// The turn's guardrails, read now; their turn and cost limits apply.
	var lim turnLimits
	if !t.Aside {
		lim = w.startGuardrails(sessionID, &t.Turn, t.agent)
		defer w.guards.end(sessionID)
		ruleNotices = w.guardrailProblems(sessionID)
	}
	ctx = tools.WithPromptID(ctx, uuid.NewString())
	// The turn's plan state: the plan tools report the user's decision here.
	gate := tools.NewPlanGate(t.Plan || t.planMode)
	ctx = tools.WithPlanGate(ctx, gate)
	var hookContext, ruleGuidance string
	if !t.Accepted {
		var err error
		if hookContext, err = w.accept(ctx, sessionID, t.Text, ""); err != nil {
			return api.TurnResult{}, err
		}
		if !t.Aside {
			// Before-agent rules judge the prompt (spec_041_gates).
			note := func(text string, isErr bool) { ruleNotices = append(ruleNotices, api.Notice{Text: text, Error: isErr}) }
			// Input guardrails first: safety before the team's expectations.
			if err = w.inputGuardrails(ctx, sessionID, t.Text, note); err != nil {
				return api.TurnResult{}, err
			}
			ruleGuidance, err = w.beforeRules(ctx, t.Text, note)
			if err != nil {
				return api.TurnResult{}, err
			}
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
	// \skill-name in the prompt: those skills' instructions go with it.
	prompt += w.skillMentionNote(t.Text, w.turnAgent(ctx))
	// A conversation's first prompt says where the workspace stands and
	// lists the earlier sessions (MEM-37, MEM-38), once it's recorded.
	first := false
	if !t.Aside {
		w.activate(st, sessionID)
		prompt := -1 // the prompt's index in the transcript, for /rewind
		if a := st.Active(); a != nil {
			prompt = a.MessageCount
		}
		first = prompt == 0 && !t.Accepted && t.run == ""
		label := textutil.Ellipsize(strings.Join(strings.Fields(recorded), " "), 60)
		if t.run != "" {
			w.tools.Checkpoints().BeginWorkerTurn(sessionID, t.run, label)
		} else {
			w.tools.Checkpoints().BeginTurn(sessionID, prompt, label)
		}
		if !t.Accepted {
			// Where the conversation stood, so /rewind can cut it here.
			events := w.engine.EventCount(ctx, sessionID)
			w.appendIn(st, sessionID, on, session.Message{Role: "user", Content: recorded + AttachmentNote(t.Images) + AddedNote(t.Context), Events: &events})
		}
		w.turnStarted(sessionID)
		defer w.turnEnded(sessionID)
		// Steer messages are taken from here until the leftovers are
		// collected (or the turn ends otherwise).
		w.engine.OpenSteers(sessionID)
		defer w.engine.CloseSteers(sessionID)
	}
	// After recording: a front end may take steer messages from here on,
	// and they must follow the prompt in the transcript.
	if t.OnAccepted != nil {
		t.OnAccepted()
	}
	for i := range ruleNotices {
		on(api.Event{Notice: &ruleNotices[i]})
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
			spent := w.costSince(sessionID, res.Before.CostUSD)
			on = func(e api.Event) {
				inner(e)
				if spent() > t.MaxCostUSD {
					cancel(fmt.Errorf("%w ($%.2f)", api.ErrCostLimit, t.MaxCostUSD))
				}
			}
		}
	}
	ctx = limited

	on = serialized(on)
	r := &relay{on: on}
	handler := r.handle
	ctx = runtime.WithSubagentEvents(ctx, w.subagentEvents(st, sessionID, on))
	// Notices the engine raises during the turn (a fallback model taking
	// over) are part of the conversation.
	ctx = runtime.WithTurnNotices(ctx, func(msg string) { on(api.Event{Notice: &api.Notice{Text: msg}}) })
	// A model connection being retried is said in passing.
	ctx = runtime.WithTurnStatus(ctx, func(msg string) { on(api.Event{Notice: &api.Notice{Text: msg, Transient: true}}) })

	if len(t.FetchGrants) > 0 {
		ctx = tools.WithFetchGrants(ctx, t.FetchGrants)
	}
	// Context from the user's hooks goes to the agent with the prompt: a
	// session_start hook's with the session's first real prompt.
	if !t.Aside {
		// Background tasks that ended since the agent last heard.
		prompt = withHookContext(prompt, "background-tasks", strings.Join(w.engine.TakeTaskNotes(sessionID), "\n\n"))
		prompt = withHookContext(prompt, "background-processes", strings.Join(w.TakeProcessNotices(sessionID), "\n\n"))
		prompt = withHookContext(prompt, "workspace-changed", w.takeMoveNote(sessionID))
		prompt = withHookContext(prompt, "session_start", w.takeSessionContext(sessionID))
		if first {
			prompt = withHookContext(prompt, "workspace-state", w.workspaceState(ctx))
			prompt = withHookContext(prompt, "earlier-sessions", w.earlierSessions(sessionID))
		}
		prompt = withUserEdits(prompt, w.takeUserEdits())
	}
	prompt = withHookContext(prompt, "prompt_submit", hookContext)
	prompt = withHookContext(prompt, "rules", ruleGuidance)
	prompt = w.withMentions(ctx, prompt, t.Text)
	prompt = withAdded(prompt, t.Context)
	// What git doesn't track yet, so the files the turn's commands make
	// are added at its end too (auto_add).
	var untracked map[string]bool
	if !t.Aside {
		untracked = w.untrackedNow(ctx)
	}
	var err error
	// The turn's model calls are counted together, across its runs (an
	// approved plan's, a stop hook's, a goal's and a rule's): max_turns is
	// the turn's (spec_017 SES-19). A job's workflow step's count toward
	// the job's run too.
	job := jobRunFrom(ctx)
	budget := runtime.NewBudget(t.MaxTurns, job.turnBudget())
	if t.Aside {
		err = w.engine.Aside(ctx, sessionID, prompt, handler)
	} else {
		base := append([]runtime.ExecOption(nil), extra...)
		base = append(base, runtime.WithBudget(budget))
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
		carried := false
		for i := 0; err == nil; i++ {
			_, path, mode, ok := gate.Take()
			if !ok {
				break
			}
			if i == maxPlanRounds {
				// Said, not left to look as if the turn simply ended.
				on(api.Event{Notice: &api.Notice{Text: i18n.T("turn.plan_cap", "n", maxPlanRounds), Error: true}})
				break
			}
			carried = true
			w.planApproved(mode)
			goAhead := runtime.CarryOutPrompt(path)
			w.appendIn(st, sessionID, on, session.Message{Role: "user", Content: "(plan approved) " + goAhead, Kind: session.KindPlan})
			base = carryOut
			r.paragraph()
			err = w.engine.Execute(ctx, sessionID, goAhead, handler, carryOut...)
		}
		// stop hooks may ask the agent to keep going, a few times at most.
		for i := 0; err == nil; i++ {
			out := w.tools.ScriptHooks().Run(ctx, "stop", "", tools.HookEvent{SessionID: sessionID, StopHookActive: i > 0})
			if !out.Blocked && !out.Continue {
				break
			}
			if i == maxStopContinues {
				on(api.Event{Notice: &api.Notice{Text: i18n.T("turn.stop_hook_cap", "n", maxStopContinues), Error: true}})
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
		// A goal sends the agent on until a judge says it holds.
		for err == nil && ctx.Err() == nil {
			next := w.goalStep(ctx, sessionID, on)
			if next == "" {
				break
			}
			w.appendIn(st, sessionID, on, session.Message{Role: "user", Content: "(goal) " + next, Kind: goalKind})
			r.paragraph()
			err = w.engine.Execute(ctx, sessionID, next, handler, base...)
		}
		// After-agent rules judge the answer and the changes; those that
		// revise send the agent back, a couple of times at most (a plan
		// that wasn't carried out isn't an answer to judge).
		notice := func(text string, isErr bool) { on(api.Event{Notice: &api.Notice{Text: text, Error: isErr}}) }
		for i := 0; err == nil && ctx.Err() == nil && (!(t.Plan || t.planMode) || carried); i++ {
			reasons := w.afterRules(ctx, sessionID, r.output.String(), notice)
			if len(reasons) == 0 {
				break
			}
			if i == maxRuleRevisions {
				notice(i18n.T("rule.still_failing", "n", maxRuleRevisions), true)
				break
			}
			msg := revisePrompt(reasons)
			w.appendIn(st, sessionID, on, session.Message{Role: "user", Content: "(rules) " + msg, Kind: session.KindRule})
			r.paragraph()
			err = w.engine.Execute(ctx, sessionID, msg, handler, base...)
		}
	}
	if cause := context.Cause(ctx); err != nil && (errors.Is(cause, api.ErrCostLimit) || errors.Is(cause, api.ErrTimeLimit)) {
		err = cause
	}
	hints := t.limits
	if job.stoppedRun(err) {
		// The job's whole run is at its limit, not this step's turn.
		lim, hints = turnLimits{}, job.limits
	}
	err = w.limitSource(err, lim, tools.Attended(ctx), hints)
	if !t.Aside {
		// The files the agent created go to git, in one batch, while the
		// turn can still say so.
		w.stageCreated(ctx, sessionID, untracked, on)
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
		res.Leftover = w.engine.CloseSteers(sessionID)
	}
	return res, err
}

// Steer queues a message for the agent while a turn runs in the session; the
// agent reads it with its next tool result. prompt_submit hooks apply, as to
// any prompt (a refusal is a *BlockedError), and the transcript records it.
func (w *Workspace) Steer(ctx context.Context, sessionID, text string) error {
	hookContext, err := w.accept(ctx, sessionID, text, audit.DecisionSteer)
	if err != nil {
		return err
	}
	w.appendIn(w.storage, sessionID, nil, session.Message{Role: "user", Content: text, Kind: session.KindSteer})
	// Too late (the turn collected its unread messages, or none runs): the
	// caller sends it as the next prompt, already recorded (BL-SVC-10).
	return w.engine.Steer(sessionID, withHookContext(text, "prompt_submit", hookContext))
}

// costSince is what the session has spent since its cost was before,
// less what its background tasks spent meanwhile: they have limits of
// their own (spec_032 BGA-06), and one started by an earlier turn mustn't
// use up this turn's (spec_018 WS-29).
func (w *Workspace) costSince(sessionID string, before float64) func() float64 {
	tasks := func() float64 {
		var sum float64
		for _, t := range w.engine.ListTasks([]string{sessionID}) {
			sum += t.Usage.CostUSD
		}
		return sum
	}
	tasksBefore := tasks()
	return func() float64 {
		spent := w.engine.Usage(sessionID).CostUSD - before
		if spent <= 0 {
			return spent
		}
		return spent - (tasks() - tasksBefore)
	}
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

// accept runs prompt_submit hooks and audits the prompt, with decision
// audit.DecisionSteer for one sent while a turn runs. It returns what the
// hooks gave as context for the agent.
func (w *Workspace) accept(ctx context.Context, sessionID, text, decision string) (string, error) {
	out := w.tools.ScriptHooks().PromptSubmitContext(ctx, sessionID, text)
	if out.Blocked {
		return "", &api.BlockedError{Reason: out.Reason}
	}
	w.tools.Hooks().Audit().Log(audit.Entry{Kind: audit.KindPrompt, Session: sessionID, Detail: text, Decision: decision})
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

// heldText is the end of a streamed answer its final event carries
// (runtime.HeldTextKey).
func heldText(ev *adksession.Event) (string, bool) {
	if ev == nil {
		return "", false
	}
	s, ok := ev.CustomMetadata[runtime.HeldTextKey].(string)
	return s, ok
}

// relay passes a turn's ADK events on as Events, marking final text that
// repeats streamed chunks and collecting the transcript's model text.
type relay struct {
	on       func(api.Event)
	streamed bool // answer text arrived in partial chunks since the last final event
	output   strings.Builder
	tasks    []api.Task // the agent's own task list, shown again after a sub-agent's
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
	if held, _ := heldText(ev); held != "" && r.streamed {
		// The end of the streamed answer, held back until it was checked:
		// the last piece, before the final text that repeats them all.
		r.on(api.Event{Author: ev.Author, Text: &api.Text{Text: held, Partial: true}})
	}
	for _, e := range events(ev) {
		if t := e.Text; t != nil && !t.Thought {
			if t.Partial {
				r.streamed = true
			} else {
				t.Repeat = r.streamed
				r.output.WriteString(t.Text)
			}
		}
		if e.Tasks != nil {
			r.tasks = e.Tasks
		}
		r.on(e)
		if res := e.ToolResult; res != nil && res.Name == "invoke_agent" && r.tasks != nil {
			r.on(api.Event{Author: e.Author, Tasks: r.tasks})
		}
	}
	if !ev.Partial {
		r.streamed = false
	}
	return nil
}
