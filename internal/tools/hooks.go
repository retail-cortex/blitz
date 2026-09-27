package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/retail-cortex/blitz/internal/audit"
	"github.com/retail-cortex/blitz/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

// ActionKind classifies a sensitive tool action for approval purposes.
type ActionKind string

const (
	ActionCommand ActionKind = "run_command" // shell commands and forged tool execution
	ActionWrite   ActionKind = "write_file"  // file creation and edits
	ActionDelete  ActionKind = "delete_file" // file deletion
	ActionNetwork ActionKind = "network"     // outbound web requests
	ActionMCP     ActionKind = "mcp_tool"    // tools served by MCP servers
)

// ApprovalRequest describes an action awaiting user approval.
type ApprovalRequest struct {
	Tool   string
	Kind   ActionKind
	Detail string
	// Diff is a unified diff of the proposed file changes, if any.
	Diff string
	// Key identifies what "allow for this session" / "always allow" covers,
	// e.g. an exact command or all edits in a workspace. Empty means the
	// approval can only be given once.
	Key string
	// KeyLabel describes Key for the user ("this exact command").
	KeyLabel string
	// Targets are what the action is on, for policies that match them (a
	// worker's permissions): workspace-relative paths for file changes (a
	// patch may touch several), the command, the URL's host, the search
	// provider, or the MCP tool as "server:tool". Empty when nothing can
	// match, which such policies refuse.
	Targets []string
	// MustAsk: an ask rule covers the action, so the user is asked even
	// when a mode, an allow rule or a remembered approval would let it
	// through (and refused where nobody can be asked).
	MustAsk bool
}

// Decision is the user's answer to an approval request.
type Decision int

const (
	DecisionDeny    Decision = iota
	DecisionOnce             // allow this one action
	DecisionSession          // allow actions with the same Key until exit
	DecisionAlways           // allow actions with the same Key, persisted
)

func (d Decision) String() string {
	switch d {
	case DecisionOnce:
		return "once"
	case DecisionSession:
		return "session"
	case DecisionAlways:
		return "always"
	default:
		return "deny"
	}
}

// Approver asks the user to decide on a sensitive action.
type Approver func(ctx context.Context, req ApprovalRequest) (Decision, error)

// UserPromptFunc asks the user a question and returns the answer.
type UserPromptFunc func(ctx context.Context, question string, options []string) (string, error)

// InvokeAgentFunc runs a registered sub-agent with a prompt and returns its reply.
type InvokeAgentFunc func(ctx context.Context, agentName, prompt string) (string, error)

// Policy controls which actions skip the approval prompt.
type Policy struct {
	// Mode is the permission mode ("" is ModeDefault). NewRegistry sets
	// it from [blitz] permission_mode and auto_approve.
	Mode                PermissionMode
	AutoApproveCommands bool // tools.auto_approve_commands
}

// ErrNotApproved is returned when an action is denied or cannot be approved.
var ErrNotApproved = errors.New("action not approved")

// Hooks holds callbacks injected by the host application (TUI, engine) and the
// approval rules remembered for this session or persisted across sessions.
type Hooks struct {
	mu       sync.RWMutex
	policy   Policy
	approver Approver
	prompter UserPromptFunc
	invoker  InvokeAgentFunc
	session  map[string]bool
	store    *ApprovalStore
	audit    *audit.Logger
	rules    *PermissionRules
}

// SetRules attaches the permission rules the gate applies.
func (h *Hooks) SetRules(r *PermissionRules) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rules = r
}

// Rules returns the permission rules (nil when none are attached).
func (h *Hooks) Rules() *PermissionRules {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rules
}

// NewHooks creates hooks with the given approval policy.
func NewHooks(policy Policy) *Hooks { return &Hooks{policy: policy, session: map[string]bool{}} }

func (h *Hooks) SetApprover(a Approver) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.approver = a
}

func (h *Hooks) SetUserPrompter(p UserPromptFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prompter = p
}

func (h *Hooks) SetSubagentInvoker(i InvokeAgentFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.invoker = i
}

// SetStore attaches the persistent "always allow" rules.
func (h *Hooks) SetStore(s *ApprovalStore) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.store = s
}

// SetAudit attaches the audit log.
func (h *Hooks) SetAudit(l *audit.Logger) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.audit = l
}

// Audit returns the audit logger (possibly nil, which is a no-op).
func (h *Hooks) Audit() *audit.Logger {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.audit
}

func (h *Hooks) userPrompter() UserPromptFunc {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.prompter
}

func (h *Hooks) subagentInvoker() InvokeAgentFunc {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.invoker
}

