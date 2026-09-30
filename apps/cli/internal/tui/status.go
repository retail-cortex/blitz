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
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// The REPL's views of its state (spec_parity_027 §8.3): /status, /config,
// /copy, /context's breakdown, a browsable /diff, and notifications when
// a long turn ends or waits for you.

// cmdStatus shows where the session stands (PAR-UI-05).
func cmdStatus(app *App) {
	row := func(k, v string) { fmt.Printf("  %-16s %s\n", k, safe(v)) }
	fmt.Printf("\n%s%s%s\n", Bold, i18n.T("status.title"), Reset)
	row(i18n.T("status.version"), app.Version)
	where := i18n.T("status.local")
	if app.Attached {
		where = i18n.T("status.attached")
	}
	row(i18n.T("status.workspace"), app.Workspace.Dir()+" ("+where+")")
	if s, ok := app.Workspace.ActiveSession(); ok {
		row(i18n.T("status.session"), s.ID+" · "+sessionTitle(s))
	}
	st := app.Workspace.Settings()
	row(i18n.T("status.agent"), st.Agent)
	model := st.Model.Name
	if st.Model.Provider != "" {
		model = st.Model.Provider + "/" + model
	}
	if err := app.Workspace.ModelErr(); err != nil {
		model += " — " + err.Error()
	}
	row(i18n.T("status.model"), model)
	row(i18n.T("status.mode"), st.PermissionMode)
	row(i18n.T("status.sandbox"), strings.Join(app.Workspace.SandboxSummary(), "; "))
	var mcp []string
	for _, m := range app.Workspace.ListMCPServers() {
		mcp = append(mcp, m.Name)
	}
	if len(mcp) > 0 {
		row(i18n.T("status.mcp"), strings.Join(mcp, ", "))
	}
	files := []string{filepath.Join(config.ConfigDir(app.ConfigDir), ".env.toml")}
	if ps := app.Workspace.ProjectSettings(); len(ps.Files) > 0 {
		files = append(files, ps.Files...)
	}
	row(i18n.T("status.settings"), strings.Join(files, ", "))
	fmt.Println()
}

// configKeys are the settings /config changes, with what --save writes.
var configKeys = map[string]string{
	"model": "blitz.default_model", "agent": "blitz.default_agent", "mode": "blitz.permission_mode",
	"agency": "blitz.agency_level", "style": "ui.style", "effort": "", "locale": "ui.locale",
}

// cmdConfig shows the session's settings, or sets one, keeping it with
// --save (PAR-UI-07; /set is the same).
func cmdConfig(ctx context.Context, args []string, app *App) {
	save := false
	var rest []string
	for _, a := range args {
		if a == "--save" {
			save = true
		} else {
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 {
		showSettings(app)
		fmt.Printf("  %s%s%s\n\n", Dim, i18n.T("config.keys", "keys", strings.Join(sortedConfigKeys(), ", ")), Reset)
		return
	}
	key, value, ok := strings.Cut(strings.Join(rest, " "), "=")
	key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
	if !ok || key == "" {
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("set.usage"), Reset)
		return
	}
	var err error
	switch key {
	case "model":
		_, err = app.Workspace.SetModel(ctx, value)
	case "agent":
		_, err = app.Workspace.SetAgent(ctx, value)
	case "mode", "permission_mode":
		key = "mode"
		_, err = app.Workspace.SetPermissionMode(value)
	case "locale":
		_, err = app.Workspace.SetLocale(ctx, value)
	default:
		_, err = app.Workspace.Set(ctx, key, value)
	}
	var unknown *api.UnknownSettingError
	switch {
	case errors.As(err, &unknown):
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("set.unknown", "key", safe(unknown.Key)), Reset)
		return
	case errors.Is(err, api.ErrInvalidAgency):
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("set.agency_invalid"), Reset)
		return
	case err != nil:
		fmt.Printf("%s✗ %s%s\n", Red, i18n.T("set.failed", "error", err), Reset)
		return
	}
	fmt.Printf("%s✓ %s%s\n", Green, i18n.T("set.updated", "key", key, "value", safe(value)), Reset)
	if !save {
		return
	}
	file := configKeys[key]
	if file == "" {
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("config.not_saved", "key", key), Reset)
		return
	}
	path, err := config.SetValue(app.ConfigDir, "", file, value)
	if err != nil {
		fmt.Printf("%s✗ %v%s\n", Red, err, Reset)
		return
	}
	fmt.Printf("%s%s%s\n", Dim, i18n.T("config.saved", "key", file, "path", safe(path)), Reset)
}

