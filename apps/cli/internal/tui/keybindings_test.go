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
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ergochat/readline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseKey(t *testing.T) {
	tests := []struct {
		in   string
		want rune
		err  bool
	}{
		{"ctrl+g", 7, false},
		{"Ctrl+T", 20, false},
		{"shift+tab", readline.MetaShiftTab, false},
		{"none", 0, false},
		{"ctrl+c", 0, true},
		{"ctrl+m", 0, true},
		{"alt+x", 0, true},
		{"ctrl+1", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseKey(tt.in)
			if tt.err {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadKeyBindings(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "keybindings.toml")
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		return p
	}
	k, err := LoadKeyBindings(filepath.Join(dir, "missing.toml"))
	require.NoError(t, err)
	assert.Equal(t, DefaultKeyBindings(), k, "no file: the defaults")

	k, err = LoadKeyBindings(write("editor = \"ctrl+e\"\ncycle_mode = \"none\"\n[commands]\n\"ctrl+t\" = \"/tasks\"\n"))
	require.NoError(t, err)
	assert.Equal(t, KeyBindings{Editor: 5, CycleMode: 0, Commands: map[rune]string{20: "/tasks"}}, k)

	tests := []struct{ name, body, err string }{
		{"unknown key", "colour = \"red\"\n", "unknown key"},
		{"bad key", "editor = \"alt+x\"\n", "isn't a key"},
		{"not a command", "[commands]\n\"ctrl+t\" = \"tasks\"\n", "isn't a /command"},
		{"already bound", "[commands]\n\"ctrl+g\" = \"/tasks\"\n", "already bound"},
		{"same key", "editor = \"shift+tab\"\n", "the same key"},
		{"none for a command", "[commands]\n\"none\" = \"/tasks\"\n", "not a key"},
		{"not TOML", "editor = \n", "keybindings.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadKeyBindings(write(tt.body))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.err)
		})
	}
}

func TestReboundKeys(t *testing.T) {
	keys := KeyBindings{Editor: 5, CycleMode: 0, Commands: map[rune]string{20: "/tasks"}}
	f := newFakeTerminalWith(t, TerminalOptions{Keys: &keys})
	ctx := context.Background()
	calls := 0
	f.in.SetPromptKeys(PromptKeys{CycleMode: func() string { calls++; return "" }})

	// A command's key at an empty prompt sends it.
	f.keys(t, "\x14")
	line, err := within(t, func() (string, error) { return f.in.ReadInput(ctx, "> ") })
	require.NoError(t, err)
	assert.Equal(t, "/tasks", line)

	// Not once something is typed; unbound Shift+Tab does nothing; the
	// editor has moved to Ctrl+E.
	var edited string
	f.in.edit = func(text string) (string, error) { edited = text; return text + "!", nil }
	f.keys(t, "hi", "\x14", "\x1b[Z", "\x05")
	line, err = within(t, func() (string, error) { return f.in.ReadInput(ctx, "> ") })
	require.NoError(t, err)
	assert.Equal(t, "hi!", line)
	assert.Equal(t, "hi", edited)
	assert.Zero(t, calls, "Shift+Tab is unbound")
}

func TestVimMode(t *testing.T) {
	f := newFakeTerminalWith(t, TerminalOptions{Vim: true})
	ctx := context.Background()
	// Insert "abc", Esc to normal mode, 0 to the start, x deletes "a",
	// then append at the end.
	f.keys(t, "abc", "\x1b", "0", "x", "A", "d\r")
	line, err := within(t, func() (string, error) { return f.in.ReadInput(ctx, "> ") })
	require.NoError(t, err)
	assert.Equal(t, "bcd", line)
}

func TestCmdTheme(t *testing.T) {
	app := newTestApp(t, nil)
	app.ConfigDir = t.TempDir()
	defer plainDiffs.Store(false)
	tests := []struct {
		name, line string
		want       []string
		theme      string
		plain      bool
	}{
		{"list", "/theme", []string{"Themes", "› auto", "dracula"}, "", false},
		{"unknown", "/theme neon", []string{"No theme neon"}, "", false},
		{"switch", "/theme light", []string{"Theme: light"}, "light", false},
		{"plain diffs, saved", "/theme notty --save", []string{"Theme: notty", "Saved ui.theme"}, "notty", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				handled, err := HandleCommand(context.Background(), tt.line, app)
				require.NoError(t, err)
				require.True(t, handled)
			})
			for _, w := range tt.want {
				assert.Contains(t, out, w)
			}
			assert.Equal(t, tt.theme, app.Printer.Theme)
			assert.Equal(t, tt.plain, plainDiffs.Load())
		})
	}
	d, _ := RenderDiff("+added\n-removed", 0)
	assert.Equal(t, "   +added\n   -removed\n", d, "no colours in a plain theme")
	b, err := os.ReadFile(filepath.Join(app.ConfigDir, ".env.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(b), `theme = "notty"`)
}
