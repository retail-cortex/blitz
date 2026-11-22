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
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/i18n"
)

// dispatched are the slash commands the dispatchers handle: the cases of
// HandleCommand's and handleExtraCommand's switches, and the ones the REPL
// runs itself (strings.CutPrefix(line, "/btw"), line == "/init").
func dispatched(t *testing.T) []string {
	t.Helper()
	var out []string
	fset := token.NewFileSet()
	for _, file := range []string{"commands.go", "commands_extra.go"} {
		f, err := parser.ParseFile(fset, file, nil, 0)
		require.NoError(t, err)
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "HandleCommand" && fn.Name.Name != "handleExtraCommand" {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sw, ok := n.(*ast.SwitchStmt)
				if !ok {
					return true
				}
				if id, ok := sw.Tag.(*ast.Ident); !ok || id.Name != "cmd" {
					return true
				}
				for _, s := range sw.Body.List {
					for _, e := range s.(*ast.CaseClause).List {
						if lit, ok := e.(*ast.BasicLit); ok {
							v, _ := strconv.Unquote(lit.Value)
							out = append(out, v)
						}
					}
				}
				return false
			})
		}
	}
	repl, err := os.ReadFile("repl.go")
	require.NoError(t, err)
	for _, m := range regexp.MustCompile(`(?:CutPrefix\(line, |line == )"/([a-z_]+)"`).FindAllStringSubmatch(string(repl), -1) {
		out = append(out, m[1])
	}
	return out
}

// Every command a dispatcher handles is declared in the tree, so /help,
// completion and the desktop's parity test see it (CLP-10).
func TestEveryDispatchedCommandIsDeclared(t *testing.T) {
	got := dispatched(t)
	require.Greater(t, len(got), 50, "the dispatchers' cases weren't found")
	for _, name := range got {
		assert.NotNil(t, FindCommand(name), "/%s is handled but not in Commands (commandtree.go)", name)
	}
	assert.Contains(t, got, "btw")
	assert.Contains(t, got, "init")
}

// The tree declares each name once, and each command's help has words.
func TestCommandTreeIsWellFormed(t *testing.T) {
	seen := map[string]string{}
	for _, c := range Commands {
		require.NotEmpty(t, c.Group, "/%s has no /help group", c.Name)
		require.NotEmpty(t, c.Help, "/%s has no help", c.Name)
		assert.NotEqual(t, c.Help, i18n.T(c.Help), "/%s: %s has no words", c.Name, c.Help)
		for _, n := range c.names() {
			if other, dup := seen[n]; dup {
				t.Errorf("/%s is both /%s and /%s", n, other, c.Name)
			}
			seen[n] = c.Name
		}
		subs := map[string]bool{}
		for _, s := range c.Subs {
			assert.False(t, subs[s.Name], "/%s %s twice", c.Name, s.Name)
			subs[s.Name] = true
		}
	}
}

func TestFindCommand(t *testing.T) {
	cases := map[string]string{"permissions": "permissions", "perms": "permissions", "PS": "processes", "set": "config", "show": "config", "nope": ""}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			c := FindCommand(in)
			if want == "" {
				assert.Nil(t, c)
				return
			}
			require.NotNil(t, c)
			assert.Equal(t, want, c.Name)
		})
	}
}

func TestNearest(t *testing.T) {
	cases := map[string]string{
		"permisions": "permissions", "modle": "model", "sesion": "session", "hlep": "help", "qiut": "exit",
		"xyzzy": "", "a": "",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) { assert.Equal(t, want, nearest(Commands, in)) })
	}
	assert.Equal(t, 0, editDistance("abc", "abc"))
	assert.Equal(t, 3, editDistance("", "abc"))
	assert.Equal(t, 1, editDistance("kitten", "sitten"))
	assert.Equal(t, 1, editDistance("modle", "model"), "a swap is one")
}

// An unknown command says what's nearest; an unknown sub-command of /skills
// or /session says so rather than nothing (CLP-24).
func TestUnknownCommandsSayWhatIsNear(t *testing.T) {
	app := newTestApp(t, nil)
	out := stripANSI(captureStdout(t, func() { HandleCommand(context.Background(), "/permisions", app) }))
	assert.Contains(t, out, "Did you mean /permissions?")
	out = stripANSI(captureStdout(t, func() { HandleCommand(context.Background(), "/skills lsit", app) }))
	assert.Contains(t, out, "/skills has no “lsit”")
	assert.Contains(t, out, "Did you mean /skills list?")
	out = stripANSI(captureStdout(t, func() { HandleCommand(context.Background(), "/session neww", app) }))
	assert.Contains(t, out, "Did you mean /session new?")
}

// /help <command> shows its usage, sub-commands, values, flags and other
// names; /help keys the keys.
func TestHelpForOneCommand(t *testing.T) {
	app := newTestApp(t, nil)
	help := func(line string) string {
		return stripANSI(captureStdout(t, func() { HandleCommand(context.Background(), line, app) }))
	}
	out := help("/help permissions")
	for _, want := range []string{"/permissions [allow|ask|deny|remove <rule> [--save]]", "sub-commands", "allow, ask, deny, remove", "also", "/perms", "Tab completes"} {
		assert.Contains(t, out, want)
	}
	out = help("/help /mode")
	assert.Contains(t, out, "auto, default, accept-edits, plan, dont-ask, bypass, unsafe")
	assert.Contains(t, help("/help keys"), "Shift+Tab")
	assert.Contains(t, help("/help modle"), "Did you mean /model?")
	assert.True(t, strings.Contains(help("/help"), "/help [command|keys]"))
}
