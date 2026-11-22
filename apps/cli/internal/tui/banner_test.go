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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/i18n"
)

// envOf is a getenv over a map.
func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// withColor sets colour for a test and puts it back after.
func withColor(t *testing.T, on bool, env map[string]string) {
	t.Helper()
	SetColor(on, envOf(env))
	t.Cleanup(func() { SetColor(true, envOf(nil)) })
}

func TestColorWanted(t *testing.T) {
	cases := map[string]struct {
		flag string
		tty  bool
		env  map[string]string
		want bool
	}{
		"a terminal":                 {tty: true, want: true},
		"a pipe":                     {tty: false, want: false},
		"NO_COLOR":                   {tty: true, env: map[string]string{"NO_COLOR": "1"}, want: false},
		"NO_COLOR empty is unset":    {tty: true, env: map[string]string{"NO_COLOR": ""}, want: true},
		"dumb terminal":              {tty: true, env: map[string]string{"TERM": "dumb"}, want: false},
		"CLICOLOR_FORCE on a pipe":   {tty: false, env: map[string]string{"CLICOLOR_FORCE": "1"}, want: true},
		"FORCE_COLOR on a pipe":      {tty: false, env: map[string]string{"FORCE_COLOR": "true"}, want: true},
		"FORCE_COLOR=0":              {tty: false, env: map[string]string{"FORCE_COLOR": "0"}, want: false},
		"NO_COLOR beats FORCE":       {tty: true, env: map[string]string{"NO_COLOR": "1", "FORCE_COLOR": "1"}, want: false},
		"--color=always":             {flag: "always", tty: false, env: map[string]string{"NO_COLOR": "1"}, want: true},
		"--color=never":              {flag: "never", tty: true, want: false},
		"--color=auto on a terminal": {flag: "auto", tty: true, want: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.want, ColorWanted(c.flag, c.tty, envOf(c.env)))
		})
	}
}

func TestSetColorBlanksEveryCode(t *testing.T) {
	withColor(t, false, nil)
	for _, c := range colorCodes {
		assert.Empty(t, *c)
	}
	assert.False(t, ColorOn())
	SetColor(true, envOf(map[string]string{"COLORTERM": "truecolor"}))
	assert.Equal(t, "\033[1m", Bold, "the codes come back")
	assert.True(t, truecolor)
}

func TestRenderBanner(t *testing.T) {
	info := BannerInfo{Version: "1.3.0", Workspace: "/work/shop", Branch: "cli-parity", Agent: "blitz", Model: "gemini-flash", Mode: "default", Sandbox: "seatbelt", Width: 100}
	cases := map[string]struct {
		form    string
		width   int
		color   bool
		noModel bool
		mode    string
		app     string
		art     bool
		want    []string
		absent  []string
	}{
		"full": {form: BannerFull, color: true, art: true,
			want: []string{"powered by Retail Cortex", "v1.3.0", "/work/shop", "(cli-parity)", "blitz", "gemini-flash", "default", "seatbelt", "/help", "Tab completes", "Ctrl+C"}},
		"empty form is full":     {form: "", color: true, art: true, want: []string{"powered by Retail Cortex"}},
		"compact":                {form: BannerCompact, color: true, want: []string{"⚡", "Blitz", "v1.3.0", "powered by Retail Cortex", "gemini-flash"}},
		"narrow terminal":        {form: BannerFull, width: 40, color: true, want: []string{"⚡", "Blitz", "powered by Retail Cortex"}},
		"no colour":              {form: BannerFull, color: false, want: []string{"Blitz", "powered by Retail Cortex", "/work/shop"}, absent: []string{"\033["}},
		"no model":               {form: BannerFull, color: true, art: true, noModel: true, want: []string{"no model set up yet", "blitz config set-key"}, absent: []string{"gemini-flash"}},
		"unsafe is red":          {form: BannerFull, color: true, art: true, mode: "unsafe", want: []string{Red + Bold + "unsafe"}},
		"an organization's name": {form: BannerFull, color: true, app: "Acme Assist", want: []string{"Acme Assist", "powered by Retail Cortex"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			withColor(t, c.color, nil)
			if c.app != "" {
				i18n.SetAppName(c.app)
				t.Cleanup(func() { i18n.SetAppName("") })
			}
			b := info
			b.Form, b.NoModel = c.form, c.noModel
			if c.width != 0 {
				b.Width = c.width
			}
			if c.mode != "" {
				b.Mode = c.mode
			}
			out := RenderBanner(b)
			for _, w := range c.want {
				assert.Contains(t, out, w)
			}
			for _, a := range c.absent {
				assert.NotContains(t, out, a)
			}
			assert.Equal(t, c.art, strings.Contains(out, "█"), "the wordmark")
		})
	}
	t.Run("off", func(t *testing.T) {
		assert.Empty(t, RenderBanner(BannerInfo{Form: BannerOff, Version: "1"}))
	})
	t.Run("no version", func(t *testing.T) {
		withColor(t, false, nil)
		assert.NotContains(t, RenderBanner(BannerInfo{Form: BannerCompact}), "· ·")
	})
}

