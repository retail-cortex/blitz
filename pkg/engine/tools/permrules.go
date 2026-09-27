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
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// Effect is what a permission rule does.
type Effect string

const (
	// EffectAllow lets a matching action run without asking.
	EffectAllow Effect = "allow"
	// EffectAsk makes a matching action ask even when a mode, a
	// remembered approval or an allow rule would let it through.
	EffectAsk Effect = "ask"
	// EffectDeny refuses a matching action, in every mode.
	EffectDeny Effect = "deny"
)

// Rule kinds: what a rule's pattern is matched against.
const (
	RuleShell  = "shell"  // a command, with the command policy's pattern syntax
	RuleRead   = "read"   // a path glob (deny only: it becomes a blocked path)
	RuleWrite  = "write"  // a path glob for creates and edits
	RuleDelete = "delete" // a path glob for deletions
	RuleWeb    = "web"    // a host glob; *.example.com also matches example.com
	RuleSearch = "search" // a web search provider
	RuleMCP    = "mcp"    // server:tool, globs allowed
	RuleSkill  = "skill"  // a skill name
	RuleAgent  = "agent"  // an agent name, for invoke_agent
	RuleTool   = "tool"   // a bare tool name, e.g. web_search
)

var ruleKinds = []string{RuleShell, RuleRead, RuleWrite, RuleDelete, RuleWeb, RuleSearch, RuleMCP, RuleSkill, RuleAgent}

// PermissionRule is one allow, ask or deny rule.
type PermissionRule struct {
	Effect  Effect
	Kind    string
	Pattern string // "" for a bare tool name
	// Source says where the rule came from: "config", "flag", "session".
	Source string

	cmd []cmdPattern     // shell
	re  []*regexp.Regexp // paths and names
}

// String is the rule as written: kind(pattern), or the bare tool name.
func (r PermissionRule) String() string {
	if r.Kind == RuleTool {
		return r.Pattern
	}
	return r.Kind + "(" + r.Pattern + ")"
}

// ParsePermissionRule reads "kind(pattern)" or a bare tool name. "Bash"
// and "Edit"/"Write" are accepted for shell and write, as Claude Code
// spells them.
func ParsePermissionRule(effect Effect, text, source string) (PermissionRule, error) {
	s := strings.TrimSpace(text)
	r := PermissionRule{Effect: effect, Source: source}
	if effect != EffectAllow && effect != EffectAsk && effect != EffectDeny {
		return r, fmt.Errorf("%w: effect %q (allow, ask or deny)", api.ErrBadRule, effect)
	}
	open := strings.IndexByte(s, '(')
	switch {
	case s == "":
		return r, fmt.Errorf("%w: empty", api.ErrBadRule)
	case open < 0:
		if !validToolName.MatchString(s) {
			return r, fmt.Errorf("%w %q: use kind(pattern) or a tool name", api.ErrBadRule, text)
		}
		r.Kind, r.Pattern = RuleTool, s
		if k := kindAlias(s); k != "" { // "shell" alone means every command
			r.Kind, r.Pattern = k, "*"
		}
	case !strings.HasSuffix(s, ")"):
		return r, fmt.Errorf("%w %q: missing )", api.ErrBadRule, text)
	default:
		r.Kind = kindAlias(s[:open])
		r.Pattern = strings.TrimSpace(s[open+1 : len(s)-1])
		if r.Kind == "" {
			return r, fmt.Errorf("%w %q: unknown kind %q (%s)", api.ErrBadRule, text, s[:open], strings.Join(ruleKinds, ", "))
		}
		if r.Pattern == "" {
			return r, fmt.Errorf("%w %q: empty pattern", api.ErrBadRule, text)
		}
	}
	if r.Kind == RuleRead && effect != EffectDeny {
		return r, fmt.Errorf("%w %q: reading never asks, so only deny read(...) rules apply", api.ErrBadRule, text)
	}
	if err := r.compile(); err != nil {
		return r, fmt.Errorf("%w %q: %v", api.ErrBadRule, text, err)
	}
	return r, nil
}

