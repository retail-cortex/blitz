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
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

// Voice (spec_063_voice): a live conversation with a Gemini Live model,
// the voice agent, whose only tools hand work to the window: the window
// runs it as a typed message, so approvals, the sandbox and Changes apply
// as they do to typing (VOX-11). What it says about finished work comes
// from the turn's facts, collected here as the turn runs (VOX-17).

// ErrVoiceOff is voice used while [voice] enabled is false (VOX-02).
var ErrVoiceOff = errors.New("voice is off: turn it on in Settings › Voice")

// VoiceEvent is what a voice conversation tells the window (VOX-30).
type VoiceEvent struct {
	// Kind is "audio" (Audio: the voice's 24 kHz PCM), "heard" and "said"
	// (Text, a transcript's next piece; Final ends a line), "state"
	// (State: listening, speaking), "interrupted", the window's work:
	// "run_task", "steer" (Text), "stop", "answer" (Text, a question's
	// answer), "approve" (ID, Decision once or deny); "error" (Text) and
	// "end" (Text: why: closed, idle, goodbye, off, error).
	Kind     string
	Text     string
	Final    bool
	State    string
	Audio    []byte
	ID       string
	Decision string
}

// VoiceWaiting is what the window says is waiting on the person (VOX-13):
// an approval showing, a question asked.
type VoiceWaiting struct {
	// ApprovalID and Approval are the approval card showing ("" for none):
	// its ID and what it wants, in plain words.
	ApprovalID, Approval string
	// Question and Options are a question the agent asked.
	Question string
	Options  []string
}

// VoiceOptions are a conversation's: the session it's about and where its
// events go (called from one goroutine at a time).
type VoiceOptions struct {
	SessionID string
	OnEvent   func(VoiceEvent)
	// SummaryOnly speaks the session's last turn's summary and ends
	// (summaries = "always" without a conversation open, VOX-17).
	SummaryOnly bool
}

// Voice is a live conversation (one at a time per window, VOX-15).
type Voice struct {
	w       *Workspace
	opts    VoiceOptions
	cfg     config.VoiceConfig
	model   *runtime.VoiceModel
	live    agent.LiveSession
	cancel  context.CancelFunc
	done    chan struct{}
	idleFor time.Duration

	mu        sync.Mutex
	waiting   VoiceWaiting
	heard     strings.Builder // what the person said since the voice last answered
	said      strings.Builder // what the voice is saying
	lastHeard string          // the person's last whole line
	active    time.Time       // the last thing said or done
	ending    string          // why, once the voice said goodbye
	closed    bool
	unwatch   func()
}

// voicePrompt is the voice agent's instructions (VOX-10).
const voicePrompt = `You are Blitz's voice: the person's spoken link to the agent that works in their workspace. You talk; the agent does the work.

- Keep every reply short: one or two sentences, unless they ask for more. You are heard, not read: no lists, no Markdown, no code read aloud; say file names plainly.
- Anything that needs reading files, running commands, searching or changing something goes to the agent: call run_task with a clear, complete written request (what to do, where, and how to know it's done). Say in a few words that you've passed it on. While a task runs, more for it goes through steer; "stop" or "cancel" is stop.
- Never say you did or checked something yourself: you can't see the workspace. For where things stand, call status; for what was said earlier in the conversation, recent.
- When the agent asks for approval, say in plain words what it wants and ask whether to allow it. Call answer with the approval's ID only after the person clearly says yes or no. You can only allow it once; for anything broader, they click.
- When the agent asks a question, read it and its options, then call answer with theirs.
- Messages that start with [Blitz] are from Blitz, not the person: say what they ask, briefly, in your own words.
- When the person says they're done ("stop listening", "goodbye", "that's all"), say a short goodbye and call end_conversation.`

