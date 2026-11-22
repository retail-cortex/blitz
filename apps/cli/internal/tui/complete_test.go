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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// treeCompleter is a completer with every source answering, counting the
// models' asks.
func treeCompleter(t *testing.T) (*Completer, *int) {
	t.Helper()
	ws := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "notes.md"), nil, 0o644))
	c := NewCompleter(ws)
	asked := 0
	c.Source(SrcModels, func() []string { asked++; return []string{"gemini-flash", "anthropic/claude-sonnet"} })
	c.Source(SrcAgents, func() []string { return []string{"blitz", "coder"} })
	c.Source(SrcStyles, func() []string { return []string{"default", "terse"} })
	c.Source(SrcSessions, func() []string { return []string{"session-1", "session-2"} })
	custom := []string{"review"}
	c.Custom(func() []string { return custom })
	t.Cleanup(func() { custom = nil })
	return c, &asked
}

func words(c *Completer, line string) []string {
	cands, _ := c.Do([]rune(line), len([]rune(line)))
	var out []string
	for _, r := range cands {
		out = append(out, strings.TrimSpace(string(r)))
	}
	return out
}

// Completion at every level: sub-commands of sub-commands, their flags,
// values from sources, /config's key=value, aliases, and custom commands
// as they are now (CLP-11).
func TestCompletionAtEveryLevel(t *testing.T) {
	c, _ := treeCompleter(t)
	cases := map[string]struct {
		line string
		want []string
	}{
		"command names":                       {"/perm", []string{"issions"}},
		"names, not their aliases":            {"/per", []string{"missions", "sonality"}},
		"an alias alone matches":              {"/us", []string{"age"}},
		"sub-commands":                        {"/skills re", []string{"gistry"}},
		"a sub-command's flag":                {"/permissions allow ", []string{"--save"}},
		"a used flag isn't again":             {"/rewind --force ", nil},
		"sub-commands of an alias":            {"/perms d", []string{"eny"}},
		"choices":                             {"/mode ac", []string{"cept-edits"}},
		"unsafe is a choice":                  {"/mode uns", []string{"afe"}},
		"a source":                            {"/model gem", []string{"ini-flash"}},
		"a second argument":                   {"/pin_model coder anth", []string{"ropic/claude-sonnet"}},
		"a sub-command's source":              {"/session load session-", []string{"1", "2"}},
		"themes":                              {"/theme tok", []string{"yo-night"}},
		"config keys":                         {"/config per", []string{"sonality="}},
		"config values":                       {"/config mode=pl", []string{"an"}},
		"config values by source":             {"/set style=te", []string{"rse"}},
		"paths":                               {"/export no", []string{"tes.md"}},
		"folders only":                        {"/cd ", []string{"docs/"}},
		"custom commands":                     {"/rev", []string{"iew"}},
		"help names commands":                 {"/help perm", []string{"issions"}},
		"a custom command's args are its own": {"/review x", nil},
		"plain words":                         {"hello wo", nil},
	}
	for name, cs := range cases {
		t.Run(name, func(t *testing.T) {
			got := words(c, cs.line)
			if cs.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.ElementsMatch(t, cs.want, got)
		})
	}
}

// Sources are asked on Tab and kept a while; the hint as you type uses
// only what's kept.
func TestSourcesAreKept(t *testing.T) {
	c, asked := treeCompleter(t)
	now := time.Now()
	c.now = func() time.Time { return now }
	ghost, _ := c.Suggest("/model gem")
	assert.Empty(t, ghost, "nothing kept yet, and the hint doesn't ask")
	assert.Equal(t, 0, *asked)
	words(c, "/model gem")
	words(c, "/model ")
	assert.Equal(t, 1, *asked, "kept")
	ghost, _ = c.Suggest("/model gem")
	assert.Equal(t, "ini-flash", ghost, "the hint uses what's kept")
	now = now.Add(sourceTTL + time.Second)
	words(c, "/model ")
	assert.Equal(t, 2, *asked, "asked again after a while")
	c.Source(SrcModels, func() []string { return []string{"other"} })
	assert.Contains(t, words(c, "/model "), "other", "a new source forgets the kept answer")
}