var validToolName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

func kindAlias(k string) string {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "shell", "bash", "command", "cmd":
		return RuleShell
	case "read":
		return RuleRead
	case "write", "edit":
		return RuleWrite
	case "delete":
		return RuleDelete
	case "web", "webfetch", "fetch":
		return RuleWeb
	case "search", "websearch":
		return RuleSearch
	case "mcp":
		return RuleMCP
	case "skill":
		return RuleSkill
	case "agent":
		return RuleAgent
	}
	return ""
}

func (r *PermissionRule) compile() error {
	switch r.Kind {
	case RuleShell:
		ps, err := compileCmdPatterns([]string{r.Pattern})
		r.cmd = ps
		return err
	case RuleTool:
		return nil
	case RuleRead, RuleWrite, RuleDelete:
		g := filepath.ToSlash(config.ExpandHome(r.Pattern))
		g = strings.TrimPrefix(strings.TrimSuffix(g, "/"), "./")
		// A directory pattern covers what's under it.
		re, err := regexp.Compile(anchor("^" + textutil.GlobToRegex(g) + "(/.*)?$"))
		if err != nil {
			return err
		}
		r.re = []*regexp.Regexp{re}
	default: // web, search, mcp, skill, agent
		g := strings.ToLower(r.Pattern)
		exprs := []string{"^" + nameGlob(g) + "$"}
		if r.Kind == RuleWeb && strings.HasPrefix(g, "*.") {
			exprs = append(exprs, "^"+regexp.QuoteMeta(g[2:])+"$")
		}
		for _, e := range exprs {
			re, err := regexp.Compile(e)
			if err != nil {
				return err
			}
			r.re = append(r.re, re)
		}
	}
	return nil
}

func anchor(expr string) string {
	if caseInsensitiveFS {
		return "(?i)" + expr
	}
	return expr
}