// StartVoice opens a voice conversation about a session (VOX-07, VOX-10):
// ErrVoiceOff with voice off, runtime.ErrVoiceNotSetUp without Gemini,
// runtime.ErrVoiceModel for a model that isn't live.
func (w *Workspace) StartVoice(ctx context.Context, opts VoiceOptions) (*Voice, error) {
	cfg := w.voiceSettings()
	if !cfg.Voice.Enabled {
		return nil, ErrVoiceOff
	}
	newModel := w.newVoiceModel
	if newModel == nil {
		newModel = runtime.NewVoiceModel
	}
	m, err := newModel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	v := &Voice{w: w, opts: opts, cfg: cfg.Voice, model: m, done: make(chan struct{}), idleFor: cfg.Voice.Idle(), active: time.Now()}
	if v.opts.OnEvent == nil {
		v.opts.OnEvent = func(VoiceEvent) {}
	}
	tools, err := v.tools()
	if err != nil {
		return nil, err
	}
	instruction := voicePrompt + v.context() + framedPersonality(w.Personality())
	a, err := llmagent.New(llmagent.Config{
		Name:                "voice",
		Description:         "Blitz's voice",
		Model:               m.LLM,
		InstructionProvider: func(agent.ReadonlyContext) (string, error) { return instruction, nil },
		Tools:               tools,
	})
	if err != nil {
		return nil, err
	}
	r, err := runner.New(runner.Config{AppName: "blitz-voice", Agent: a, SessionService: adksession.InMemoryService(), AutoCreateSession: true})
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	live, events, err := r.RunLive(runCtx, "person", "voice-"+opts.SessionID, runtime.VoiceLiveConfig(v.voiceName(), cfg.Voice.ListenMode() == config.VoiceListenHold))
	if err != nil {
		cancel()
		return nil, err
	}
	v.live, v.cancel = live, cancel
	if !opts.SummaryOnly {
		v.unwatch = w.watchTurns(opts.SessionID, v.turnEnded)
	}
	slog.InfoContext(ctx, "voice conversation started", "logger", "voice", "session", opts.SessionID, "model", m.ID, "summary_only", opts.SummaryOnly)
	go v.loop(runCtx, events)
	if !opts.SummaryOnly {
		go v.idleWatch(runCtx)
	} else if err := v.tell(v.summaryRequest(w.lastTurnFacts(opts.SessionID))); err != nil {
		v.end("error")
		return nil, err
	}
	return v, nil
}

// voiceSettings are the person's settings, which voice reads (VOX-04: a
// workspace's can't change them), with the workspace's providers.
func (w *Workspace) voiceSettings() *config.Config {
	cfg := *w.settings()
	if global, err := config.Load(w.cfg.Dir); err == nil {
		cfg.Voice = global.Voice
	}
	return &cfg
}

// voiceName is the voice spoken in: the personality's, else [voice]
// voice's, else the default (spec_064_personality PER-40).
func (v *Voice) voiceName() string {
	return cmp.Or(v.w.Personality().Voice, v.cfg.Voice, config.DefaultVoice)
}

// context is what the voice knows of where it is (VOX-10).
func (v *Voice) context() string {
	st := v.w.Settings()
	return fmt.Sprintf("\n\n## Where you are\nThe workspace is %q. Its agent is %q, on %s. Reply in the language the person speaks.", filepath.Base(v.w.Dir()), st.Agent, st.Model.Name)
}

// Audio sends the person's 16 kHz PCM.
func (v *Voice) Audio(pcm []byte) error {
	return v.live.Send(agent.LiveRequest{RealtimeInput: &genai.Blob{MIMEType: "audio/pcm;rate=16000", Data: pcm}})
}

// Activity says the person started (true) or stopped speaking, with
// listen = "hold" (VOX-07).
func (v *Voice) Activity(start bool) error {
	v.touch()
	if start {
		return v.live.Send(agent.LiveRequest{RealtimeInput: &genai.ActivityStart{}})
	}
	return v.live.Send(agent.LiveRequest{RealtimeInput: &genai.ActivityEnd{}})
}