func TestSuggest(t *testing.T) {
	c, _ := treeCompleter(t)
	cases := map[string]struct{ line, ghost, hint string }{
		"the rest of one command":  {"/permi", "ssions", ""},
		"what candidates agree on": {"/che", "ckpoints", ""},
		"nothing they agree on":    {"/s", "", ""},
		"the hint once typed":      {"/permissions", "", "  [allow|ask|deny|remove <rule> [--save]] — "},
		"the hint after a space":   {"/permissions ", "", " [allow|ask|deny|remove <rule> [--save]] — "},
		"a sub-command":            {"/skills reg", "istry", ""},
		"no hint without usage":    {"/clear ", "", " — "},
		"no hint for arguments":    {"/rename foo ", "", ""},
		"not a command":            {"hello", "", ""},
		"an unknown command":       {"/zzz ", "", ""},
	}
	for name, cs := range cases {
		t.Run(name, func(t *testing.T) {
			ghost, hint := c.Suggest(cs.line)
			assert.Equal(t, cs.ghost, ghost)
			if cs.hint == "" {
				assert.Empty(t, hint)
			} else {
				assert.True(t, strings.HasPrefix(hint, cs.hint), "%q", hint)
			}
		})
	}
}

// Tab with nothing starting with what's typed takes the one candidate it
// matches by letters in order (CLP-13).
func TestFuzzy(t *testing.T) {
	c, _ := treeCompleter(t)
	cases := map[string]struct {
		line, want string
		ok         bool
	}{
		"letters in order":  {"/pmod", "/pin_model ", true},
		"in a sub-command":  {"/skills rgstry", "/skills registry ", true},
		"a prefix is Tab's": {"/perm", "", false},
		"more than one":     {"/se", "", false},
		"nothing":           {"/qqq", "", false},
		"nothing typed":     {"/skills ", "", false},
	}
	for name, cs := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := c.Fuzzy(cs.line)
			assert.Equal(t, cs.ok, ok)
			assert.Equal(t, cs.want, got)
		})
	}
	assert.True(t, subsequence("pmd", "pin_model"))
	assert.False(t, subsequence("dmp", "pin_model"))
}

// In the line editor: → at the end takes the ghost text, Tab takes a
// fuzzy match, and the painter draws the hint after the cursor and puts
// the cursor back.
func TestTheEditorTakesSuggestions(t *testing.T) {
	withColor(t, true, nil)
	c, _ := treeCompleter(t)
	f := newFakeTerminalWith(t, TerminalOptions{Completer: c})
	ctx := context.Background()

	f.keys(t, "/permi", "\x1b[C", "\r")
	line, err := within(t, func() (string, error) { return f.in.ReadInput(ctx, "> ") })
	require.NoError(t, err)
	assert.Equal(t, "/permissions", line, "→ took the ghost text")

	f.keys(t, "/pmod", "\t", "\r")
	line, err = within(t, func() (string, error) { return f.in.ReadInput(ctx, "> ") })
	require.NoError(t, err)
	assert.Equal(t, "/pin_model", strings.TrimSpace(line), "Tab took the fuzzy match")

	f.in.mu.Lock()
	f.in.line = "/permi"
	f.in.mu.Unlock()
	painted := string(f.in.paint([]rune("/permi"), 6))
	assert.True(t, strings.HasPrefix(painted, "/permi"+Dim+"ssions"+Reset), "%q", painted)
	assert.True(t, strings.HasSuffix(painted, "\x1b[6D"), "the cursor goes back: %q", painted)
	assert.Equal(t, "/per", string(f.in.paint([]rune("/per"), 4)), "only the whole line is painted")
	f.in.mu.Lock()
	f.in.line, f.in.searching = "/permi", true
	f.in.mu.Unlock()
	assert.Equal(t, "/permi", string(f.in.paint([]rune("/permi"), 6)), "not while searching history")
	f.in.mu.Lock()
	f.in.searching = false
	f.in.mu.Unlock()
	withColor(t, false, nil)
	assert.Equal(t, "/permi", string(f.in.paint([]rune("/permi"), 6)), "nor without colour")
}

func TestVisibleWidth(t *testing.T) {
	assert.Equal(t, 8, visibleWidth("\x1b[1mblitz\x1b[0m › "))
	assert.Equal(t, 2, visibleWidth("first line\n› "))
}

// Tab completes a command a custom one shares a start with (/skills and
// the bundled /skills-update) as far as they agree.
func TestTabCompletesWithCustomCommands(t *testing.T) {
	c := NewCompleter(t.TempDir())
	c.Custom(func() []string { return []string{"skills-update"} })
	f := newFakeTerminalWith(t, TerminalOptions{Completer: c})
	f.keys(t, "/sk", "\t", "\r")
	line, err := within(t, func() (string, error) { return f.in.ReadInput(context.Background(), "> ") })
	require.NoError(t, err)
	assert.Equal(t, "/skills", line)
}
