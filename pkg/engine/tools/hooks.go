package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/observability"
	"go.opentelemetry.io/otel/attribute"
)

// InvokeAgentFunc runs a registered sub-agent with a prompt and returns its reply.
type InvokeAgentFunc func(ctx context.Context, agentName, prompt string) (string, error)

// Policy controls which actions skip the approval prompt.
type Policy struct {
	// Mode is the permission mode ("" is ModeDefault). NewRegistry sets
	// it from [blitz] permission_mode and auto_approve.
	Mode                api.PermissionMode
	AutoApproveCommands bool // tools.auto_approve_commands
}

// ErrNotApproved is returned when an action is denied or cannot be approved.
var ErrNotApproved = errors.New("action not approved")

// Hooks holds callbacks injected by the host application (TUI, engine) and the
// approval rules remembered for this session or persisted across sessions.
type Hooks struct {
	mu       sync.RWMutex
	policy   Policy
	approver api.Approver
	prompter api.UserPromptFunc
	invoker  InvokeAgentFunc
	session  map[string]bool
	store    *ApprovalStore
	audit    *audit.Logger
	rules    *PermissionRules
	// permHook runs permission_request hooks; notify runs notification
	// hooks (in the background). Nil: none.
	permHook func(context.Context, api.ApprovalRequest) Outcome
	notify   func(ctx context.Context, typ, message string)
}

// SetEventHooks connects the gate to permission_request and notification
// hooks.
func (h *Hooks) SetEventHooks(perm func(context.Context, api.ApprovalRequest) Outcome, notify func(ctx context.Context, typ, message string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.permHook, h.notify = perm, notify
}

// Notify fires notification hooks (e.g. a question waiting for the user).
func (h *Hooks) Notify(ctx context.Context, typ, message string) {
	h.mu.RLock()
	notify := h.notify
	h.mu.RUnlock()
	if notify != nil {
		notify(ctx, typ, message)
	}
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

func (h *Hooks) SetApprover(a api.Approver) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.approver = a
}

func (h *Hooks) SetUserPrompter(p api.UserPromptFunc) {
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

func (h *Hooks) userPrompter() api.UserPromptFunc {
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
func Unattended(ctx context.Context, decide api.Approver) context.Context {
	return context.WithValue(ctx, unattendedKey{}, decide)
}

// isUnattended reports whether ctx is an unattended run.
func isUnattended(ctx context.Context) bool {
	_, ok := ctx.Value(unattendedKey{}).(api.Approver)
	return ok
}

func (h *Hooks) Approve(ctx context.Context, req api.ApprovalRequest) error {
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

	if decide, ok := ctx.Value(unattendedKey{}).(api.Approver); ok {
		if mustAsk { // an ask rule needs a person, and nobody is watching
			h.Audit().Log(audit.Entry{Kind: audit.KindDenial, Tool: req.Tool, Detail: req.Detail, Decision: "unattended-refused"})
			return fmt.Errorf("%w: %s needs to be asked about (an ask rule), and this run is unattended", ErrNotApproved, req.Detail)
		}
		decision, err := decide(ctx, req)
		allowed := err == nil && decision != api.DecisionDeny
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
	case mustAsk && policy.Mode == api.ModeDontAsk:
		record("mode-dont-ask")
		return fmt.Errorf("%w: %s must be asked about (an ask rule), and the permission mode is dont-ask", ErrNotApproved, req.Tool)
	case mustAsk:
		// Straight to the question: modes, allow rules and remembered
		// approvals don't apply.
	case effect == EffectAllow:
		record("rule-allow " + rule.String())
		return nil
	case policy.Mode == api.ModeBypass:
		record("mode-bypass")
		return nil
	case req.Kind == api.ActionCommand && policy.AutoApproveCommands:
		record("auto-policy")
		return nil
	case remembered:
		record("session-rule")
		return nil
	case req.Key != "" && store.Has(req.Key):
		record("saved-rule")
		return nil
	case policy.Mode == api.ModeAcceptEdits && (req.Kind == api.ActionWrite || req.Kind == api.ActionDelete):
		// The file tools resolved the paths as writable before asking.
		record("mode-accept-edits")
		return nil
	case policy.Mode == api.ModeDontAsk:
		record("mode-dont-ask")
		return fmt.Errorf("%w: %s would need approval, and the permission mode is dont-ask", ErrNotApproved, req.Tool)
	}

	// The user's permission_request hooks may answer instead of the user.
	h.mu.RLock()
	permHook, notify := h.permHook, h.notify
	h.mu.RUnlock()
	if permHook != nil {
		switch out := permHook(ctx, req); out.Decision {
		case "allow":
			record("hook-allow")
			return nil
		case "deny":
			record("hook-deny")
			why := out.Reason
			if why == "" {
				why = "a permission_request hook denied it"
			}
			return fmt.Errorf("%w: %s", ErrNotApproved, why)
		}
	}
	if approver == nil {
		record("no-approver")
		if mustAsk {
			return fmt.Errorf("%w: %s must be asked about (an ask rule), but no interactive approver is available", ErrNotApproved, req.Tool)
		}
		return fmt.Errorf("%w: %s requires approval but no interactive approver is available (allow it with a rule or a permission mode such as accept-edits)", ErrNotApproved, req.Tool)
	}
	if notify != nil {
		notify(ctx, "permission_prompt", req.Detail)
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
	case api.DecisionOnce:
		return nil
	case api.DecisionSession, api.DecisionAlways:
		if req.Key == "" {
			return nil
		}
		h.mu.Lock()
		h.session[req.Key] = true
		h.mu.Unlock()
		if decision == api.DecisionAlways && store != nil {
			if err := store.Add(req.Key, req.KeyLabel); err != nil {
				return fmt.Errorf("approved, but saving the rule failed: %w", err)
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: the user declined this %s", ErrNotApproved, req.Kind)
	}
}
