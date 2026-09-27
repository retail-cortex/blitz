package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/retail-cortex/blitz/internal/audit"
	"github.com/retail-cortex/blitz/internal/config"
	"github.com/retail-cortex/blitz/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const defaultHookTimeout = 30 * time.Second

// Variables so tests can shrink them.
var (
	// postQueueSize bounds post_tool events waiting for the hook worker.
	postQueueSize = 256
	// postDrainTimeout is how long Close lets queued post_tool hooks finish
	// before killing them.
	postDrainTimeout = 5 * time.Second
)

// HookEvent is the JSON document a hook receives on stdin.
type HookEvent struct {
	Event     string         `json:"event"` // pre_tool, post_tool, prompt_submit
	SessionID string         `json:"session_id,omitempty"`
	Workspace string         `json:"workspace"`
	Tool      string         `json:"tool,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
	Result    map[string]any `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
	Prompt    string         `json:"prompt,omitempty"`

	// Context for every event.
	PromptID       string `json:"prompt_id,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
	Agent          string `json:"agent,omitempty"`
	Cwd            string `json:"cwd,omitempty"`

	// Event-specific.
	Reason         string   `json:"reason,omitempty"`           // session_start/end, compaction trigger
	StopHookActive bool     `json:"stop_hook_active,omitempty"` // stop: already continued by a stop hook
	Type           string   `json:"type,omitempty"`             // notification type
	Message        string   `json:"message,omitempty"`          // notification text
	Kind           string   `json:"kind,omitempty"`             // permission_request: the action kind
	Detail         string   `json:"detail,omitempty"`           // permission_request: what will happen
	Targets        []string `json:"targets,omitempty"`          // permission_request
	Subagent       string   `json:"subagent,omitempty"`         // subagent_start/stop
	Output         string   `json:"output,omitempty"`           // subagent_stop: its reply
}

// HookInfo is session context added to every hook event.
type HookInfo struct {
	PromptID, TranscriptPath, PermissionMode, Agent string
}

type promptIDKey struct{}

// WithPromptID tags a turn's context with its prompt's ID, for hooks.
func WithPromptID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, promptIDKey{}, id)
}

// PromptID returns the ID WithPromptID set ("" if none).
func PromptID(ctx context.Context) string {
	id, _ := ctx.Value(promptIDKey{}).(string)
	return id
}

// Outcome is what synchronous hooks decided together.
type Outcome struct {
	// Blocked (exit 2 or {"decision":"block"}), with the reason.
	Blocked bool
	Reason  string
	// Decision is "allow", "deny" or "ask" from {"decision": …}
	// (permission_request, pre_tool).
	Decision string
	// Continue: a stop hook asks the agent to keep going (Reason says why).
	Continue bool
	// Context is text for the agent: plain stdout on session_start and
	// prompt_submit, or additional_context.
	Context string
}

// hookReply is the optional JSON a hook may print on stdout.
type hookReply struct {
	Decision          string `json:"decision"` // block, allow, deny or ask
	Reason            string `json:"reason"`
	Continue          bool   `json:"continue"`
	AdditionalContext string `json:"additional_context"`
}

// ScriptHooks runs user-configured commands at lifecycle points. Hooks run
// through the same ExecEnv as shell commands (sandboxed, guarded, scrubbed
// environment) in the workspace directory.
type ScriptHooks struct {
	hooks map[string][]config.HookConfig // by event
	exec  *ExecEnv
	// Info adds session context (prompt ID, transcript, mode, agent) to
	// events; nil adds none.
	Info      func(ctx context.Context, session string) HookInfo
	workspace string
	audit     *audit.Logger
	// Warn reports hook failures that don't block (defaults to no-op).
	Warn func(string)

	// post_tool hooks only observe, so they run on one background worker
	// (in order) instead of delaying the turn. See PostTool and Close.
	postQ postQueue
}

// postJob is one post_tool event, encoded when queued so later changes to
// the tool's result maps can't race with or alter what the hook sees.
type postJob struct {
	ctx     context.Context // detached from the turn; keeps its trace
	event   string
	tool    string
	payload []byte
	barrier chan struct{} // flush marker: closed when reached
}

type postQueue struct {
	start   sync.Once
	mu      sync.RWMutex // write lock only to close jobs
	closed  bool
	jobs    chan postJob
	done    chan struct{}
	ctx     context.Context // cancelled to kill hooks still running at Close
	cancel  context.CancelFunc
	dropped atomic.Int64
}

// NewScriptHooks validates and prepares hooks.
func NewScriptHooks(cfg config.HooksConfig, env *ExecEnv, workspace string, log *audit.Logger) (*ScriptHooks, error) {
	for _, list := range cfg.ByEvent() {
		for _, h := range list {
			if strings.TrimSpace(h.Command) == "" {
				return nil, fmt.Errorf("hook with empty command (match %q)", h.Match)
			}
			if _, err := path.Match(h.Match, ""); err != nil {
				return nil, fmt.Errorf("invalid hook match %q: %w", h.Match, err)
			}
		}
	}
	return &ScriptHooks{hooks: cfg.ByEvent(), exec: env, workspace: workspace, audit: log, Warn: func(string) {}}, nil
}

// Empty reports whether no hooks are configured.
func (s *ScriptHooks) Empty() bool {
	if s == nil {
		return true
	}
	for _, list := range s.hooks {
		if len(list) > 0 {
			return false
		}
	}
	return true
}

// PreTool runs pre_tool hooks; a non-empty reason means the call is blocked.
func (s *ScriptHooks) PreTool(ctx context.Context, session, tool string, args map[string]any) string {
	if s == nil {
		return ""
	}
	return s.runAll(ctx, "pre_tool", tool, HookEvent{SessionID: session, Tool: tool, Args: args}).blockReason()
}

// PostTool queues post_tool hooks for the background worker and returns
// immediately. The hooks observe; they can't change the result. Events are
// delivered in order. If hooks fall so far behind that the queue fills, the
// event is dropped and reported rather than slowing the agent.
func (s *ScriptHooks) PostTool(ctx context.Context, session, tool string, args, result map[string]any, toolErr error) {
	if s == nil {
		return
	}
	ev := HookEvent{SessionID: session, Tool: tool, Args: args, Result: result}
	if toolErr != nil {
		ev.Error = toolErr.Error()
	}
	s.Async(ctx, "post_tool", tool, ev)
	failed := toolErr != nil
	if msg, _ := result["error"].(string); msg != "" {
		failed = true
		ev.Error = msg
	}
	if failed {
		s.Async(ctx, "post_tool_failure", tool, ev)
	}
}

// Async queues an observe-only event for the background worker, in order,
// and returns at once (tool names filter by each hook's match). Events are
// encoded now, so later changes to their maps can't alter what hooks see.
func (s *ScriptHooks) Async(ctx context.Context, event, tool string, ev HookEvent) {
	if s == nil || !anyMatch(s.hooks[event], tool) {
		return
	}
	ev = s.enrich(ctx, event, ev)
	payload, err := json.Marshal(ev)
	if err != nil {
		s.Warn(fmt.Sprintf("%s hook event could not be encoded: %v", event, err))
		return
	}
	q := s.startPost()
	job := postJob{ctx: trace.ContextWithSpanContext(q.ctx, trace.SpanContextFromContext(ctx)), event: event, tool: tool, payload: payload}

	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		return
	}
	select {
	case q.jobs <- job:
	default:
		if q.dropped.Add(1) == 1 {
			s.Warn("background hooks are falling behind; events are being dropped (see the log)")
		}
		slog.WarnContext(ctx, "hook queue full; event dropped", "event", event, "tool", tool)
		s.audit.Log(audit.Entry{Kind: audit.KindHook, Tool: tool, Detail: event + ": queue full", Decision: "dropped"})
	}
}

// startPost starts the worker on first use, so no goroutine exists unless
// a post_tool hook actually fires.
func (s *ScriptHooks) startPost() *postQueue {
	q := &s.postQ
	q.start.Do(func() {
		q.jobs = make(chan postJob, postQueueSize)
		q.done = make(chan struct{})
		q.ctx, q.cancel = context.WithCancel(context.Background())
		go func() {
			defer close(q.done)
			for job := range q.jobs {
				if job.barrier != nil {
					close(job.barrier)
					continue
				}
				s.dispatchSafely(job)
			}
		}()
	})
	return q
}

// dispatchSafely runs one queued event. The worker lives for the whole
// session, so a panic (in a hook's Warn callback, say) is contained to the
// event and logged; later events still run.
func (s *ScriptHooks) dispatchSafely(job postJob) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(job.ctx, "post_tool hook panicked", "tool", job.tool, "panic", r, "stack", string(debug.Stack()))
		}
	}()
	s.dispatch(job.ctx, s.hooks[job.event], job.tool, job.event, job.payload)
}

// flush waits until every post_tool event queued so far has been handled.
func (s *ScriptHooks) flush(ctx context.Context) error {
	q := s.startPost()
	barrier := make(chan struct{})
	q.mu.RLock()
	if q.closed {
		q.mu.RUnlock()
		return nil
	}
	select {
	case q.jobs <- postJob{barrier: barrier}:
	case <-ctx.Done():
		q.mu.RUnlock()
		return ctx.Err()
	}
	q.mu.RUnlock()
	select {
	case <-barrier:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops accepting post_tool events and lets queued hooks finish for
// up to postDrainTimeout, then kills any still running. Safe to call more
// than once.
func (s *ScriptHooks) Close() {
	if s == nil {
		return
	}
	q := &s.postQ
	q.mu.Lock()
	started := q.jobs != nil
	if started && !q.closed {
		close(q.jobs)
	}
	q.closed = true
	q.mu.Unlock()
	if !started {
		return
	}
	select {
	case <-q.done:
	case <-time.After(postDrainTimeout):
		slog.Warn("post_tool hooks still running at exit were stopped", "after", postDrainTimeout)
		q.cancel()
		<-q.done
	}
	q.cancel()
}

// anyMatch reports whether any hook applies to tool.
func anyMatch(hooks []config.HookConfig, tool string) bool {
	for _, h := range hooks {
		if h.Match == "" {
			return true
		}
		if ok, _ := path.Match(h.Match, tool); ok {
			return true
		}
	}
	return false
}

// PromptSubmit runs prompt_submit hooks; a non-empty reason blocks the prompt.
func (s *ScriptHooks) PromptSubmit(ctx context.Context, session, prompt string) string {
	if s == nil {
		return ""
	}
	return s.PromptSubmitContext(ctx, session, prompt).blockReason()
}

// PromptSubmitContext runs prompt_submit hooks and returns their outcome:
// a block, or context text for the agent.
func (s *ScriptHooks) PromptSubmitContext(ctx context.Context, session, prompt string) Outcome {
	if s == nil {
		return Outcome{}
	}
	return s.runAll(ctx, "prompt_submit", "", HookEvent{SessionID: session, Prompt: prompt})
}

// Run runs an event's hooks synchronously and returns their outcome (for
// session_start, stop and permission_request; tool filters by match).
func (s *ScriptHooks) Run(ctx context.Context, event, tool string, ev HookEvent) Outcome {
	if s == nil || !anyMatch(s.hooks[event], tool) {
		return Outcome{}
	}
	return s.runAll(ctx, event, tool, ev)
}

// Has reports whether any hook handles event.
func (s *ScriptHooks) Has(event string) bool { return s != nil && len(s.hooks[event]) > 0 }

func (o Outcome) blockReason() string {
	if o.Blocked {
		return o.Reason
	}
	return ""
}

// enrich fills in the event name, workspace and session context.
func (s *ScriptHooks) enrich(ctx context.Context, event string, ev HookEvent) HookEvent {
	ev.Event, ev.Workspace, ev.Cwd = event, s.workspace, s.workspace
	if ev.PromptID == "" {
		ev.PromptID = PromptID(ctx)
	}
	if s.Info != nil {
		info := s.Info(ctx, ev.SessionID)
		if ev.PromptID == "" {
			ev.PromptID = info.PromptID
		}
		ev.TranscriptPath, ev.PermissionMode, ev.Agent = info.TranscriptPath, info.PermissionMode, info.Agent
	}
	return ev
}

func (s *ScriptHooks) runAll(ctx context.Context, event, tool string, ev HookEvent) Outcome {
	ev = s.enrich(ctx, event, ev)
	payload, err := json.Marshal(ev)
	if err != nil {
		payload = nil // each hook then fails with the encoding error below
	}
	return s.dispatch(ctx, s.hooks[event], tool, event, payload)
}

// dispatch runs the hooks matching tool on an encoded event; the first
// block wins.
func (s *ScriptHooks) dispatch(ctx context.Context, hooks []config.HookConfig, tool, event string, payload []byte) Outcome {
	var out Outcome
	var contexts []string
	for _, h := range hooks {
		if tool != "" && h.Match != "" {
			if ok, _ := path.Match(h.Match, tool); !ok {
				continue
			}
		}
		o, err := s.run(ctx, h, event, tool, payload)
		entry := audit.Entry{Kind: audit.KindHook, Tool: tool, Detail: event + ": " + h.Command}
		switch {
		case o.Blocked:
			entry.Decision = "block"
			entry.Error = o.Reason
			s.audit.Log(entry)
			o.Context = strings.Join(append(contexts, o.Context), "\n\n")
			return o
		case err != nil:
			entry.Error = err.Error()
			s.audit.Log(entry)
			slog.WarnContext(ctx, "hook failed", "event", event, "command", h.Command, "error", err)
			if h.FailClosed {
				return Outcome{Blocked: true, Reason: fmt.Sprintf("hook %q failed and is fail_closed: %v", h.Command, err)}
			}
			s.Warn(fmt.Sprintf("hook %q failed: %v", h.Command, err))
			continue
		}
		if o.Decision != "" && out.Decision == "" {
			out.Decision, out.Reason = o.Decision, o.Reason
			entry.Decision = o.Decision
			s.audit.Log(entry)
		}
		if o.Continue && !out.Continue {
			out.Continue = true
			if out.Reason == "" {
				out.Reason = o.Reason
			}
		}
		if strings.TrimSpace(o.Context) != "" {
			contexts = append(contexts, strings.TrimSpace(o.Context))
		}
	}
	out.Context = strings.Join(contexts, "\n\n")
	return out
}

// run executes one hook. Exit 0 continues unless stdout is a JSON block
// decision; exit 2 blocks with stderr as the reason; other failures are errors.
func (s *ScriptHooks) run(ctx context.Context, h config.HookConfig, event, tool string, payload []byte) (out Outcome, err error) {
	ctx, span := observability.Start(ctx, "hook "+event,
		attribute.String("hook.event", event), attribute.String("tool", tool))
	defer func() {
		span.SetAttributes(attribute.Bool("blocked", out.Blocked))
		observability.End(span, err)
	}()
	timeout := defaultHookTimeout
	if h.TimeoutSeconds > 0 {
		timeout = time.Duration(h.TimeoutSeconds) * time.Second
	}
	hctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if payload == nil {
		return Outcome{}, errors.New("hook event could not be encoded")
	}
	cmd, err := s.exec.command(hctx, []string{"bash", "-c", h.Command})
	if err != nil {
		return Outcome{}, err
	}
	cmd.Dir = s.workspace
	cmd.Stdin = bytes.NewReader(payload)
	stdout, stderr := newCappedBuffer(64*1024), newCappedBuffer(16*1024)
	cmd.Stdout, cmd.Stderr = stdout, stderr

	runErr := cmd.Run()
	if hctx.Err() == context.DeadlineExceeded {
		return Outcome{}, fmt.Errorf("timed out after %s", timeout)
	}
	if runErr != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 2 {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = "blocked by hook"
			}
			return Outcome{Blocked: true, Reason: msg}, nil
		}
		return Outcome{}, fmt.Errorf("%v: %s", runErr, strings.TrimSpace(stderr.String()))
	}
	text := strings.TrimSpace(stdout.String())
	var reply hookReply
	if strings.HasPrefix(text, "{") && json.Unmarshal([]byte(text), &reply) == nil {
		out = Outcome{Reason: reply.Reason, Continue: reply.Continue, Context: reply.AdditionalContext}
		switch d := strings.ToLower(reply.Decision); d {
		case "block":
			out.Blocked = true
			if out.Reason == "" {
				out.Reason = "blocked by hook"
			}
		case "allow", "deny", "ask":
			out.Decision = d
		}
		return out, nil
	}
	// Plain output is context for the agent where an event gives it any.
	if event == "session_start" || event == "prompt_submit" {
		out.Context = text
	}
	return out, nil
}
