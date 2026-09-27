package app

import (
	"errors"
	"slices"

	"github.com/retail-cortex/blitz/internal/config"
	"github.com/retail-cortex/blitz/internal/tools"
)

// PermissionRule is an allow, ask or deny rule in force.
type PermissionRule struct {
	Effect string // allow, ask or deny
	Rule   string // kind(pattern), or a tool name
	Source string // config, session
}

// ErrBadRule reports a permission rule that can't be parsed.
var ErrBadRule = tools.ErrBadRule

// ListPermissionRules returns the rules in force: deny, then ask, then
// allow.
func (w *Workspace) ListPermissionRules() []PermissionRule {
	var out []PermissionRule
	for _, r := range w.tools.Rules().List() {
		out = append(out, PermissionRule{Effect: string(r.Effect), Rule: r.String(), Source: r.Source})
	}
	return out
}

// PermissionChange is what AddPermissionRule or RemovePermissionRule did.
type PermissionChange struct {
	Rule    string // canonical form
	Removed int    // RemovePermissionRule: how many rules went
	// NextStart: the rule is saved but only applies from the next start
	// (read rules become blocked paths, which are fixed at start).
	NextStart bool
	Saved     Saved // empty unless saving was asked for
}

// AddPermissionRule adds a rule for this session and, with save, to the
// config file's [permissions]. It applies at once, except read rules,
// which need saving and apply from the next start.
func (w *Workspace) AddPermissionRule(effect, rule string, save bool) (PermissionChange, error) {
	r, err := tools.ParsePermissionRule(tools.Effect(effect), rule, "session")
	if err != nil {
		return PermissionChange{}, err
	}
	out := PermissionChange{Rule: r.String()}
	if r.Kind == tools.RuleRead {
		out.NextStart = true
		if !save {
			return out, errors.New("read rules become blocked paths, fixed when the workspace opens: save it (it applies from the next start)")
		}
	} else if err := w.tools.Rules().Add(r.Effect, rule, "session"); err != nil {
		return PermissionChange{}, err
	}
	if save {
		list := w.permList(effect)
		if !slices.Contains(*list, out.Rule) {
			*list = append(*list, out.Rule)
		}
		out.Saved.Path, out.Saved.Err = config.SavePermissionRules(config.ConfigDir(""), effect, *list)
	}
	return out, nil
}

// RemovePermissionRule removes a rule (any effect) for this session and,
// with save, from the config file.
func (w *Workspace) RemovePermissionRule(rule string, save bool) (PermissionChange, error) {
	out := PermissionChange{Rule: rule}
	if r, err := tools.ParsePermissionRule(tools.EffectDeny, rule, ""); err == nil {
		out.Rule = r.String()
	}
	out.Removed = w.tools.Rules().Remove(rule, "")
	if save {
		for _, effect := range []string{"allow", "ask", "deny"} {
			list := w.permList(effect)
			if i := slices.Index(*list, out.Rule); i >= 0 {
				*list = slices.Delete(*list, i, i+1)
				out.Saved.Path, out.Saved.Err = config.SavePermissionRules(config.ConfigDir(""), effect, *list)
			}
		}
	}
	return out, nil
}

func (w *Workspace) permList(effect string) *[]string {
	switch effect {
	case "allow":
		return &w.cfg.Permissions.Allow
	case "ask":
		return &w.cfg.Permissions.Ask
	}
	return &w.cfg.Permissions.Deny
}
