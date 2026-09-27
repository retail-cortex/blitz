package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	core "github.com/retail-cortex/blitz/internal/app"
	"github.com/retail-cortex/blitz/internal/i18n"
)

// cmdPermissions lists, adds or removes permission rules:
//
//	/permissions
//	/permissions allow|ask|deny <rule> [--save]
//	/permissions remove <rule> [--save]
func cmdPermissions(args []string, app *App) {
	if len(args) == 0 {
		listPermissions(app)
		return
	}
	save := slices.Contains(args, "--save")
	args = slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "--save" })
	verb, rule := args[0], strings.TrimSpace(strings.Join(args[1:], " "))
	if rule == "" {
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("perm.usage"), Reset)
		return
	}
	switch verb {
	case "allow", "ask", "deny":
		res, err := app.Workspace.AddPermissionRule(verb, rule, save)
		switch {
		case errors.Is(err, core.ErrBadRule):
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
		res, _ := app.Workspace.RemovePermissionRule(rule, save)
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
	if len(rules) == 0 {
		fmt.Printf("  %s%s%s\n", Dim, i18n.T("perm.none"), Reset)
	}
	for _, r := range rules {
		color := Green
		switch r.Effect {
		case "deny":
			color = Red
		case "ask":
			color = Yellow
		}
		fmt.Printf("  %s%-5s%s %s  %s(%s)%s\n", color, r.Effect, Reset, safe(r.Rule), Dim, r.Source, Reset)
	}
	fmt.Printf("  %s%s%s\n\n", Dim, i18n.T("perm.hint", "mode", app.Workspace.Settings().PermissionMode), Reset)
}