// Waiting is what the window shows waiting on the person; a new approval
// or question is said (VOX-13, VOX-17).
func (v *Voice) Waiting(now VoiceWaiting) {
	v.mu.Lock()
	before := v.waiting
	v.waiting = now
	v.mu.Unlock()
	switch {
	case now.ApprovalID != "" && now.ApprovalID != before.ApprovalID:
		v.tellQuietly(fmt.Sprintf("[Blitz] The agent asks for approval (ID %s): %s. Ask the person whether to allow it once.", now.ApprovalID, now.Approval))
	case now.Question != "" && now.Question != before.Question:
		v.tellQuietly(fmt.Sprintf("[Blitz] The agent asks: %s%s. Read it to the person.", now.Question, optionsText(now.Options)))
	}
}

func optionsText(opts []string) string {
	if len(opts) == 0 {
		return ""
	}
	return " (options: " + strings.Join(opts, "; ") + ")"
}

// Close ends the conversation (VOX-14).
func (v *Voice) Close() { v.end("closed") }

// End ends it for a reason ("off": voice was turned off, VOX-02).
func (v *Voice) End(why string) { v.end(why) }

// ModelID is the live model it talks with.
func (v *Voice) ModelID() string { return v.model.ID }

// VoiceName is the voice it speaks in.
func (v *Voice) VoiceName() string { return v.voiceName() }

// VoiceSettings are the person's [voice] settings (VOX-04).
func (w *Workspace) VoiceSettings() config.VoiceConfig { return w.voiceSettings().Voice }

// Done is closed when the conversation has ended.
func (v *Voice) Done() <-chan struct{} { return v.done }

func (v *Voice) end(why string) {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	v.closed = true
	unwatch := v.unwatch
	v.mu.Unlock()
	if unwatch != nil {
		unwatch()
	}
	v.flush()
	_ = v.live.Close()
	v.cancel()
	v.opts.OnEvent(VoiceEvent{Kind: "end", Text: why})
	slog.Info("voice conversation ended", "logger", "voice", "session", v.opts.SessionID, "why", why)
	close(v.done)
}

func (v *Voice) touch() {
	v.mu.Lock()
	v.active = time.Now()
	v.mu.Unlock()
}

// idleWatch ends a quiet conversation with nothing running (VOX-14).
func (v *Voice) idleWatch(ctx context.Context) {
	tick := time.NewTicker(max(min(v.idleFor/4, 15*time.Second), 10*time.Millisecond))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			v.mu.Lock()
			quiet := time.Since(v.active) >= v.idleFor
			v.mu.Unlock()
			if quiet && !v.w.turnRunning(v.opts.SessionID) {
				v.end("idle")
				return
			}
		}
	}
}

// loop turns the live events into the window's (VOX-30).
func (v *Voice) loop(ctx context.Context, events func(func(*adksession.Event, error) bool)) {
	for ev, err := range events {
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("voice conversation failed", "logger", "voice", "error", err)
				v.opts.OnEvent(VoiceEvent{Kind: "error", Text: v.errorText(err)})
				v.end("error")
			}
			return
		}
		if ev == nil {
			continue
		}
		v.event(ev)
	}
	v.end(cmp.Or(v.goodbye(), "closed"))
}

func (v *Voice) goodbye() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.ending
}