func sortedConfigKeys() []string {
	keys := make([]string, 0, len(configKeys))
	for k := range configKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// showSettings prints the session's settings.
func showSettings(app *App) {
	st := app.Workspace.Settings()
	fmt.Printf("\n%s%s:%s\n", Bold, i18n.T("settings.title"), Reset)
	fmt.Printf("  %-14s %s\n", i18n.T("settings.agency")+":", st.Agency)
	fmt.Printf("  %-14s %s\n", i18n.T("settings.model")+":", i18n.T("settings.model_value", "model", st.Model.Name, "provider", st.Model.Provider))
	fmt.Printf("  %-14s %s\n", i18n.T("settings.agent")+":", st.Agent)
	fmt.Printf("  %-14s %s\n", i18n.T("settings.locale")+":", st.Locale)
	fmt.Printf("  %-14s %s\n", i18n.T("settings.mode")+":", st.PermissionMode)
	effort := st.Effort
	if effort == "" {
		effort = i18n.T("effort.auto")
	}
	fmt.Printf("  %-14s %s\n", i18n.T("settings.effort")+":", effort)
	fmt.Printf("  %-14s %s\n", i18n.T("settings.style")+":", st.Style)
}

// cmdCopy copies an answer to the clipboard (PAR-UI-04, BL-CLI-01): the
// last, or the nth from last.
func cmdCopy(args []string, app *App) {
	n := 1
	if len(args) == 1 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v < 1 {
			fmt.Println(i18n.T("copy.usage"))
			return
		}
		n = v
	}
	s, ok := app.Workspace.ActiveSession()
	if !ok {
		fmt.Println(i18n.T("session.none_active"))
		return
	}
	var answers []string
	for _, m := range s.Messages {
		if m.Role == "model" && m.Kind == "" && strings.TrimSpace(m.Text) != "" {
			answers = append(answers, m.Text)
		}
	}
	if n > len(answers) {
		fmt.Println(i18n.T("copy.none"))
		return
	}
	how, err := copyToClipboard(answers[len(answers)-n])
	if err != nil {
		fmt.Printf("%s✗ %v%s\n", Red, err, Reset)
		return
	}
	fmt.Printf("%s✓ %s%s\n", Green, i18n.T("copy.done", "how", how), Reset)
}

// clipboardCommands are the programs that set the clipboard, by system.
var clipboardCommands = func() [][]string {
	switch goruntime.GOOS {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		return [][]string{{"clip.exe"}}
	}
	var out [][]string
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		out = append(out, []string{"wl-copy"})
	}
	return append(out, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"}, []string{"clip.exe"})
}

// osc52Out is where OSC 52 goes (the terminal); a variable for tests.
var osc52Out = func() *os.File { return os.Stdout }

