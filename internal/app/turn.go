package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/retail-cortex/blitz/internal/audit"
	"github.com/retail-cortex/blitz/internal/i18n"
	"github.com/retail-cortex/blitz/internal/images"
	"github.com/retail-cortex/blitz/internal/runtime"
	"github.com/retail-cortex/blitz/internal/session"
	"github.com/retail-cortex/blitz/internal/textutil"
	"github.com/retail-cortex/blitz/internal/tools"
	adksession "google.golang.org/adk/v2/session"
)

// Turn is one prompt for the agent.
type Turn struct {
	// Text is what the user typed. prompt_submit hooks see it and the
	// transcript records it.
	Text string
	// Prompt, if set, is sent to the agent instead of Text (e.g. /search
	// sends the fetched results, and the transcript records the command).
	Prompt string
	// Plan: Text is a goal to plan for. Tools that change anything are
	// refused for the whole turn (runtime.WithPlanOnly), and the transcript
	// records "/plan <Text>".
	Plan bool
	// ReadOnly names a mode that refuses the same tools as plan mode
	// (runtime.WithReadOnly).
	ReadOnly string
	// Aside: a /btw question, answered in a throwaway copy of the session
	// (runtime.Engine.Aside) and recorded nowhere. It takes no images, starts
	// no checkpoint and leaves queued steer messages alone.
	Aside bool
	// Accepted: Text already passed prompt_submit hooks and was recorded (a
	// steer message that arrived too late to be read mid-turn).
	Accepted bool
	// Command: Text is a custom slash command ("/name args"), expanded into
	// its prompt with its agent, model, tools and plan mode; the
	// transcript records Text.
	Command bool
	// Images are sent with the prompt.
	Images []*images.Image
	// MaxTurns limits the model calls in the turn (0: unlimited).
	MaxTurns int
	// MaxCostUSD stops the turn once it has cost more than this (0:
	// unlimited); it can only be enforced when the model has a price.
	MaxCostUSD float64
	// Timeout stops the turn after this long (0: unlimited).
	Timeout time.Duration
	// FetchGrants are URLs the agent may fetch in this turn without asking
	// (the pages a web search handed it).
	FetchGrants []string
	// OnAccepted, if set, runs once the prompt has passed prompt_submit
	// hooks and been recorded, just before it is sent.
	OnAccepted func()
	// OnFinished, if set, runs when the agent has stopped, before unread
	// steer messages are collected: a front end still taking a steer message
	// finishes here, so the message ends up in TurnResult.Leftover.
	OnFinished func()

	// planMode: the workspace is in plan permission mode, so the turn is
	// planned like Plan but the transcript records the text as typed.
	planMode bool
}

// TurnResult describes a finished turn.
type TurnResult struct {
	// Output is the model's final text (partial and thought text excluded).
	Output string
	// Before and After are the session's usage around the turn.
	Before, After runtime.Usage
	// Leftover are steer messages sent after the model's last tool call, so
	// never read. Front ends send them as the next turn (with Accepted set)
	// or, if the turn was interrupted, drop them.
	Leftover []string
}

// Limits a turn can stop at. They wrap the limit, e.g. "the turn reached
// its cost limit ($0.50)", and runtime.ErrMaxTurns is re-exported so
// front ends match all three without importing the engine.
var (
	ErrMaxTurns  = runtime.ErrMaxTurns
	ErrCostLimit = errors.New("the turn reached its cost limit")
	ErrTimeLimit = errors.New("the turn reached its time limit")
)

// IsLimit reports whether err is a turn stopping at one of its limits.
func IsLimit(err error) bool {
	return errors.Is(err, ErrMaxTurns) || errors.Is(err, ErrCostLimit) || errors.Is(err, ErrTimeLimit)
}

// BlockedError reports a prompt refused by a prompt_submit hook. Nothing
// was recorded or sent.
type BlockedError struct{ Reason string }

func (e *BlockedError) Error() string { return "prompt blocked by hook: " + e.Reason }

// Run sends one prompt to the agent in the session and passes its events to
// on as they happen. It runs prompt_submit hooks, audits the prompt, starts
// a checkpoint for /undo and records both sides in the transcript. A
// refused prompt is a *BlockedError. A turn that fails part way still
// returns what it produced.
func (w *Workspace) Run(ctx context.Context, sessionID string, t Turn, on func(Event)) (TurnResult, error) {
	// Plan permission mode plans every prompt; side questions and
	// read-only turns are read-only already.
	if w.tools.Hooks().Mode() == tools.ModePlan && !t.Plan && !t.Aside && t.ReadOnly == "" {
		t.planMode = true
	}
	var opts []runtime.ExecOption
	if t.Command {
		var err error
		if opts, err = w.expandCommand(ctx, &t); err != nil {
			return TurnResult{}, err
		}
	}
	return w.run(ctx, sessionID, t, on, w.storage, opts...)
}