// nameGlob turns a name glob (* and ?) into a regexp body; "/" and ":"
// are ordinary characters here.
func nameGlob(g string) string {
	var sb strings.Builder
	for _, c := range g {
		switch c {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return sb.String()
}

// matches reports whether the rule covers one target of its kind.
func (r PermissionRule) matches(target string) bool {
	switch r.Kind {
	case RuleTool:
		return strings.EqualFold(r.Pattern, target)
	case RuleRead, RuleWrite, RuleDelete:
		t := filepath.ToSlash(target)
		for _, re := range r.re {
			if re.MatchString(t) {
				return true
			}
		}
		return false
	default:
		t := strings.ToLower(strings.TrimSuffix(target, "."))
		for _, re := range r.re {
			if re.MatchString(t) {
				return true
			}
		}
		return false
	}
}

// PermissionRules are the rules in force, from configuration, flags and
// the session. They are safe for concurrent use and can change while a
// session runs.
type PermissionRules struct {
	mu    sync.RWMutex
	rules []PermissionRule
}

// NewPermissionRules parses the configured rules; source labels them.
func NewPermissionRules(cfg config.PermissionsConfig, source string) (*PermissionRules, error) {
	pr := &PermissionRules{}
	var errs []error
	for effect, list := range map[Effect][]string{EffectAllow: cfg.Allow, EffectAsk: cfg.Ask, EffectDeny: cfg.Deny} {
		for _, text := range list {
			if err := pr.Add(effect, text, source); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return pr, errors.Join(errs...)
}

// Add adds a rule (a duplicate is ignored).
func (p *PermissionRules) Add(effect Effect, text, source string) error {
	r, err := ParsePermissionRule(effect, text, source)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, x := range p.rules {
		if x.Effect == r.Effect && x.String() == r.String() {
			return nil
		}
	}
	p.rules = append(p.rules, r)
	return nil
}

// Remove removes the rules written as text (any effect unless one is
// given) and reports how many.
func (p *PermissionRules) Remove(text string, effect Effect) int {
	want, err := ParsePermissionRule(cmpEffect(effect), text, "")
	key := strings.TrimSpace(text)
	if err == nil {
		key = want.String()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	p.rules = slices.DeleteFunc(p.rules, func(r PermissionRule) bool {
		if r.String() == key && (effect == "" || r.Effect == effect) {
			n++
			return true
		}
		return false
	})
	return n
}

func cmpEffect(e Effect) Effect {
	if e == "" {
		return EffectDeny // any effect parses the same pattern
	}
	return e
}

// List returns the rules, deny first, then ask, then allow.
func (p *PermissionRules) List() []PermissionRule {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := slices.Clone(p.rules)
	order := map[Effect]int{EffectDeny: 0, EffectAsk: 1, EffectAllow: 2}
	slices.SortStableFunc(out, func(a, b PermissionRule) int { return order[a.Effect] - order[b.Effect] })
	return out
}

// Patterns returns the patterns of kind with effect (e.g. read denies,
// which become blocked paths).
func (p *PermissionRules) Patterns(kind string, effect Effect) []string {
	var out []string
	for _, r := range p.List() {
		if r.Kind == kind && r.Effect == effect {
			out = append(out, r.Pattern)
		}
	}
	return out
}

// Decide returns the effect for an action of kind on targets: deny if any
// target matches a deny rule, else ask if any matches an ask rule, else
// allow if every target matches an allow rule, else "" (no rule applies).
// The rule that decided is returned too.
func (p *PermissionRules) Decide(kind string, targets []string) (Effect, *PermissionRule) {
	if p == nil || len(targets) == 0 {
		return "", nil
	}
	rules := p.List()
	first := func(effect Effect, t string) *PermissionRule {
		for i := range rules {
			if rules[i].Effect == effect && rules[i].Kind == kind && rules[i].matches(t) {
				return &rules[i]
			}
		}
		return nil
	}
	for _, effect := range []Effect{EffectDeny, EffectAsk} {
		for _, t := range targets {
			if r := first(effect, t); r != nil {
				return effect, r
			}
		}
	}
	var last *PermissionRule
	for _, t := range targets {
		r := first(EffectAllow, t)
		if r == nil {
			return "", nil
		}
		last = r
	}
	return EffectAllow, last
}

// shellPatterns returns the compiled shell patterns with effect, for the
// command policy.
func (p *PermissionRules) shellPatterns(effect Effect) []cmdPattern {
	var out []cmdPattern
	for _, r := range p.List() {
		if r.Kind == RuleShell && r.Effect == effect {
			for _, c := range r.cmd {
				out = append(out, cmdPattern{raw: r.String(), re: c.re})
			}
		}
	}
	return out
}

// ruleKind is the kind of rule that governs an approval request.
func ruleKind(req api.ApprovalRequest) string {
	switch req.Kind {
	case api.ActionWrite:
		return RuleWrite
	case api.ActionDelete:
		return RuleDelete
	case api.ActionMCP:
		return RuleMCP
	case api.ActionNetwork:
		if req.Tool == "web_search" {
			return RuleSearch
		}
		return RuleWeb
	case api.ActionCommand:
		return RuleShell
	}
	return ""
}

// ToolDenied returns the deny rule that refuses a tool call outright (by
// tool name, or a skill or agent it names), or nil.
func (p *PermissionRules) ToolDenied(tool string, args map[string]any) *PermissionRule {
	if p == nil {
		return nil
	}
	if eff, r := p.Decide(RuleTool, []string{tool}); eff == EffectDeny {
		return r
	}
	name := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	switch tool {
	case "activate_skill", "run_skill_script":
		if s := name("skill_name", "skill", "name"); s != "" {
			if eff, r := p.Decide(RuleSkill, []string{s}); eff == EffectDeny {
				return r
			}
		}
	case "invoke_agent":
		if a := name("agent_name", "agent"); a != "" {
			if eff, r := p.Decide(RuleAgent, []string{a}); eff == EffectDeny {
				return r
			}
		}
	}
	return nil
}
