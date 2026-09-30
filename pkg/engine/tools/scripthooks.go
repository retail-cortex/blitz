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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/observability"
	"github.com/retail-cortex/blitz/pkg/textutil"
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
	// UpdatedArgs replace a tool call's arguments (pre_tool).
	UpdatedArgs map[string]any
}

// hookReply is the optional JSON a hook may print on stdout.
type hookReply struct {
	Decision          string         `json:"decision"` // block, allow, deny or ask
	Reason            string         `json:"reason"`
	Continue          bool           `json:"continue"`
	AdditionalContext string         `json:"additional_context"`
	UpdatedArgs       map[string]any `json:"updated_args"`
	SystemMessage     string         `json:"system_message"`
}

// Judge has a model decide a prompt hook: it reads prompt (the user's
// criteria) and the event, and answers like a hook's JSON reply (the engine
// supplies it).
type Judge func(ctx context.Context, model, prompt string, event []byte) (string, error)

// HookFailure is a hook's failed run.
type HookFailure struct {
	Time  time.Time
	Error string
}

// maxHookFailures is how many failures /hooks keeps per hook.
const maxHookFailures = 5

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
	// Notify shows a hook's system_message to the user (nil: logged only).
	Notify func(ctx context.Context, msg string)
	// Judge decides prompt hooks (nil: they fail).
	Judge Judge
	// HTTP posts http hooks' events.
	HTTP *http.Client

	// ifs are the hooks' if rules, parsed, by their text.
	ifs map[string]PermissionRule

	failMu   sync.Mutex
	failures map[string][]HookFailure // by event and index, "pre_tool#0"

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
	args    map[string]any // for the hooks' if rules
	barrier chan struct{}  // flush marker: closed when reached
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
	ifs := map[string]PermissionRule{}
	for event, list := range cfg.ByEvent() {
		for _, h := range list {
			if err := validHook(h); err != nil {
				return nil, fmt.Errorf("%s hook %q: %w", event, h.Describe(), err)
			}
			if _, err := path.Match(h.Match, ""); err != nil {
				return nil, fmt.Errorf("invalid hook match %q: %w", h.Match, err)
			}
			if h.If != "" {
				r, err := ParsePermissionRule(EffectDeny, h.If, "hook")
				if err != nil {
					return nil, fmt.Errorf("%s hook %q: if: %w", event, h.Describe(), err)
				}
				ifs[h.If] = r
			}
		}
	}
	return &ScriptHooks{
		hooks: cfg.ByEvent(), exec: env, workspace: workspace, audit: log, Warn: func(string) {},
		HTTP: &http.Client{}, ifs: ifs, failures: map[string][]HookFailure{},
	}, nil
}

// validHook checks a hook has what its type needs.
func validHook(h config.HookConfig) error {
	switch h.Kind() {
	case config.HookCommand:
		if strings.TrimSpace(h.Command) == "" && len(h.Args) == 0 {
			return errors.New("a command hook needs command or args")
		}
		if h.Command != "" && len(h.Args) > 0 {
			return errors.New("command and args both set: use one")
		}
	case config.HookHTTP:
		u, err := url.Parse(h.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("an http hook needs an http(s) url, not %q", h.URL)
		}
	case config.HookPrompt:
		if strings.TrimSpace(h.Prompt) == "" {
			return errors.New("a prompt hook needs a prompt")
		}
	default:
		return fmt.Errorf("unknown type %q (command, http or prompt)", h.Type)
	}
	return nil
}