// run is Run recording the transcript in st, which holds the session as
// its active one (a worker run has its own). extra are further engine
// options for a non-aside turn (a worker's agent and model).
func (w *Workspace) run(ctx context.Context, sessionID string, t Turn, on func(Event), st *session.Storage, extra ...runtime.ExecOption) (TurnResult, error) {
	ctx = tools.WithPromptID(ctx, uuid.NewString())
	var hookContext string
	if !t.Accepted {
		var err error
		if hookContext, err = w.accept(ctx, sessionID, t.Text); err != nil {
			return TurnResult{}, err
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
		w.tools.Checkpoints().Begin(textutil.Ellipsize(strings.Join(strings.Fields(recorded), " "), 60))
		if !t.Accepted {
			w.recordIn(st, "user", recorded+AttachmentNote(t.Images))
		}
	}
	// After recording: a front end may take steer messages from here on,
	// and they must follow the prompt in the transcript.
	if t.OnAccepted != nil {
		t.OnAccepted()
	}

	res := TurnResult{Before: w.engine.Usage(sessionID)}

	// Cost and time limits cancel the turn with their reason as the cause.
	limited := ctx
	if t.MaxCostUSD > 0 || t.Timeout > 0 {
		var cancel context.CancelCauseFunc
		limited, cancel = context.WithCancelCause(ctx)
		defer cancel(nil)
		if t.Timeout > 0 {
			var stop context.CancelFunc
			limited, stop = context.WithTimeoutCause(limited, t.Timeout, fmt.Errorf("%w (%s)", ErrTimeLimit, t.Timeout))
			defer stop()
		}
		if t.MaxCostUSD > 0 {
			inner := on
			on = func(e Event) {
				inner(e)
				if w.engine.Usage(sessionID).CostUSD-res.Before.CostUSD > t.MaxCostUSD {
					cancel(fmt.Errorf("%w ($%.2f)", ErrCostLimit, t.MaxCostUSD))
				}
			}
		}
	}
	ctx = limited

	r := &relay{on: on}
	handler := r.handle

	if len(t.FetchGrants) > 0 {
		ctx = tools.WithFetchGrants(ctx, t.FetchGrants)
	}
	// Context from the user's hooks goes to the agent with the prompt: a
	// session_start hook's with the session's first real prompt.
	if !t.Aside {
		prompt = withHookContext(prompt, "session_start", w.takeSessionContext(sessionID))
	}
	prompt = withHookContext(prompt, "prompt_submit", hookContext)
	var err error
	if t.Aside {
		err = w.engine.Aside(ctx, sessionID, prompt, handler)
	} else {
		base := append([]runtime.ExecOption(nil), extra...)
		if t.MaxTurns > 0 {
			base = append(base, runtime.WithMaxTurns(t.MaxTurns))
		}
		if t.Plan || t.planMode {
			base = append(base, runtime.WithPlanOnly())
		}
		if t.ReadOnly != "" {
			base = append(base, runtime.WithReadOnly(t.ReadOnly))
		}
		opts := slices.Clone(base)
		for _, img := range t.Images {
			opts = append(opts, runtime.WithAttachments(images.Part(img)))
		}
		err = w.engine.Execute(ctx, sessionID, prompt, handler, opts...)
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
			w.recordIn(st, "user", "(stop hook) "+reason)
			err = w.engine.Execute(ctx, sessionID, reason, handler, base...)
		}
	}
	if cause := context.Cause(ctx); err != nil && (errors.Is(cause, ErrCostLimit) || errors.Is(cause, ErrTimeLimit)) {
		err = cause
	}
	if t.OnFinished != nil {
		t.OnFinished()
	}
	res.After = w.engine.Usage(sessionID)
	res.Output = r.output.String()

	if !t.Aside {
		if res.Output != "" {
			w.recordIn(st, "model", res.Output)
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
	w.record("user", text)
	w.engine.Steer(sessionID, withHookContext(text, "prompt_submit", hookContext))
	return nil
}

// maxStopContinues bounds how often stop hooks can make one prompt go on.
const maxStopContinues = 5

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
		return "", &BlockedError{Reason: out.Reason}
	}
	w.tools.Hooks().Audit().Log(audit.Entry{Kind: audit.KindPrompt, Session: sessionID, Detail: text})
	return out.Context, nil
}

// record adds a message to the active session's transcript. A failure is
// reported, not returned: the conversation goes on without it.
func (w *Workspace) record(role, text string) { w.recordIn(w.storage, role, text) }

// recordIn adds a message to st's active session.
func (w *Workspace) recordIn(st *session.Storage, role, text string) {
	if err := st.AddMessage(role, text); err != nil {
		slog.Warn("session save failed", "error", err)
		w.warn(i18n.T("session.save_failed", "error", err))
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
	on       func(Event)
	streamed bool // answer text arrived in partial chunks since the last final event
	output   strings.Builder
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
