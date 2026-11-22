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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/i18n"
)

// The banner's forms (ui.banner, spec_067_cli_parity CLP-01, CLP-02).
const (
	BannerFull    = "full"
	BannerCompact = "compact"
	BannerOff     = "off"
)

// wordmark is Blitz in block letters, drawn in the lightning gradient.
var wordmark = []string{
	"██████  ██      ██ ████████ ███████",
	"██   ██ ██      ██    ██       ███ ",
	"██████  ██      ██    ██      ███  ",
	"██   ██ ██      ██    ██     ███   ",
	"██████  ███████ ██    ██    ███████",
}

// wordmarkMin is the narrowest terminal the wordmark is drawn in; a
// narrower one gets the one-line mark.
const wordmarkMin = 60

var (
	colorOn   = true
	truecolor bool
	// The codes SetColor puts back when colour comes on again.
	colorCodes = []*string{&Reset, &Bold, &Dim, &Red, &Green, &Yellow, &Blue, &Magenta, &Cyan, &White, &BgBlack, &BgBlue}
	colorSaved []string
)

func init() {
	for _, c := range colorCodes {
		colorSaved = append(colorSaved, *c)
	}
}

// ColorWanted decides whether output is coloured (CLP-03): --color
// (always, never; auto or "" to decide), then NO_COLOR (set, not empty: off),
// then CLICOLOR_FORCE or FORCE_COLOR (on), then TERM=dumb or output
// that isn't a terminal (off).
func ColorWanted(flag string, tty bool, getenv func(string) string) bool {
	switch strings.ToLower(flag) {
	case "always", "yes", "on":
		return true
	case "never", "no", "off":
		return false
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	if getenv("NO_COLOR") != "" { // no-color.org: present and not empty
		return false
	}
	if v := getenv("CLICOLOR_FORCE"); v != "" && v != "0" {
		return true
	}
	if v := getenv("FORCE_COLOR"); v != "" && v != "0" {
		return true
	}
	if getenv("TERM") == "dumb" {
		return false
	}
	return tty
}

// SetColor turns every colour the CLI prints on or off; with it on,
// truecolor (COLORTERM=truecolor or 24bit) draws the banner's gradient in
// full colour, else in 256 colours.
func SetColor(on bool, getenv func(string) string) {
	if getenv == nil {
		getenv = os.Getenv
	}
	colorOn = on
	for i, c := range colorCodes {
		if on {
			*c = colorSaved[i]
		} else {
			*c = ""
		}
	}
	ct := strings.ToLower(getenv("COLORTERM"))
	truecolor = on && (ct == "truecolor" || ct == "24bit")
}

// ColorOn reports whether output is coloured.
func ColorOn() bool { return colorOn }

// BannerInfo is what the banner shows.
type BannerInfo struct {
	// Form is BannerFull, BannerCompact or BannerOff.
	Form    string
	Width   int
	Version string
	// Workspace is its folder, Branch its git branch ("" for none).
	Workspace, Branch string
	Agent, Model      string
	// NoModel says no model is set up yet.
	NoModel bool
	Mode    string
	// Sandbox is the shell sandbox in a word or two.
	Sandbox string
}

// RenderBanner is the interactive CLI's first lines (CLP-01): the Blitz
// wordmark in the lightning gradient (or one line when the terminal is
// narrow, colour is off, the organization renames the app, or the form is
// compact), "powered by Retail Cortex" and the version, then the
// workspace, the agent and model, the mode and the sandbox, and one line
// of how to get going.
func RenderBanner(b BannerInfo) string {
	if b.Form == BannerOff {
		return ""
	}
	var sb strings.Builder
	version := b.Version
	if version != "" && version != "dev" && !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	powered := i18n.T("banner.powered")
	if version != "" {
		version = " · " + version
	}
	full := b.Form != BannerCompact && colorOn && (b.Width == 0 || b.Width >= wordmarkMin) && i18n.AppName() == i18n.Product
	sb.WriteByte('\n')
	if full {
		for _, line := range wordmark {
			sb.WriteString("  ")
			sb.WriteString(gradient(line))
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "  %s⚡ %s%s%s%s%s\n\n", Yellow, Reset, powered, Dim, version, Reset)
	} else {
		fmt.Fprintf(&sb, "  %s⚡ %s%s%s%s%s · %s%s\n\n", Yellow, Bold, i18n.AppName(), Reset, Dim, version, powered, Reset)
	}

	model := b.Model
	if b.NoModel {
		model = Yellow + i18n.T("banner.no_model") + Reset
	}
	where := homeShort(b.Workspace)
	if b.Branch != "" {
		where += Dim + "  (" + b.Branch + ")" + Reset
	}
	mode := b.Mode
	if c := modeColor(mode); c != "" {
		mode = c + mode + Reset
	}
	if b.Sandbox != "" {
		mode += Dim + " · " + i18n.T("banner.sandbox") + " " + b.Sandbox + Reset
	}
	rows := [][2]string{
		{i18n.T("banner.workspace"), where},
		{i18n.T("banner.agent"), b.Agent + Dim + " · " + Reset + model},
		{i18n.T("banner.mode"), mode},
	}
	pad := 0
	for _, r := range rows {
		pad = max(pad, utf8.RuneCountInString(r[0]))
	}
	for _, r := range rows {
		fmt.Fprintf(&sb, "  %s%-*s%s  %s\n", Dim, pad, r[0], Reset, r[1])
	}
	fmt.Fprintf(&sb, "\n  %s%s%s\n\n", Dim, i18n.T("banner.hint", "help", "/help", "key", "Ctrl+C"), Reset)
	return sb.String()
}

// gradient colours a line of the wordmark from lightning yellow to
// orange, left to right.
func gradient(line string) string {
	runes := []rune(line)
	if !colorOn {
		return line
	}
	var sb strings.Builder
	n := len(runes)
	for i, r := range runes {
		if r == ' ' {
			sb.WriteRune(r)
			continue
		}
		t := float64(i) / float64(max(n-1, 1))
		if truecolor {
			// #FFD400 to #FF5A00.
			g := int(0xD4 - t*float64(0xD4-0x5A))
			fmt.Fprintf(&sb, "\033[38;2;255;%d;0m%c", g, r)
		} else {
			steps := []int{226, 220, 214, 208, 202}
			fmt.Fprintf(&sb, "\033[38;5;%dm%c", steps[min(int(t*float64(len(steps))), len(steps)-1)], r)
		}
	}
	sb.WriteString(Reset)
	return sb.String()
}

// homeShort writes a path under the home folder as ~/….
func homeShort(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || dir == "" {
		return dir
	}
	if dir == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
		return "~" + string(filepath.Separator) + rel
	}
	return dir
}

