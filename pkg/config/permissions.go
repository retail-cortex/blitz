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

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// ValidatePermissionRule checks a rule and returns its canonical form. The
// rule parser (pkg/engine/tools, which depends on this package) sets it;
// unset, rules are taken as written.
var ValidatePermissionRule func(effect, rule string) (string, error)

func validRule(effect, rule string) (string, error) {
	if ValidatePermissionRule == nil {
		return strings.TrimSpace(rule), nil
	}
	return ValidatePermissionRule(effect, rule)
}

// validRules checks every rule in p, reporting each invalid one.
func validRules(p PermissionsConfig) error {
	var errs []error
	for effect, list := range map[string][]string{"allow": p.Allow, "ask": p.Ask, "deny": p.Deny} {
		for _, r := range list {
			if _, err := validRule(effect, r); err != nil {
				errs = append(errs, fmt.Errorf("[permissions] %s: %w", effect, err))
			}
		}
	}
	return errors.Join(errs...)
}

// ScopePermissions returns the [permissions] a scope's own settings file
// sets (workspace "" is global; a workspace's add to the global ones), and
// the file.
func ScopePermissions(prefixDir, workspace string) (PermissionsConfig, string, error) {
	dir := scopeDir(prefixDir, workspace)
	if dir == "" {
		return PermissionsConfig{}, "", errors.New("no settings directory")
	}
	path := filepath.Join(dir, ".env.toml")
	var f struct {
		Permissions PermissionsConfig `toml:"permissions"`
	}
	if _, err := toml.DecodeFile(path, &f); err != nil && !errors.Is(err, os.ErrNotExist) {
		return PermissionsConfig{}, path, fmt.Errorf("%s: %w", path, err)
	}
	return f.Permissions, path, nil
}

// AddPermissionRule checks a rule and adds it to a scope's file, returning
// the file and the rule's canonical form. A rule already there is kept.
func AddPermissionRule(prefixDir, workspace, effect, rule string) (path, canonical string, err error) {
	if canonical, err = validRule(effect, rule); err != nil {
		return "", "", err
	}
	p, path, err := ScopePermissions(prefixDir, workspace)
	if err != nil {
		return path, canonical, err
	}
	list := permList(&p, effect)
	if list == nil {
		return path, canonical, fmt.Errorf("unknown permission effect %q (allow, ask or deny)", effect)
	}
	if slices.Contains(*list, canonical) {
		return path, canonical, nil
	}
	path, err = SavePermissionRules(scopeDir(prefixDir, workspace), effect, append(*list, canonical))
	return path, canonical, err
}

// RemovePermissionRule removes a rule, as written or in its canonical
// form, from every list in a scope's file, returning the file and how many
// went.
func RemovePermissionRule(prefixDir, workspace, rule string) (path string, removed int, err error) {
	p, path, err := ScopePermissions(prefixDir, workspace)
	if err != nil {
		return path, 0, err
	}
	want := []string{strings.TrimSpace(rule)}
	if c, err := validRule("deny", rule); err == nil {
		want = append(want, c)
	}
	for _, effect := range []string{"allow", "ask", "deny"} {
		list := permList(&p, effect)
		kept := slices.DeleteFunc(slices.Clone(*list), func(r string) bool { return slices.Contains(want, strings.TrimSpace(r)) })
		if n := len(*list) - len(kept); n > 0 {
			removed += n
			if path, err = SavePermissionRules(scopeDir(prefixDir, workspace), effect, kept); err != nil {
				return path, removed, err
			}
		}
	}
	return path, removed, nil
}

// SetReadOnlyDefaults turns the built-in read-only rules on or off in a
// scope, or (nil) leaves the choice to the global settings.
func SetReadOnlyDefaults(prefixDir, workspace string, on *bool) (string, error) {
	return editConfigFile(scopeDir(prefixDir, workspace),
		func(doc string) string {
			if on == nil {
				return removeTOMLKey(doc, "permissions", "read_only_defaults")
			}
			return setTOMLKey(doc, "permissions", "read_only_defaults", strconv.FormatBool(*on))
		},
		func(check map[string]any) error {
			got := lookup(check, "permissions", "read_only_defaults")
			if (on == nil && got != nil) || (on != nil && got != *on) {
				return errors.New("could not set [permissions] read_only_defaults")
			}
			return nil
		})
}

func permList(p *PermissionsConfig, effect string) *[]string {
	switch effect {
	case "allow":
		return &p.Allow
	case "ask":
		return &p.Ask
	case "deny":
		return &p.Deny
	}
	return nil
}

// SavePermissionRules writes one [permissions] list (allow, ask or deny)
// in dir/.env.toml, replacing that key's line only; an empty list removes
// it. Comments and other settings are kept. Every rule is checked first.
func SavePermissionRules(dir, effect string, rules []string) (string, error) {
	switch effect {
	case "allow", "ask", "deny":
	default:
		return "", fmt.Errorf("unknown permission effect %q", effect)
	}
	for _, r := range rules {
		if _, err := validRule(effect, r); err != nil {
			return "", err
		}
	}
	quoted := make([]string, len(rules))
	for i, r := range rules {
		quoted[i] = strconv.Quote(r)
	}
	return editConfigFile(dir,
		func(doc string) string {
			if len(rules) == 0 {
				return removeTOMLKey(doc, "permissions", effect)
			}
			return setTOMLKey(doc, "permissions", effect, "["+strings.Join(quoted, ", ")+"]")
		},
		func(check map[string]any) error {
			perms, _ := check["permissions"].(map[string]any)
			var got []string
			if list, ok := perms[effect].([]any); ok {
				for _, v := range list {
					s, _ := v.(string)
					got = append(got, s)
				}
			}
			if !slices.Equal(got, rules) {
				return errors.New("could not update [permissions] " + effect)
			}
			return nil
		})
}
