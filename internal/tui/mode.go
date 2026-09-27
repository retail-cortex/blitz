package tui

import (
	"context"
	"errors"
	"fmt"

	core "github.com/retail-cortex/blitz/internal/app"
	"github.com/retail-cortex/blitz/internal/i18n"
	"github.com/retail-cortex/blitz/internal/tools"
)

// cmdMode shows or changes the permission mode (/mode [name]).
func cmdMode(args []string, app *App) {
	if len(args) == 0 {
		current := app.Workspace.Settings().PermissionMode
		fmt.Printf("%s\n", i18n.T("mode.current", "mode", current))
		for _, m := range tools.Modes {
			mark := "  "
			if string(m) == current {
				mark = Green + "● " + Reset
			}
			fmt.Printf("  %s%-13s %s%s%s\n", mark, m, Dim, i18n.T("mode.describe."+string(m)), Reset)
		}
		return
	}
	mode, err := app.Workspace.SetPermissionMode(args[0])
	switch {
	case errors.Is(err, core.ErrUnknownMode):
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("mode.usage"), Reset)
	case errors.Is(err, core.ErrBypassNeedsSandbox):
		fmt.Printf("%s✗ %s%s\n", Red, i18n.T("mode.bypass_unavailable"), Reset)
	case err != nil:
		fmt.Printf("%s✗ %s%s\n", Red, i18n.T("set.failed", "error", safe(err.Error())), Reset)
	default:
		fmt.Printf("%s%s%s\n", modeColor(mode), i18n.T("mode.set", "mode", mode), Reset)
	}
}

// modeColor is how the prompt and messages show a mode: red for bypass,
// yellow for the others that change what asks, nothing for default.
func modeColor(mode string) string {
	switch tools.PermissionMode(mode) {
	case tools.ModeBypass:
		return Red + Bold
	case tools.ModeDefault, "":
		return ""
	}
	return Yellow
}

// modeTag is the prompt's mark for a mode other than default.
func modeTag(mode string) string {
	if mode == "" || mode == string(tools.ModeDefault) {
		return ""
	}
	return fmt.Sprintf(" %s[%s]%s", modeColor(mode), mode, Reset)
}

// cmdEffort shows or sets the session's reasoning effort (/effort [level]).
func cmdEffort(ctx context.Context, args []string, app *App) {
	if len(args) == 0 {
		effort := app.Workspace.Settings().Effort
		if effort == "" {
			effort = i18n.T("effort.auto")
		}
		fmt.Printf("%s\n", i18n.T("effort.current", "effort", effort))
		return
	}
	if _, err := app.Workspace.Set(ctx, "effort", args[0]); err != nil {
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("effort.usage"), Reset)
		return
	}
	effort := app.Workspace.Settings().Effort
	if effort == "" {
		effort = i18n.T("effort.auto")
	}
	fmt.Printf("%s%s%s\n", Green, i18n.T("effort.set", "effort", effort), Reset)
}

// nextMode is the mode Shift+Tab switches to: default, accept-edits, plan,
// then bypass only when the session started in it (dont-ask is left for
// /mode), then default again.
func nextMode(current string, withBypass bool) string {
	switch tools.PermissionMode(current) {
	case tools.ModeDefault:
		return string(tools.ModeAcceptEdits)
	case tools.ModeAcceptEdits:
		return string(tools.ModePlan)
	case tools.ModePlan:
		if withBypass {
			return string(tools.ModeBypass)
		}
	}
	return string(tools.ModeDefault)
}

// cycleMode switches to the next mode (Shift+Tab) and reports whether it
// changed. Failing to enter bypass (the sandbox went away) skips it.
func cycleMode(app *App, withBypass bool) bool {
	current := app.Workspace.Settings().PermissionMode
	next := nextMode(current, withBypass)
	if _, err := app.Workspace.SetPermissionMode(next); err != nil {
		if next != string(tools.ModeBypass) {
			return false
		}
		if _, err := app.Workspace.SetPermissionMode(string(tools.ModeDefault)); err != nil {
			return false
		}
	}
	return app.Workspace.Settings().PermissionMode != current
}
