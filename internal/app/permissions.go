package app

import (
	"errors"
	"slices"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/internal/config"
	"github.com/retail-cortex/blitz/internal/tools"
)

// ListPermissionRules returns the rules in force: deny, then ask, then
// allow.
func (w *Workspace) ListPermissionRules() []api.PermissionRule {
	var out []api.PermissionRule
	for _, r := range w.tools.Rules().List() {
		out = append(out, api.PermissionRule{Effect: string(r.Effect), Rule: r.String(), Source: r.Source})
	}
	return out
}

// AddPermissionRule adds a rule for this session and, with save, to the
// config file's [permissions]. It applies at once, except read rules,
// which need saving and apply from the next start.
func (w *Workspace) AddPermissionRule(effect, rule string, save bool) (api.PermissionChange, error) {
	r, err := tools.ParsePermissionRule(tools.Effect(effect), rule, "session")
	if err != nil {
		return api.PermissionChange{}, err
	}
	out := api.PermissionChange{Rule: r.String()}
	if r.Kind == tools.RuleRead {
		out.NextStart = true
		if !save {
			return out, errors.New("read rules become blocked paths, fixed when the workspace opens: save it (it applies from the next start)")
		}
	} else if err := w.tools.Rules().Add(r.Effect, rule, "session"); err != nil {
		return api.PermissionChange{}, err
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
func (w *Workspace) RemovePermissionRule(rule string, save bool) (api.PermissionChange, error) {
	out := api.PermissionChange{Rule: rule}
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