// SessionRules returns the keys allowed for this session, sorted.
func (h *Hooks) SessionRules() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	keys := make([]string, 0, len(h.session))
	for k := range h.session {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Store returns the persistent rule store (may be nil).
func (h *Hooks) Store() *ApprovalStore {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.store
}

// RevokeSession forgets a session rule; it reports whether one existed.
func (h *Hooks) RevokeSession(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	ok := h.session[key]
	delete(h.session, key)
	return ok
}

// Approve returns nil when the action may proceed. It fails closed: with no
// approver configured and no auto-approve policy or remembered rule covering
// the action, the action is denied.
// unattendedKey carries an unattended run's decision function.
type unattendedKey struct{}

// Unattended marks ctx as an unattended run (a worker): approvals in it go
// only to decide, bypassing auto-approval, remembered and saved rules and
// the interactive approver, and nothing decided is remembered; questions to
// the user are refused. The run gets exactly what decide permits.
func Unattended(ctx context.Context, decide Approver) context.Context {
	return context.WithValue(ctx, unattendedKey{}, decide)
}

// isUnattended reports whether ctx is an unattended run.
func isUnattended(ctx context.Context) bool {
	_, ok := ctx.Value(unattendedKey{}).(Approver)
	return ok
}

func (h *Hooks) Approve(ctx context.Context, req ApprovalRequest) error {
	if h == nil {
		return fmt.Errorf("%w: no approval hooks configured", ErrNotApproved)
	}
	// Permission rules first: deny wins in every mode and run, and ask
	// forces the question below.
	h.mu.RLock()
	rules := h.rules
	h.mu.RUnlock()
	effect, rule := rules.Decide(ruleKind(req), req.Targets)
	if effect == EffectDeny {
		h.Audit().Log(audit.Entry{Kind: audit.KindDenial, Tool: req.Tool, Detail: req.Detail, Decision: "rule-deny " + rule.String()})
		return fmt.Errorf("%w: %s is denied by the permission rule deny %s", ErrNotApproved, req.Detail, rule)
	}
	mustAsk := req.MustAsk || effect == EffectAsk

	if decide, ok := ctx.Value(unattendedKey{}).(Approver); ok {
		if mustAsk { // an ask rule needs a person, and nobody is watching
			h.Audit().Log(audit.Entry{Kind: audit.KindDenial, Tool: req.Tool, Detail: req.Detail, Decision: "unattended-refused"})
			return fmt.Errorf("%w: %s needs to be asked about (an ask rule), and this run is unattended", ErrNotApproved, req.Detail)
		}
		decision, err := decide(ctx, req)
		allowed := err == nil && decision != DecisionDeny
		h.mu.RLock()
		log := h.audit
		h.mu.RUnlock()
		if allowed {
			log.Log(audit.Entry{Kind: audit.KindApproval, Tool: req.Tool, Detail: req.Detail, Decision: "unattended-permitted"})
			return nil
		}
		log.Log(audit.Entry{Kind: audit.KindDenial, Tool: req.Tool, Detail: req.Detail, Decision: "unattended-refused"})
		return fmt.Errorf("%w: %s isn't among this unattended run's permissions", ErrNotApproved, req.Detail)
	}
	h.mu.RLock()
	policy, approver, store, log := h.policy, h.approver, h.store, h.audit
	remembered := req.Key != "" && h.session[req.Key]
	h.mu.RUnlock()

	record := func(decision string) {
		kind := audit.KindApproval
		if decision == "deny" || decision == "no-approver" || decision == "error" {
			kind = audit.KindDenial
		}
		log.Log(audit.Entry{Kind: kind, Tool: req.Tool, Detail: req.Detail, Decision: decision})
	}

	switch {
	case mustAsk && policy.Mode == ModeDontAsk:
		record("mode-dont-ask")
		return fmt.Errorf("%w: %s must be asked about (an ask rule), and the permission mode is dont-ask", ErrNotApproved, req.Tool)
	case mustAsk && approver == nil:
		record("no-approver")
		return fmt.Errorf("%w: %s must be asked about (an ask rule), but no interactive approver is available", ErrNotApproved, req.Tool)
	case mustAsk:
		// Straight to the question: modes, allow rules and remembered
		// approvals don't apply.
	case effect == EffectAllow:
		record("rule-allow " + rule.String())
		return nil
	case policy.Mode == ModeBypass:
		record("mode-bypass")
		return nil
	case req.Kind == ActionCommand && policy.AutoApproveCommands:
		record("auto-policy")
		return nil
	case remembered:
		record("session-rule")
		return nil
	case req.Key != "" && store.Has(req.Key):
		record("saved-rule")
		return nil
	case policy.Mode == ModeAcceptEdits && (req.Kind == ActionWrite || req.Kind == ActionDelete):
		// The file tools resolved the paths as writable before asking.
		record("mode-accept-edits")
		return nil
	case policy.Mode == ModeDontAsk:
		record("mode-dont-ask")
		return fmt.Errorf("%w: %s would need approval, and the permission mode is dont-ask", ErrNotApproved, req.Tool)
	case approver == nil:
		record("no-approver")
		return fmt.Errorf("%w: %s requires approval but no interactive approver is available (allow it with a rule or a permission mode such as accept-edits)", ErrNotApproved, req.Tool)
	}

	// The span isolates time spent waiting for the user from tool runtime.
	actx, span := observability.Start(ctx, "approval",
		attribute.String("tool", req.Tool), attribute.String("kind", string(req.Kind)))
	decision, err := approver(actx, req)
	span.SetAttributes(attribute.String("decision", decision.String()))
	observability.End(span, err)
	if err != nil {
		record("error")
		return fmt.Errorf("%w: %v", ErrNotApproved, err)
	}
	record(decision.String())

	switch decision {
	case DecisionOnce:
		return nil
	case DecisionSession, DecisionAlways:
		if req.Key == "" {
			return nil
		}
		h.mu.Lock()
		h.session[req.Key] = true
		h.mu.Unlock()
		if decision == DecisionAlways && store != nil {
			if err := store.Add(req.Key, req.KeyLabel); err != nil {
				return fmt.Errorf("approved, but saving the rule failed: %w", err)
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: the user declined this %s", ErrNotApproved, req.Kind)
	}
}