func (v *Voice) event(ev *adksession.Event) {
	r := ev.LLMResponse
	if !r.Partial && (r.InputTranscription != nil || r.OutputTranscription != nil) {
		// ADK repeats a turn's whole transcript, finished, after its
		// pieces: the pieces are kept already.
		return
	}
	if t := r.InputTranscription; t != nil && t.Text != "" {
		v.touch()
		v.mu.Lock()
		v.heard.WriteString(t.Text)
		v.mu.Unlock()
		v.opts.OnEvent(VoiceEvent{Kind: "heard", Text: t.Text, Final: t.Finished})
	}
	if t := r.OutputTranscription; t != nil && t.Text != "" {
		v.touch()
		v.mu.Lock()
		first := v.said.Len() == 0
		v.said.WriteString(t.Text)
		v.mu.Unlock()
		if first {
			v.closeHeard()
			v.opts.OnEvent(VoiceEvent{Kind: "state", State: "speaking"})
		}
		v.opts.OnEvent(VoiceEvent{Kind: "said", Text: t.Text})
	}
	if r.Content != nil {
		for _, p := range r.Content.Parts {
			if p != nil && p.InlineData != nil && strings.HasPrefix(p.InlineData.MIMEType, "audio/") {
				v.opts.OnEvent(VoiceEvent{Kind: "audio", Audio: p.InlineData.Data})
			}
		}
	}
	if r.Interrupted {
		v.opts.OnEvent(VoiceEvent{Kind: "interrupted"})
	}
	if r.UsageMetadata != nil {
		slog.Debug("voice usage", "logger", "voice", "model", v.model.ID, "input_tokens", r.UsageMetadata.PromptTokenCount, "output_tokens", r.UsageMetadata.CandidatesTokenCount)
	}
	if r.TurnComplete || r.Interrupted {
		v.flush()
		v.opts.OnEvent(VoiceEvent{Kind: "state", State: "listening"})
		if why := v.goodbye(); why != "" || v.opts.SummaryOnly {
			go v.end(cmp.Or(why, "summary"))
		}
	}
}

// closeHeard ends the person's line: kept for the record and for VOX-13's
// check of a spoken yes.
func (v *Voice) closeHeard() {
	v.mu.Lock()
	line := strings.TrimSpace(v.heard.String())
	v.heard.Reset()
	if line != "" {
		v.lastHeard = line
	}
	v.mu.Unlock()
	if line != "" {
		v.opts.OnEvent(VoiceEvent{Kind: "heard", Final: true})
		v.record("user", line)
	}
}

// flush keeps what was said on both sides (VOX-16).
func (v *Voice) flush() {
	v.closeHeard()
	v.mu.Lock()
	said := strings.TrimSpace(v.said.String())
	v.said.Reset()
	v.mu.Unlock()
	if said != "" {
		v.opts.OnEvent(VoiceEvent{Kind: "said", Final: true})
		v.record("model", said)
	}
}

func (v *Voice) record(role, text string) {
	if v.opts.SessionID == "" {
		return
	}
	v.w.appendIn(v.w.storage, v.opts.SessionID, nil, session.Message{Role: role, Content: text, Kind: session.KindSpoken})
}

// tell sends Blitz's own message to the voice ([Blitz] …), which it says.
func (v *Voice) tell(text string) error {
	v.touch()
	return v.live.Send(agent.LiveRequest{Content: genai.NewContentFromText(text, genai.RoleUser)})
}

func (v *Voice) tellQuietly(text string) {
	if err := v.tell(text); err != nil {
		slog.Warn("voice: not told", "logger", "voice", "error", err)
	}
}

// errorText is err as the window says it (VOX-24), with no secret in it.
func (v *Voice) errorText(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "API key not valid"), strings.Contains(s, "401"), strings.Contains(s, "403"):
		return "Google refused the voice's sign-in: check Gemini in Settings › Providers."
	case strings.Contains(s, "429"), strings.Contains(s, "RESOURCE_EXHAUSTED"), strings.Contains(s, "quota"):
		return "Gemini's quota for voice is spent for now: try again later."
	}
	return "The voice conversation stopped: " + v.w.secretRedactor().String(s)
}

// ---- The voice agent's tools (VOX-11).

// VoiceTaskInput is run_task's and steer's.
type VoiceTaskInput struct {
	Prompt string `json:"prompt" jsonschema:"The request for the agent, written out clearly: what to do, where, and how to know it's done"`
}

// VoiceAnswerInput is answer's.
type VoiceAnswerInput struct {
	Choice     string `json:"choice" jsonschema:"For a question: the option chosen, or the person's answer. For an approval: yes or no"`
	ApprovalID string `json:"approval_id,omitempty" jsonschema:"The approval's ID, from Blitz's message or status, when answering an approval"`
}

// VoiceRecentInput is recent's.
type VoiceRecentInput struct {
	N int `json:"n,omitempty" jsonschema:"How many of the conversation's last messages (1 to 10; default 5)"`
}

