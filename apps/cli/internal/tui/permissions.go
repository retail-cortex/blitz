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

package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/pkg/i18n"
)

// cmdPermissions lists, adds or removes permission rules:
//
//	/permissions
//	/permissions allow|ask|deny <rule> [--save [--workspace]]
//	/permissions remove <rule> [--save [--workspace]]
//
// --save saves to the global settings, or with --workspace (which implies
// --save) to the workspace's own.
func cmdPermissions(args []string, app *App) {
	if len(args) == 0 {
		listPermissions(app)
		return
	}
	scope := api.ScopeSession
	switch {
	case slices.Contains(args, "--workspace"):
		scope = api.ScopeWorkspace
	case slices.Contains(args, "--save"):
		scope = api.ScopeGlobal
	}
	save := scope != api.ScopeSession
	args = slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--save" || a == "--workspace" })
	verb, rule := args[0], strings.TrimSpace(strings.Join(args[1:], " "))
	if rule == "" {
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("perm.usage"), Reset)
		return
	}
	switch verb {
	case "allow", "ask", "deny":
		res, err := app.Workspace.AddPermissionRule(verb, rule, scope)
		switch {
		case errors.Is(err, api.ErrBadRule):
			fmt.Printf("%s✗ %s%s\n", Red, safe(err.Error()), Reset)
		case err != nil:
			fmt.Printf("%s✗ %s%s\n", Red, i18n.T("perm.failed", "error", safe(err.Error())), Reset)
		case res.NextStart:
			fmt.Printf("%s%s%s\n", Yellow, i18n.T("perm.next_start", "effect", verb, "rule", safe(res.Rule)), Reset)
		default:
			fmt.Printf("%s%s%s\n", Green, i18n.T("perm.added", "effect", verb, "rule", safe(res.Rule)), Reset)
		}
		if save {
			printSaved(res.Saved)
		}
	case "remove", "rm":
		res, err := app.Workspace.RemovePermissionRule(rule, scope)
		if err != nil {
			fmt.Printf("%s✗ %s%s\n", Red, i18n.T("perm.failed", "error", safe(err.Error())), Reset)
			return
		}
		if res.Removed == 0 && !save {
			fmt.Printf("%s%s%s\n", Yellow, i18n.T("perm.not_found", "rule", safe(res.Rule)), Reset)
			return
		}
		fmt.Printf("%s%s%s\n", Green, i18n.T("perm.removed", "rule", safe(res.Rule)), Reset)
		if save {
			printSaved(res.Saved)
		}
	default:
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("perm.usage"), Reset)
	}
}

func listPermissions(app *App) {
	rules := app.Workspace.ListPermissionRules()
	fmt.Printf("\n%s%s%s\n", Bold, i18n.T("perm.title"), Reset)
	if !slices.ContainsFunc(rules, func(r api.PermissionRule) bool { return r.Source != "built-in" }) {
		fmt.Printf("  %s%s%s\n", Dim, i18n.T("perm.none"), Reset)
	}
	color := func(effect string) string {
		switch effect {
		case "deny":
			return Red
		case "ask":
			return Yellow
		}
		return Green
	}
	builtIn := map[string][]string{}
	for _, r := range rules {
		if r.Source == "built-in" { // one line each, below
			builtIn[r.Effect] = append(builtIn[r.Effect], strings.TrimSuffix(strings.TrimPrefix(r.Rule, "shell("), ")"))
			continue
		}
		fmt.Printf("  %s%-5s%s %s  %s(%s)%s\n", color(r.Effect), r.Effect, Reset, safe(r.Rule), Dim, r.Source, Reset)
	}
	for _, effect := range []string{"ask", "allow"} {
		if list := builtIn[effect]; len(list) > 0 {
			fmt.Printf("  %s%-5s%s %s\n", color(effect), effect, Reset, i18n.T("perm.builtin", "commands", safe(strings.Join(list, ", "))))
		}
	}
	fmt.Printf("  %s%s%s\n\n", Dim, i18n.T("perm.hint", "mode", app.Workspace.Settings().PermissionMode), Reset)
}