// gitBranch is the branch checked out in dir or a folder above it ("" when
// it isn't in a repository, or HEAD is detached: then the commit's short
// hash). It reads .git/HEAD rather than running git.
func gitBranch(dir string) string {
	for d := dir; d != ""; {
		gitPath := filepath.Join(d, ".git")
		if fi, err := os.Stat(gitPath); err == nil {
			if !fi.IsDir() { // a worktree: "gitdir: <path>"
				b, err := os.ReadFile(gitPath)
				if err != nil {
					return ""
				}
				gitPath = strings.TrimSpace(strings.TrimPrefix(string(b), "gitdir:"))
				if !filepath.IsAbs(gitPath) {
					gitPath = filepath.Join(d, gitPath)
				}
			}
			head, err := os.ReadFile(filepath.Join(gitPath, "HEAD"))
			if err != nil {
				return ""
			}
			h := strings.TrimSpace(string(head))
			if ref, ok := strings.CutPrefix(h, "ref: refs/heads/"); ok {
				return ref
			}
			if len(h) >= 8 {
				return h[:8]
			}
			return ""
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return ""
}

// shellSandbox is the shell sandbox of a workspace's sandbox summary, in a
// word, without the details /sandbox shows ("" when there's none).
func shellSandbox(summary []string) string {
	for _, l := range summary {
		if s, ok := strings.CutPrefix(l, "shell OS sandbox: "); ok {
			word, _, _ := strings.Cut(s, " (") // "on (writes limited to …)": on
			return word
		}
	}
	return ""
}

// printBanner prints the session's first lines (CLP-01).
func printBanner(app *App) {
	st := app.Workspace.Settings()
	agent := st.Agent
	if agent == "" {
		agent = app.Workspace.ActiveAgent().Name
	}
	model := st.Model.Name
	if model == "" {
		model = app.Workspace.Model().Name
	}
	fmt.Print(RenderBanner(BannerInfo{
		Form: app.Banner, Width: app.Width, Version: app.Version,
		Workspace: app.Workspace.Dir(), Branch: gitBranch(app.Workspace.Dir()),
		Agent: agent, Model: model, NoModel: app.Workspace.ModelErr() != nil,
		Mode: st.PermissionMode, Sandbox: shellSandbox(app.Workspace.SandboxSummary()),
	}))
}

// hintsFile marks that the start-up hints were shown once (CLP-04).
const hintsFile = "cli-hints-shown"

// printFirstHints shows how to steer, the keys and multi-line input the
// first time the interactive CLI starts on a terminal; /help keys shows
// them again.
func printFirstHints(app *App) {
	if _, ok := app.Input.(steerInput); !ok || app.StateDir == "" || app.Banner == BannerOff {
		return
	}
	mark := filepath.Join(app.StateDir, hintsFile)
	if _, err := os.Stat(mark); err == nil {
		return
	}
	printKeyHints()
	_ = os.WriteFile(mark, []byte("1\n"), 0o600)
}

// printKeyHints prints the REPL's hints: input, steering and keys.
func printKeyHints() {
	for _, k := range []string{"repl.hint", "steer.hint", "keys.hint"} {
		fmt.Printf("  %s%s%s\n", Dim, i18n.T(k), Reset)
	}
	fmt.Println()
}