// VoiceNone is the tools that take nothing.
type VoiceNone struct{}

// VoiceResult is what the tools return.
type VoiceResult struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Status string `json:"status,omitempty"`
}

func (v *Voice) tools() ([]tool.Tool, error) {
	var out []tool.Tool
	add := func(t tool.Tool, err error) error {
		out = append(out, t)
		return err
	}
	errs := []error{
		add(functiontool.New(functiontool.Config{Name: "run_task", Description: "Hand a request to the workspace's agent, as if the person typed it. Only when no task runs (then use steer)."},
			func(_ agent.Context, in VoiceTaskInput) (VoiceResult, error) {
				v.touch()
				if strings.TrimSpace(in.Prompt) == "" {
					return VoiceResult{Error: "write the request out"}, nil
				}
				if v.w.turnRunning(v.opts.SessionID) {
					return VoiceResult{Error: "a task is running: use steer to add to it, or stop it first"}, nil
				}
				v.opts.OnEvent(VoiceEvent{Kind: "run_task", Text: strings.TrimSpace(in.Prompt)})
				return VoiceResult{OK: true}, nil
			})),
		add(functiontool.New(functiontool.Config{Name: "steer", Description: "Add to the task that's running: a correction, a detail, a next step."},
			func(_ agent.Context, in VoiceTaskInput) (VoiceResult, error) {
				v.touch()
				if !v.w.turnRunning(v.opts.SessionID) {
					return VoiceResult{Error: "no task is running: use run_task"}, nil
				}
				v.opts.OnEvent(VoiceEvent{Kind: "steer", Text: strings.TrimSpace(in.Prompt)})
				return VoiceResult{OK: true}, nil
			})),
		add(functiontool.New(functiontool.Config{Name: "stop", Description: "Stop the task that's running."},
			func(agent.Context, VoiceNone) (VoiceResult, error) {
				v.touch()
				if !v.w.turnRunning(v.opts.SessionID) {
					return VoiceResult{Error: "no task is running"}, nil
				}
				v.opts.OnEvent(VoiceEvent{Kind: "stop"})
				return VoiceResult{OK: true}, nil
			})),
		add(functiontool.New(functiontool.Config{Name: "status", Description: "Where things stand: what's running, what's waiting on the person (an approval with its ID, a question), and how the last task ended."},
			func(agent.Context, VoiceNone) (VoiceResult, error) {
				v.touch()
				return VoiceResult{OK: true, Status: v.status()}, nil
			})),
		add(functiontool.New(functiontool.Config{Name: "answer", Description: "Answer the agent's question, or an approval (with its ID) once the person clearly said yes or no."},
			func(_ agent.Context, in VoiceAnswerInput) (VoiceResult, error) {
				v.touch()
				return v.answer(in), nil
			})),
		add(functiontool.New(functiontool.Config{Name: "recent", Description: "The conversation's last messages, as text."},
			func(_ agent.Context, in VoiceRecentInput) (VoiceResult, error) {
				v.touch()
				return VoiceResult{OK: true, Status: v.recent(in.N)}, nil
			})),
		add(functiontool.New(functiontool.Config{Name: "end_conversation", Description: "End the voice conversation, after saying goodbye."},
			func(agent.Context, VoiceNone) (VoiceResult, error) {
				v.mu.Lock()
				v.ending = "goodbye"
				v.mu.Unlock()
				return VoiceResult{OK: true}, nil
			})),
	}
	return out, errors.Join(errs...)
}

var (
	spokenYes = regexp.MustCompile(`(?i)\b(yes|yeah|yep|yup|sure|ok|okay|allow( it)?|go ahead|do it|approve[d]?|sí|si|oui|d'accord|vas-y|adelante)\b`)
	spokenNo  = regexp.MustCompile(`(?i)\b(no|nope|don'?t|deny|refuse|stop|cancel|not|non|pas|nunca)\b`)
)

