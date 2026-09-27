package tools

import (
	"errors"
	"fmt"
	"strings"
)

// PermissionMode decides which actions run without asking.
type PermissionMode string

const (
	// ModeDefault asks for every sensitive action not covered by a rule.
	ModeDefault PermissionMode = "default"
	// ModeAcceptEdits runs file creates, edits and deletes inside the
	// writable roots without asking; commands and the rest ask as usual.
	ModeAcceptEdits PermissionMode = "accept-edits"
	// ModePlan refuses every tool that could change something, for every
	// prompt, until the mode changes (applied by the workspace).
	ModePlan PermissionMode = "plan"
	// ModeDontAsk denies whatever would ask: only rules and saved
	// approvals let actions through (for CI and scripts).
	ModeDontAsk PermissionMode = "dont-ask"
	// ModeBypass asks for nothing. Deny rules, blocked paths and the
	// sandboxes still apply, and the registry allows it only while the
	// OS sandbox is active.
	ModeBypass PermissionMode = "bypass"
)

// Modes are the permission modes in Shift+Tab order; bypass is last and
// is only offered when the session started in it.
var Modes = []PermissionMode{ModeDefault, ModeAcceptEdits, ModePlan, ModeDontAsk, ModeBypass}

// ErrUnknownMode reports a name that isn't a permission mode.
var ErrUnknownMode = errors.New("unknown permission mode")

// ErrBypassNeedsSandbox refuses bypass mode without an active OS sandbox.
var ErrBypassNeedsSandbox = errors.New("bypass mode needs the OS sandbox (sandbox.shell), which isn't active")

// ParsePermissionMode reads a mode name, accepting Claude Code's spellings
// (acceptEdits, dontAsk, bypassPermissions) too.
func ParsePermissionMode(s string) (PermissionMode, error) {
	k := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.TrimSpace(s)))
	switch k {
	case "", "default", "ask", "manual":
		return ModeDefault, nil
	case "acceptedits", "edits":
		return ModeAcceptEdits, nil
	case "plan":
		return ModePlan, nil
	case "dontask", "deny":
		return ModeDontAsk, nil
	case "bypass", "bypasspermissions":
		return ModeBypass, nil
	}
	return "", fmt.Errorf("%w %q (use default, accept-edits, plan, dont-ask or bypass)", ErrUnknownMode, s)
}

// Mode returns the permission mode.
func (h *Hooks) Mode() PermissionMode {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.policy.Mode == "" {
		return ModeDefault
	}
	return h.policy.Mode
}

// setMode changes the mode; Registry.SetPermissionMode checks bypass's
// requirement first.
func (h *Hooks) setMode(m PermissionMode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.policy.Mode = m
}

// SetPermissionMode changes the permission mode, refusing bypass unless
// the OS sandbox is active.
func (r *Registry) SetPermissionMode(m PermissionMode) error {
	if _, err := ParsePermissionMode(string(m)); err != nil {
		return err
	}
	if m == ModeBypass && !r.ShellSandbox().Active() {
		return ErrBypassNeedsSandbox
	}
	r.hooks.setMode(m)
	return nil
}