// applies reports whether h runs for a call of tool with args: its match
// glob and its if rule.
func (s *ScriptHooks) applies(h config.HookConfig, tool string, args map[string]any) bool {
	if tool == "" {
		return true
	}
	if h.Match != "" {
		if ok, _ := path.Match(h.Match, tool); !ok {
			return false
		}
	}
	if h.If != "" {
		r, ok := s.ifs[h.If]
		return ok && r.matchesCall(tool, args)
	}
	return true
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

// PreTool runs pre_tool hooks: a block (or deny), a decision, arguments
// to use instead, and context for the agent.
func (s *ScriptHooks) PreTool(ctx context.Context, session, tool string, args map[string]any) Outcome {
	if s == nil {
		return Outcome{}
	}
	return s.Run(ctx, "pre_tool", tool, HookEvent{SessionID: session, Tool: tool, Args: args})
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
	if s == nil || !s.anyApplies(event, tool, ev.Args) {
		return
	}
	ev = s.enrich(ctx, event, ev)
	payload, err := json.Marshal(ev)
	if err != nil {
		s.Warn(fmt.Sprintf("%s hook event could not be encoded: %v", event, err))
		return
	}
	q := s.startPost()
	job := postJob{ctx: trace.ContextWithSpanContext(q.ctx, trace.SpanContextFromContext(ctx)), event: event, tool: tool, payload: payload, args: maps.Clone(ev.Args)}

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
	s.dispatch(job.ctx, job.event, job.tool, job.args, job.payload)
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

// anyApplies reports whether any of event's hooks applies to the call.
func (s *ScriptHooks) anyApplies(event, tool string, args map[string]any) bool {
	for _, h := range s.hooks[event] {
		if s.applies(h, tool, args) {
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
	if s == nil || !s.anyApplies(event, tool, ev.Args) {
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
	return s.dispatch(ctx, event, tool, ev.Args, payload)
}

// dispatch runs event's hooks that apply to the call on an encoded event;
// the first block wins.
func (s *ScriptHooks) dispatch(ctx context.Context, event, tool string, args map[string]any, payload []byte) Outcome {
	var out Outcome
	var contexts []string
	for i, h := range s.hooks[event] {
		if !s.applies(h, tool, args) {
			continue
		}
		o, msg, err := s.run(ctx, h, event, tool, payload)
		if msg != "" {
			s.notify(ctx, msg)
		}
		if err != nil {
			s.recordFailure(fmt.Sprintf("%s#%d", event, i), err)
		}
		entry := audit.Entry{Kind: audit.KindHook, Tool: tool, Detail: event + ": " + h.Describe()}
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
			slog.WarnContext(ctx, "hook failed", "event", event, "hook", h.Describe(), "error", err)
			if h.FailClosed {
				return Outcome{Blocked: true, Reason: fmt.Sprintf("hook %q failed and is fail_closed: %v", h.Describe(), err)}
			}
			s.Warn(fmt.Sprintf("hook %q failed: %v", h.Describe(), err))
			continue
		}
		if o.UpdatedArgs != nil && out.UpdatedArgs == nil && event == "pre_tool" {
			out.UpdatedArgs = o.UpdatedArgs
			s.audit.Log(audit.Entry{Kind: audit.KindHook, Tool: tool, Detail: event + ": " + h.Describe(), Decision: "updated-args", Args: o.UpdatedArgs})
			args, payload = o.UpdatedArgs, s.reencode(payload, o.UpdatedArgs) // later hooks see them
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

// run runs one hook by its type and reads its answer: the outcome, a
// message to show the user, and an error when the hook failed.
func (s *ScriptHooks) run(ctx context.Context, h config.HookConfig, event, tool string, payload []byte) (out Outcome, msg string, err error) {
	ctx, span := observability.Start(ctx, "hook "+event,
		attribute.String("hook.event", event), attribute.String("hook.type", h.Kind()), attribute.String("tool", tool))
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
		return Outcome{}, "", errors.New("hook event could not be encoded")
	}
	var text string
	switch h.Kind() {
	case config.HookHTTP:
		text, err = s.post(hctx, h, payload)
	case config.HookPrompt:
		if s.Judge == nil {
			return Outcome{}, "", errors.New("no model to judge prompt hooks")
		}
		text, err = s.Judge(hctx, h.Model, h.Prompt, payload)
	default:
		var blocked *Outcome
		text, blocked, err = s.command(hctx, h, payload)
		if blocked != nil {
			return *blocked, "", nil
		}
	}
	if hctx.Err() == context.DeadlineExceeded {
		return Outcome{}, "", fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		return Outcome{}, "", err
	}
	return readReply(event, strings.TrimSpace(text))
}

// readReply reads what a hook answered: JSON with a decision and the rest,
// or plain text, which is context for the agent where an event takes any.
func readReply(event, text string) (Outcome, string, error) {
	var reply hookReply
	if strings.HasPrefix(text, "{") && json.Unmarshal([]byte(text), &reply) == nil {
		out := Outcome{Reason: reply.Reason, Continue: reply.Continue, Context: reply.AdditionalContext, UpdatedArgs: reply.UpdatedArgs}
		switch d := strings.ToLower(reply.Decision); d {
		case "block":
			out.Blocked = true
		case "deny":
			if event == "pre_tool" { // a pre_tool deny is a block
				out.Blocked = true
			} else {
				out.Decision = d
			}
		case "allow", "ask":
			out.Decision = d
		}
		if out.Blocked && out.Reason == "" {
			out.Reason = "blocked by hook"
		}
		return out, strings.TrimSpace(reply.SystemMessage), nil
	}
	if event == "session_start" || event == "prompt_submit" {
		return Outcome{Context: text}, "", nil
	}
	return Outcome{}, "", nil
}

// command runs a command hook: bash -c command, or args without a shell.
// Exit 2 blocks with stderr as the reason; other failures are errors.
func (s *ScriptHooks) command(ctx context.Context, h config.HookConfig, payload []byte) (string, *Outcome, error) {
	argv := []string{"bash", "-c", h.Command}
	if len(h.Args) > 0 {
		argv = h.Args
	}
	cmd, err := s.exec.command(ctx, argv)
	if err != nil {
		return "", nil, err
	}
	cmd.Dir = s.workspace
	cmd.Stdin = bytes.NewReader(payload)
	stdout, stderr := newCappedBuffer(64*1024), newCappedBuffer(16*1024)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 2 {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = "blocked by hook"
			}
			return "", &Outcome{Blocked: true, Reason: msg}, nil
		}
		return "", nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil, nil
}

// post sends the event to an http hook; a 2xx body is its answer.
func (s *ScriptHooks) post(ctx context.Context, h config.HookConfig, payload []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "blitz-hooks")
	for k, v := range h.Headers {
		req.Header.Set(k, expandAllowed(v, h.AllowedEnvVars))
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, textutil.Ellipsize(strings.TrimSpace(string(body)), 200))
	}
	return string(body), nil
}

// expandAllowed replaces $VAR and ${VAR} in v with the environment's value
// for the names allowed; others stay as written.
func expandAllowed(v string, allowed []string) string {
	return os.Expand(v, func(name string) string {
		if slices.Contains(allowed, name) {
			return os.Getenv(name)
		}
		return "${" + name + "}"
	})
}

// reencode puts args in an encoded event, for the hooks after one that
// changed them.
func (s *ScriptHooks) reencode(payload []byte, args map[string]any) []byte {
	var ev map[string]any
	if json.Unmarshal(payload, &ev) != nil {
		return payload
	}
	ev["args"] = args
	out, err := json.Marshal(ev)
	if err != nil {
		return payload
	}
	return out
}

func (s *ScriptHooks) notify(ctx context.Context, msg string) {
	slog.InfoContext(ctx, "hook message", "message", msg)
	if s.Notify != nil {
		s.Notify(ctx, msg)
	}
}

func (s *ScriptHooks) recordFailure(key string, err error) {
	s.failMu.Lock()
	defer s.failMu.Unlock()
	list := append(s.failures[key], HookFailure{Time: time.Now(), Error: err.Error()})
	if len(list) > maxHookFailures {
		list = list[len(list)-maxHookFailures:]
	}
	s.failures[key] = list
}

// List is every configured hook with its recent failures, by event.
func (s *ScriptHooks) List() []api.HookInfo {
	if s == nil {
		return nil
	}
	s.failMu.Lock()
	defer s.failMu.Unlock()
	events := make([]string, 0, len(s.hooks))
	for e := range s.hooks {
		events = append(events, e)
	}
	sort.Strings(events)
	var out []api.HookInfo
	for _, e := range events {
		for i, h := range s.hooks[e] {
			info := api.HookInfo{Event: e, Type: h.Kind(), Match: h.Match, If: h.If, Runs: h.Describe(), Source: h.Source, FailClosed: h.FailClosed}
			for _, f := range s.failures[fmt.Sprintf("%s#%d", e, i)] {
				info.Failures = append(info.Failures, api.HookFailure{Time: f.Time, Error: f.Error})
			}
			out = append(out, info)
		}
	}
	return out
}