// answer is VOX-13: an approval only once, only the one showing, and only
// after the person's own words say so; a question by its answer.
func (v *Voice) answer(in VoiceAnswerInput) VoiceResult {
	v.closeHeard()
	v.mu.Lock()
	w, heard := v.waiting, v.lastHeard
	v.mu.Unlock()
	if in.ApprovalID == "" {
		if w.Question == "" {
			return VoiceResult{Error: "no question is waiting"}
		}
		v.opts.OnEvent(VoiceEvent{Kind: "answer", Text: strings.TrimSpace(in.Choice)})
		return VoiceResult{OK: true}
	}
	if w.ApprovalID == "" || in.ApprovalID != w.ApprovalID {
		return VoiceResult{Error: "that approval isn't the one showing: ask the person to look at the window"}
	}
	yes, no := spokenYes.MatchString(heard), spokenNo.MatchString(heard)
	choiceYes := spokenYes.MatchString(in.Choice) && !spokenNo.MatchString(in.Choice)
	switch {
	case no && !yes || !choiceYes && spokenNo.MatchString(in.Choice) && no:
		v.opts.OnEvent(VoiceEvent{Kind: "approve", ID: w.ApprovalID, Decision: "deny"})
		return VoiceResult{OK: true, Status: "denied"}
	case choiceYes && yes && !no:
		v.opts.OnEvent(VoiceEvent{Kind: "approve", ID: w.ApprovalID, Decision: "once"})
		return VoiceResult{OK: true, Status: "allowed once"}
	}
	return VoiceResult{Error: "the person didn't clearly say yes or no: ask again, or leave it for them to click"}
}

// status is what the status tool says (VOX-11), with nothing secret in it
// (VOX-12).
func (v *Voice) status() string {
	var b strings.Builder
	f := v.w.lastTurnFacts(v.opts.SessionID)
	if v.w.turnRunning(v.opts.SessionID) {
		cur := v.w.currentTurnFacts(v.opts.SessionID)
		fmt.Fprintf(&b, "A task is running (for %s)", cur.took().Round(time.Second))
		if len(cur.Tools) > 0 {
			fmt.Fprintf(&b, "; its last tools: %s", strings.Join(lastN(cur.Tools, 5), ", "))
		}
		if len(cur.Tasks) > 0 {
			fmt.Fprintf(&b, "; its task list: %s", strings.Join(cur.Tasks, "; "))
		}
		b.WriteString(". ")
	} else {
		b.WriteString("No task is running. ")
	}
	v.mu.Lock()
	w := v.waiting
	v.mu.Unlock()
	if w.ApprovalID != "" {
		fmt.Fprintf(&b, "An approval is waiting (ID %s): %s. ", w.ApprovalID, w.Approval)
	}
	if w.Question != "" {
		fmt.Fprintf(&b, "A question is waiting: %s%s. ", w.Question, optionsText(w.Options))
	}
	if f != nil {
		b.WriteString("The last task: " + f.describe())
	}
	return v.w.secretRedactor().String(strings.TrimSpace(b.String()))
}

// recent is the conversation's last n messages (VOX-11), redacted (VOX-12).
func (v *Voice) recent(n int) string {
	n = max(1, min(cmp.Or(n, 5), 10))
	rec, err := v.w.storage.Load(v.opts.SessionID)
	if err != nil || rec == nil {
		return "No messages yet."
	}
	msgs := rec.Messages
	if len(msgs) > n {
		msgs = msgs[len(msgs)-n:]
	}
	var b strings.Builder
	for _, m := range msgs {
		who := "The agent"
		if m.Role == "user" {
			who = "The person"
		}
		if m.Kind == session.KindSpoken {
			who += " (spoken)"
		}
		text := m.Content
		if len(text) > 1200 {
			text = text[:1200] + "…"
		}
		fmt.Fprintf(&b, "%s: %s\n", who, text)
	}
	return v.w.secretRedactor().String(b.String())
}

