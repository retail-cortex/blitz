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

package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// PermissionRule is an allow, ask or deny rule in force.
type PermissionRule struct {
	Effect string // allow, ask or deny
	Rule   string // kind(pattern), or a tool name
	// Source: "global" or "workspace" (the settings file it's in),
	// "built-in" (read_only_defaults), "flag" or "session".
	Source string
}

// Scope is where a permission rule is saved: ScopeSession (nowhere: this
// session only), ScopeGlobal (~/.blitz/.env.toml) or ScopeWorkspace (the
// workspace's own settings, which add to the global ones).
type Scope string

// The scopes a permission rule can be saved in.
const (
	ScopeSession   Scope = ""
	ScopeGlobal    Scope = "global"
	ScopeWorkspace Scope = "workspace"
)

// PermissionChange is what AddPermissionRule or RemovePermissionRule did.
type PermissionChange struct {
	Rule    string // canonical form
	Removed int    // RemovePermissionRule: how many rules went
	// NextStart: the rule is saved but only applies from the next start
	// (read rules become blocked paths, which are fixed at start).
	NextStart bool
	Saved     Saved // empty unless saving was asked for
}

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

// The answers to an approval request.
const (
	DecisionDeny    Decision = iota
	DecisionOnce             // allow this one action
	DecisionSession          // allow actions with the same Key until exit
	DecisionAlways           // allow actions with the same Key, persisted
)

// String is the decision's name: deny, once, session or always.
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

// UserPromptFunc asks the user a question and returns the answer. With
// WithMultiSelect, the user may choose several options: the answer is
// them, one per line.
type UserPromptFunc func(ctx context.Context, question string, options []string) (string, error)

type multiSelectKey struct{}

// WithMultiSelect marks a question asked with ctx as taking several of its
// options (ask_user_question's multi_select).
func WithMultiSelect(ctx context.Context) context.Context {
	return context.WithValue(ctx, multiSelectKey{}, true)
}

// IsMultiSelect reports whether a question asked with ctx takes several
// options.
func IsMultiSelect(ctx context.Context) bool {
	v, _ := ctx.Value(multiSelectKey{}).(bool)
	return v
}

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
	// ModeAuto sends what would ask to a reviewer model instead, which
	// allows or denies it with a reason; ask rules still ask the user
	// (spec_parity_027 PAR-PERM-20).
	ModeAuto PermissionMode = "auto"
	// ModeBypass asks for nothing. Deny rules, blocked paths and the
	// sandboxes still apply, and the registry allows it only while the
	// OS sandbox is active.
	ModeBypass PermissionMode = "bypass"
)

// Modes are the permission modes in Shift+Tab order; bypass is last and
// is only offered when the session started in it.
var Modes = []PermissionMode{ModeDefault, ModeAcceptEdits, ModeAuto, ModePlan, ModeDontAsk, ModeBypass}

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
	case "auto":
		return ModeAuto, nil
	case "bypass", "bypasspermissions":
		return ModeBypass, nil
	}
	return "", fmt.Errorf("%w %q (use default, accept-edits, auto, plan, dont-ask or bypass)", ErrUnknownMode, s)
}

// ErrBadRule reports a rule that can't be parsed.
var ErrBadRule = errors.New("invalid permission rule")