// copyToClipboard puts text on the clipboard: through the system's program,
// or, over SSH or without one, the terminal's OSC 52 (the terminal's own
// clipboard, wherever it runs). It says how.
func copyToClipboard(text string) (string, error) {
	if os.Getenv("SSH_CONNECTION") == "" && os.Getenv("SSH_TTY") == "" {
		for _, argv := range clipboardCommands() {
			if _, err := exec.LookPath(argv[0]); err != nil {
				continue
			}
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.Stdin = strings.NewReader(text)
			if err := cmd.Run(); err == nil {
				return argv[0], nil
			}
		}
	}
	_, err := fmt.Fprintf(osc52Out(), "\033]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text)))
	return "OSC 52", err
}

// printContextParts shows what the context is made of (PAR-UI-06).
func printContextParts(c api.ContextInfo) {
	if len(c.Parts) == 0 {
		return
	}
	names := make([]string, len(c.Parts))
	width := 0
	for i, p := range c.Parts {
		names[i] = i18n.T("context.part." + p.Name)
		width = max(width, len([]rune(names[i])))
	}
	for i, p := range c.Parts {
		pct := 0.0
		if c.Tokens > 0 {
			pct = float64(p.Tokens) / float64(c.Tokens) * 100
		}
		bar := strings.Repeat("█", int(pct/5))
		fmt.Printf("  %s%s %8s %5.1f%% %s%s%s\n", names[i], strings.Repeat(" ", width-len([]rune(names[i]))), "≈"+humanTokens(p.Tokens), pct, Dim, bar, Reset)
	}
	fmt.Println()
}

// fileDiff is one file's part of a unified diff.
type fileDiff struct {
	path       string
	added, del int
	text       string
}

// splitDiff cuts a unified diff into its files.
func splitDiff(d string) []fileDiff {
	var out []fileDiff
	var cur *fileDiff
	for line := range strings.Lines(d) {
		if strings.HasPrefix(line, "--- ") && cur != nil && strings.Contains(cur.text, "@@") ||
			strings.HasPrefix(line, "diff --git ") || cur == nil && strings.HasPrefix(line, "--- ") {
			out = append(out, fileDiff{})
			cur = &out[len(out)-1]
		}
		if cur == nil {
			continue
		}
		cur.text += line
		switch {
		case strings.HasPrefix(line, "+++ "):
			p := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			if p != "/dev/null" {
				cur.path = strings.TrimPrefix(p, "b/")
			}
		case strings.HasPrefix(line, "--- "):
			if p := strings.TrimSpace(strings.TrimPrefix(line, "--- ")); cur.path == "" && p != "/dev/null" {
				cur.path = strings.TrimPrefix(p, "a/")
			}
		case strings.HasPrefix(line, "+"):
			cur.added++
		case strings.HasPrefix(line, "-"):
			cur.del++
		}
	}
	return out
}

// browseDiff lets you pick a file of the session's diff and read it paged
// (PAR-UI-10); without a terminal picker it prints the whole diff.
func browseDiff(ctx context.Context, d string, app *App) {
	files := splitDiff(d)
	p, ok := terminalPicker(app)
	if !ok || len(files) < 2 {
		out, _ := RenderDiff(d, 0)
		fmt.Print("\n" + out + "\n")
		return
	}
	items := make([]PickItem, len(files))
	for i, f := range files {
		items[i] = PickItem{Label: f.path, Detail: fmt.Sprintf("+%d −%d", f.added, f.del)}
	}
	current := 0
	for {
		i, err := p.Pick(ctx, i18n.T("diff.pick", "count", len(files)), items, current)
		if err != nil {
			return
		}
		current = i
		out, _ := RenderDiff(files[i].text, 0)
		page(out)
	}
}

// page shows text through $PAGER (less -R by default) when there is one,
// else prints it.
func page(text string) {
	pager := strings.TrimSpace(os.Getenv("PAGER"))
	if pager == "" {
		if _, err := exec.LookPath("less"); err == nil {
			pager = "less -R"
		}
	}
	if pager == "" {
		fmt.Print("\n" + text + "\n")
		return
	}
	cmd := exec.Command("/bin/sh", "-c", pager)
	cmd.Stdin = bytes.NewReader([]byte(text))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Print("\n" + text + "\n")
	}
}

// Notifications (PAR-UI-03): after a turn that ran longer than
// ui.notify_after, or when an approval or question waits during one, ring
// the bell and show a desktop notification, as ui.notify says.

// turnStart is when the running turn started (0 when none runs).
var turnStart atomic.Int64

// Notifier sends the REPL's notifications.
type Notifier struct {
	After time.Duration // 0: never
	Mode  string        // both, bell, desktop, off
}

// longRunning reports whether the turn has run past After.
func (n Notifier) longRunning() bool {
	started := turnStart.Load()
	return n.After > 0 && started > 0 && time.Since(time.Unix(0, started)) >= n.After
}

// notifyCommand is what shows a desktop notification; a variable for tests.
var notifyCommand = func(title, body string) *exec.Cmd {
	switch goruntime.GOOS {
	case "darwin":
		script := fmt.Sprintf("display notification %s with title %s", strconv.Quote(body), strconv.Quote(title))
		return exec.Command("osascript", "-e", script)
	case "linux":
		if _, err := exec.LookPath("notify-send"); err == nil {
			return exec.Command("notify-send", title, body)
		}
	}
	return nil
}

// Send rings the bell and shows a notification, as the mode says.
func (n Notifier) Send(body string) {
	if n.Mode == "off" {
		return
	}
	if n.Mode != "desktop" {
		fmt.Print("\a")
	}
	if n.Mode == "bell" {
		return
	}
	if cmd := notifyCommand("Blitz", body); cmd != nil {
		_ = cmd.Start()
		go cmd.Wait()
	}
}

// Approving wraps an approver to notify when a request waits during a long
// turn.
func (n Notifier) Approving(a api.Approver) api.Approver {
	return func(ctx context.Context, req api.ApprovalRequest) (api.Decision, error) {
		if n.longRunning() {
			n.Send(i18n.T("notify.approval", "tool", req.Tool))
		}
		return a(ctx, req)
	}
}

// Asking wraps a question prompter the same way.
func (n Notifier) Asking(ask api.UserPromptFunc) api.UserPromptFunc {
	return func(ctx context.Context, q string, options []string) (string, error) {
		if n.longRunning() {
			n.Send(i18n.T("notify.question"))
		}
		return ask(ctx, q, options)
	}
}