// turnEnded is the session's turn ending: said when summaries allow it
// (VOX-17).
func (v *Voice) turnEnded(f *turnFacts) {
	if v.cfg.SummaryMode() == config.VoiceSummaryOff {
		if f.Outcome == "failed" {
			v.tellQuietly("[Blitz] The task failed: " + v.w.secretRedactor().String(f.Error) + ". Say so briefly.")
		}
		return
	}
	v.tellQuietly(v.summaryRequest(f))
}

// summaryRequest asks the voice to say f in a sentence or two.
func (v *Voice) summaryRequest(f *turnFacts) string {
	if f == nil {
		return "[Blitz] Nothing has run in this conversation yet. Say so in one sentence."
	}
	return "[Blitz] The task ended. " + v.w.secretRedactor().String(f.describe()) + " Tell the person in one or two sentences: how it ended, what changed, how the checks went, and what's waiting on them."
}

// ---- The turns' facts (VOX-17), collected as each turn runs.

// turnFacts are what a turn did, from its events, not from what it says.
type turnFacts struct {
	Prompt   string
	Started  time.Time
	Ended    time.Time
	Outcome  string // finished, stopped, failed, limit
	Error    string
	Files    []string
	Commands []commandFact
	Tools    []string
	Tasks    []string
	CostUSD  float64
}

type commandFact struct {
	Command string
	Exit    int
}

func (f *turnFacts) took() time.Duration {
	if f.Ended.IsZero() {
		return time.Since(f.Started)
	}
	return f.Ended.Sub(f.Started)
}

// checks are the commands that look like builds, tests or linters.
var checks = regexp.MustCompile(`\b(test|tests|build|lint|vet|check|pytest|jest|vitest|bazel|make|cargo|go|npm|pnpm|yarn|tsc|mvn|gradle)\b`)

// describe is f as plain facts for the voice.
func (f *turnFacts) describe() string {
	var b strings.Builder
	switch f.Outcome {
	case "stopped":
		b.WriteString("It was stopped. ")
	case "failed":
		fmt.Fprintf(&b, "It failed: %s. ", f.Error)
	case "limit":
		fmt.Fprintf(&b, "It stopped at a limit: %s. ", f.Error)
	default:
		b.WriteString("It finished. ")
	}
	switch n := len(f.Files); {
	case n == 0:
		b.WriteString("No files changed. ")
	case n <= 3:
		fmt.Fprintf(&b, "Files changed: %s. ", strings.Join(f.Files, ", "))
	default:
		fmt.Fprintf(&b, "%d files changed, among them %s. ", n, strings.Join(f.Files[:3], ", "))
	}
	passed, failed := 0, []string{}
	for _, c := range f.Commands {
		if !checks.MatchString(c.Command) {
			continue
		}
		if c.Exit == 0 {
			passed++
		} else {
			failed = append(failed, fmt.Sprintf("%q (exit %d)", short(c.Command, 60), c.Exit))
		}
	}
	if passed+len(failed) > 0 {
		fmt.Fprintf(&b, "Checks run: %d passed", passed)
		if len(failed) > 0 {
			fmt.Fprintf(&b, ", %d failed: %s", len(failed), strings.Join(lastN(failed, 3), ", "))
		}
		b.WriteString(". ")
	}
	if f.CostUSD >= 1 {
		fmt.Fprintf(&b, "It cost $%.2f. ", f.CostUSD)
	}
	return strings.TrimSpace(b.String())
}

