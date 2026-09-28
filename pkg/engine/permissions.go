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
	"errors"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
)

// ListPermissionRules returns the rules in force: deny, then ask, then
// allow. A rule from the settings is labelled with the file it's in:
// global, or the workspace's.
func (w *Workspace) ListPermissionRules() []api.PermissionRule {
	own, _, _ := config.ScopePermissions("", w.Dir())
	inWorkspace := func(effect, rule string) bool {
		list := map[string][]string{"allow": own.Allow, "ask": own.Ask, "deny": own.Deny}[effect]
		for _, r := range list {
			if c, err := config.ValidatePermissionRule(effect, r); err == nil && c == rule {
				return true
			}
		}
		return false
	}
	var out []api.PermissionRule
	for _, r := range w.tools.Rules().List() {
		source := r.Source
		if source == tools.SourceConfig {
			source = string(api.ScopeGlobal)
			if inWorkspace(string(r.Effect), r.String()) {
				source = string(api.ScopeWorkspace)
			}
		}
		out = append(out, api.PermissionRule{Effect: string(r.Effect), Rule: r.String(), Source: source})
	}
	return out
}

// AddPermissionRule adds a rule for this session and, with a scope, to
// that settings file's [permissions]: global, or the workspace's own. It
// applies at once, except read rules, which need saving and apply from the
// next start.
func (w *Workspace) AddPermissionRule(effect, rule string, save api.Scope) (api.PermissionChange, error) {
	r, err := tools.ParsePermissionRule(tools.Effect(effect), rule, tools.SourceSession)
	if err != nil {
		return api.PermissionChange{}, err
	}
	out := api.PermissionChange{Rule: r.String()}
	if r.Kind == tools.RuleRead {
		out.NextStart = true
		if save == api.ScopeSession {
			return out, errors.New("read rules become blocked paths, fixed when the workspace opens: save it (it applies from the next start)")
		}
	}
	if save != api.ScopeSession {
		// Saved, the rule comes back with the settings; only if saving
		// failed is it this session's.
		out.Saved.Path, _, out.Saved.Err = config.AddPermissionRule("", w.scopeDir(save), effect, out.Rule)
		if out.Saved.Err == nil {
			w.reloadSavedPermissions()
			return out, nil
		}
	}
	if r.Kind != tools.RuleRead {
		if err := w.tools.Rules().Add(r.Effect, rule, tools.SourceSession); err != nil {
			return api.PermissionChange{}, err
		}
	}
	return out, nil
}

// RemovePermissionRule removes a rule (any effect) for this session and,
// with a scope, from that settings file.
func (w *Workspace) RemovePermissionRule(rule string, save api.Scope) (api.PermissionChange, error) {
	out := api.PermissionChange{Rule: rule}
	if r, err := tools.ParsePermissionRule(tools.EffectDeny, rule, ""); err == nil {
		out.Rule = r.String()
	}
	out.Removed = w.tools.Rules().Remove(rule, "")
	if save != api.ScopeSession {
		out.Saved.Path, _, out.Saved.Err = config.RemovePermissionRule("", w.scopeDir(save), out.Rule)
		w.reloadSavedPermissions()
	}
	return out, nil
}

// scopeDir is the workspace argument config's functions take for a
// scope: "" for global, the workspace's directory for its own.
func (w *Workspace) scopeDir(s api.Scope) string {
	if s == api.ScopeWorkspace {
		return w.Dir()
	}
	return ""
}

// reloadSavedPermissions reads the saved rules again after a change here.
func (w *Workspace) reloadSavedPermissions() {
	if cfg, err := config.LoadWorkspace("", w.Dir()); err == nil {
		w.ReloadPermissions(cfg)
	}
}

// ReloadPermissions applies cfg's [permissions] (global and the
// workspace's, and the built-in read-only rules if on) in place of the
// ones from the settings: after a settings change while a session runs.
// Session and flag rules stay; read rules apply from the next start.
func (w *Workspace) ReloadPermissions(cfg *config.Config) error {
	w.cfg.Permissions = cfg.Permissions
	return w.tools.Rules().ReplaceConfigured(cfg.Permissions)
}
