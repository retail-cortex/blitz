package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/internal/i18n"
)

// rewindModeArgs are the modes as /rewind takes them.
var rewindModeArgs = map[string]api.RewindMode{
	"both": api.RewindBoth, "conversation": api.RewindConversation, "code": api.RewindCode,
	"summarize-from": api.RewindSummarizeFrom, "summarize-up-to": api.RewindSummarizeUpTo,
}

// cmdRewind takes the session back to one of its prompts (/rewind, or Esc
// Esc at an empty prompt). On a terminal it offers the prompts, newest
// first, then what to restore; otherwise it lists them, and
// "/rewind <n> [mode] [--force]" picks one (n counts back from the latest).
func cmdRewind(ctx context.Context, args []string, app *App) {
	points, err := app.Workspace.RewindPoints()
	if err != nil {
		fmt.Printf("%s✗ %v%s\n", Red, err, Reset)
		return
	}
	if len(points) == 0 {
		fmt.Println(i18n.T("rewind.none"))
		return
	}
	force := false
	var rest []string
	for _, a := range args {
		if a == "--force" || a == "-f" {
			force = true
		} else {
			rest = append(rest, a)
		}
	}
	picker, onTTY := terminalPicker(app)

	var point api.RewindPoint
	switch {
	case len(rest) > 0:
		n, err := strconv.Atoi(rest[0])
		if err != nil || n < 1 || n > len(points) {
			fmt.Printf("%s%s%s\n", Yellow, i18n.T("rewind.usage"), Reset)
			return
		}
		point = points[len(points)-n]
	case onTTY:
		items := make([]PickItem, len(points))
		for i := range points {
			p := points[len(points)-1-i]
			items[i] = PickItem{Label: oneLine(p.Text), Detail: rewindDetail(p)}
		}
		i, err := picker.Pick(ctx, i18n.T("rewind.pick_prompt"), items, 0)
		if err != nil {
			return
		}
		point = points[len(points)-1-i]
	default:
		fmt.Printf("\n%s%s%s\n", Bold, i18n.T("rewind.title"), Reset)
		for i := len(points) - 1; i >= 0; i-- {
			fmt.Printf("  %s%d%s  %s  %s%s%s\n", Bold, len(points)-i, Reset, clip(oneLine(points[i].Text), 60), Dim, rewindDetail(points[i]), Reset)
		}
		fmt.Printf("%s%s%s\n\n", Dim, i18n.T("rewind.usage"), Reset)
		return
	}

	// What to restore: the code only if something from here on changed files.
	later := false
	for _, p := range points {
		if p.Index >= point.Index && len(p.Files) > 0 {
			later = true
		}
	}
	var mode api.RewindMode
	if len(rest) > 1 {
		m, ok := rewindModeArgs[rest[1]]
		if !ok {
			fmt.Printf("%s%s%s\n", Yellow, i18n.T("rewind.usage"), Reset)
			return
		}
		mode = m
	} else {
		var modes []api.RewindMode
		for _, m := range api.RewindModes {
			code := m == api.RewindBoth || m == api.RewindCode
			if code && !later || m != api.RewindCode && !point.Conversation {
				continue
			}
			modes = append(modes, m)
		}
		if !onTTY || len(modes) == 0 {
			switch {
			case !point.Conversation:
				mode = api.RewindCode
			case !later:
				mode = api.RewindConversation
			default:
				mode = api.RewindBoth
			}
		} else {
			items := make([]PickItem, len(modes))
			for i, m := range modes {
				items[i] = PickItem{Label: i18n.T("rewind.mode." + string(m))}
			}
			i, err := picker.Pick(ctx, i18n.T("rewind.pick_mode"), items, 0)
			if err != nil {
				return
			}
			mode = modes[i]
		}
	}

	if mode == api.RewindSummarizeFrom || mode == api.RewindSummarizeUpTo {
		fmt.Printf("%s%s%s\n", Dim, i18n.T("compact.running"), Reset)
	}
	res, err := app.Workspace.Rewind(ctx, point.Index, mode, force)
	if errors.Is(err, api.ErrUndoConflict) && onTTY {
		fmt.Printf("%s!  %v%s\n", Yellow, err, Reset)
		items := []PickItem{{Label: i18n.T("rewind.overwrite")}, {Label: i18n.T("rewind.keep")}}
		if i, perr := picker.Pick(ctx, i18n.T("rewind.conflict"), items, 1); perr != nil || i != 0 {
			return
		}
		res, err = app.Workspace.Rewind(ctx, point.Index, mode, true)
	}
	switch {
	case errors.Is(err, api.ErrUndoConflict), errors.Is(err, api.ErrNothingToCompact):
		fmt.Printf("%s!  %v%s\n", Yellow, err, Reset)
		return
	case errors.Is(err, api.ErrCantRewindConversation):
		fmt.Printf("%s!  %s%s\n", Yellow, i18n.T("rewind.old_prompt"), Reset)
		return
	case err != nil:
		fmt.Printf("%s✗ %v%s\n", Red, err, Reset)
		return
	}
	if len(res.Restored) > 0 {
		fmt.Printf("%s↩️  %s%s\n", Green, i18n.T("rewind.restored", "files", safe(strings.Join(res.Restored, ", "))), Reset)
	}
	switch res.Mode {
	case api.RewindBoth, api.RewindConversation:
		fmt.Printf("%s↩️  %s%s\n", Green, i18n.T("rewind.conversation"), Reset)
		if in, ok := app.Input.(interface{ SetNextInput(string) }); ok {
			in.SetNextInput(res.Prompt) // edit it, or send it again
		} else {
			fmt.Printf("%s%s%s\n", Dim, i18n.T("rewind.prompt_was", "prompt", safe(res.Prompt)), Reset)
		}
	case api.RewindCode:
		if len(res.Restored) == 0 {
			fmt.Println(i18n.T("rewind.no_files"))
		}
	default:
		fmt.Printf("%s✓ %s%s\n", Green, i18n.T("compact.done", "events", res.Compacted.EventsCompacted, "chars", res.Compacted.SummaryChars), Reset)
		if line := UsageLine(res.Compacted.Before, res.Compacted.After); line != "" {
			fmt.Printf("%s%s%s\n", Dim, line, Reset)
		}
	}
}

// rewindDetail describes a prompt: when, and the files changed from it.
func rewindDetail(p api.RewindPoint) string {
	s := p.Time.Local().Format("15:04")
	if len(p.Files) > 0 {
		s += " · " + strings.Join(p.Files, ", ")
	}
	return s
}