func short(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func lastN[T any](s []T, n int) []T {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// fileTools are the tools that change files, by the argument naming one.
var fileTools = map[string]string{"create_file": "path", "replace_in_file": "path", "delete_file": "path", "apply_patch": "path"}

// observe adds e to f.
func (f *turnFacts) observe(e api.Event) {
	if e.Tasks != nil {
		f.Tasks = f.Tasks[:0]
		for _, t := range e.Tasks {
			if t.Status != "done" {
				f.Tasks = append(f.Tasks, t.Content)
			}
		}
	}
	if c := e.ToolCall; c != nil && !c.Partial {
		f.Tools = append(f.Tools, c.Name)
		if arg, ok := fileTools[c.Name]; ok {
			if p, _ := c.Args[arg].(string); p != "" && !slices.Contains(f.Files, p) {
				f.Files = append(f.Files, p)
			}
		}
		if c.Name == "run_shell_command" {
			cmd, _ := c.Args["command"].(string)
			f.Commands = append(f.Commands, commandFact{Command: cmd, Exit: -2})
		}
	}
	if r := e.ToolResult; r != nil && r.Name == "run_shell_command" {
		for i := len(f.Commands) - 1; i >= 0; i-- {
			if f.Commands[i].Exit == -2 {
				f.Commands[i].Exit = exitOf(r.Result)
				break
			}
		}
	}
}

func exitOf(res map[string]any) int {
	switch n := res["exit_code"].(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	if res["error"] != nil {
		return -1
	}
	return 0
}

// outcomeOf is how a turn that returned err ended.
func outcomeOf(err error) (string, string) {
	switch {
	case err == nil:
		return "finished", ""
	case errors.Is(err, context.Canceled):
		return "stopped", ""
	case api.IsLimit(err):
		return "limit", err.Error()
	}
	return "failed", err.Error()
}

// turnWatch keeps each session's running turn's facts, its last turn's,
// and the voices watching it.
type turnWatch struct {
	mu       sync.Mutex
	running  map[string]*turnFacts
	last     map[string]*turnFacts
	watchers map[string][]*turnWatcher
}

type turnWatcher struct{ ended func(*turnFacts) }

// watchTurns calls ended with the facts of each of the session's turns as
// it ends, until the returned func is called.
func (w *Workspace) watchTurns(sessionID string, ended func(*turnFacts)) func() {
	tw := &w.turns
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.watchers == nil {
		tw.watchers = map[string][]*turnWatcher{}
	}
	h := &turnWatcher{ended: ended}
	tw.watchers[sessionID] = append(tw.watchers[sessionID], h)
	return func() {
		tw.mu.Lock()
		defer tw.mu.Unlock()
		tw.watchers[sessionID] = slices.DeleteFunc(tw.watchers[sessionID], func(x *turnWatcher) bool { return x == h })
	}
}

// factsStart begins a turn's facts; the returned on adds each event to
// them before passing it on.
func (w *Workspace) factsStart(sessionID, prompt string, on func(api.Event)) func(api.Event) {
	f := &turnFacts{Prompt: prompt, Started: time.Now()}
	tw := &w.turns
	tw.mu.Lock()
	if tw.running == nil {
		tw.running, tw.last = map[string]*turnFacts{}, map[string]*turnFacts{}
	}
	tw.running[sessionID] = f
	tw.mu.Unlock()
	return func(e api.Event) {
		tw.mu.Lock()
		f.observe(e)
		tw.mu.Unlock()
		if on != nil {
			on(e)
		}
	}
}

// factsEnd ends the session's turn's facts and tells its watchers.
func (w *Workspace) factsEnd(sessionID string, res api.TurnResult, err error) {
	tw := &w.turns
	tw.mu.Lock()
	f := tw.running[sessionID]
	delete(tw.running, sessionID)
	if f == nil {
		tw.mu.Unlock()
		return
	}
	f.Ended = time.Now()
	f.Outcome, f.Error = outcomeOf(err)
	f.CostUSD = res.After.CostUSD - res.Before.CostUSD
	tw.last[sessionID] = f
	watchers := slices.Clone(tw.watchers[sessionID])
	tw.mu.Unlock()
	for _, h := range watchers {
		h.ended(f)
	}
}

func (w *Workspace) turnRunning(sessionID string) bool {
	tw := &w.turns
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.running[sessionID] != nil
}

func (w *Workspace) currentTurnFacts(sessionID string) turnFacts {
	tw := &w.turns
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if f := tw.running[sessionID]; f != nil {
		c := *f
		c.Tools, c.Tasks = slices.Clone(f.Tools), slices.Clone(f.Tasks)
		return c
	}
	return turnFacts{}
}

func (w *Workspace) lastTurnFacts(sessionID string) *turnFacts {
	tw := &w.turns
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.last[sessionID]
}