func TestGradient(t *testing.T) {
	withColor(t, true, nil)
	g := gradient("██ █")
	assert.Contains(t, g, "\033[38;5;226m█", "it starts yellow")
	assert.Contains(t, g, "\033[38;5;202m█", "and ends orange")
	assert.Contains(t, g, "m█ \033", "spaces stay plain")
	withColor(t, true, map[string]string{"COLORTERM": "24bit"})
	assert.Contains(t, gradient("██"), "\033[38;2;255;212;0m█")
	withColor(t, false, nil)
	assert.Equal(t, "██", gradient("██"))
}

func TestGitBranch(t *testing.T) {
	root := t.TempDir()
	write := func(p, s string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(s), 0o644))
	}
	repo := filepath.Join(root, "repo")
	write(filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	sub := filepath.Join(repo, "a", "b")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	detached := filepath.Join(root, "detached")
	write(filepath.Join(detached, ".git", "HEAD"), "0123456789abcdef0123456789abcdef01234567\n")
	tree := filepath.Join(root, "tree")
	write(filepath.Join(root, "gitdirs", "tree", "HEAD"), "ref: refs/heads/feature/x\n")
	write(filepath.Join(tree, ".git"), "gitdir: ../gitdirs/tree\n")
	none := filepath.Join(root, "none")
	require.NoError(t, os.MkdirAll(none, 0o755))

	cases := map[string]struct{ dir, want string }{
		"a repository":   {repo, "main"},
		"a folder in it": {sub, "main"},
		"detached":       {detached, "01234567"},
		"a worktree":     {tree, "feature/x"},
		"no workspace":   {"", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { assert.Equal(t, c.want, gitBranch(c.dir)) })
	}
	t.Run("not a repository", func(t *testing.T) {
		// A temporary folder may sit in a repository of the machine's; the
		// branch then is that one's, never an error.
		assert.NotPanics(t, func() { gitBranch(none) })
	})
}

func TestHomeShort(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, "~", homeShort(home))
	assert.Equal(t, "~"+string(filepath.Separator)+"x", homeShort(filepath.Join(home, "x")))
	assert.Equal(t, "/elsewhere", homeShort("/elsewhere"))
	assert.Empty(t, homeShort(""))
}

func TestShellSandbox(t *testing.T) {
	assert.Equal(t, "seatbelt", shellSandbox([]string{"workspace: /w", "shell OS sandbox: seatbelt"}))
	assert.Equal(t, "on", shellSandbox([]string{"shell OS sandbox: on (writes limited to 9 dirs, network allowed)"}))
	assert.Empty(t, shellSandbox([]string{"workspace: /w"}))
}

func TestBannerAndHintsInTheREPL(t *testing.T) {
	withColor(t, false, nil)
	app, s := stubApp(t, "")
	s.sandboxSummary = func() []string { return []string{"workspace: here", "shell OS sandbox: seatbelt"} }
	app.Input = &steerScript{scriptInput: &scriptInput{}}
	app.StateDir = t.TempDir()
	recapped := false
	app.AfterBanner = func() { recapped = true }
	first := captureStdout(t, func() { require.NoError(t, RunREPL(t.Context(), app)) })
	assert.Contains(t, first, "powered by Retail Cortex")
	assert.Contains(t, first, "seatbelt")
	assert.Contains(t, first, "just start typing", "the hints show the first time")
	assert.True(t, recapped, "what follows the banner is printed")
	assert.FileExists(t, filepath.Join(app.StateDir, hintsFile))

	app.Input = &steerScript{scriptInput: &scriptInput{}}
	second := captureStdout(t, func() { require.NoError(t, RunREPL(t.Context(), app)) })
	assert.NotContains(t, second, "just start typing", "and not again")

	app.Input = &steerScript{scriptInput: &scriptInput{}}
	app.Banner = BannerOff
	quiet := captureStdout(t, func() { require.NoError(t, RunREPL(t.Context(), app)) })
	assert.NotContains(t, quiet, "powered by Retail Cortex", "ui.banner = off")
}
