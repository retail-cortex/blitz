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
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /help lists the built-in commands in groups, each sorted, then the custom
// ones by where they come from, each sorted.
func TestHelpIsGroupedAndSorted(t *testing.T) {
	defer i18n.SetCurrent(nil)
	i18n.SetCurrent(nil)
	app := newTestApp(t, nil)
	dir := filepath.Join(local(app).Dir(), ".blitz", "commands")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for _, f := range []string{"zeta.md", "alpha.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("Do it."), 0o644))
	}
	out := stripANSI(captureStdout(t, func() { HandleCommand(context.Background(), "/help", app) }))

	// The groups, in order.
	at := func(s string) int { return strings.Index(out, "\n"+s+"\n") }
	var last int
	for _, g := range helpGroups {
		title := i18n.T(g.title)
		require.NotEqual(t, g.title, title, "%s has no words", g.title)
		i := at(title)
		require.Greater(t, i, last, "%q out of order:\n%s", title, out)
		last = i
	}
	// Each group sorted by command.
	for _, g := range helpGroups {
		var got []string
		for _, r := range sortedRows(g.rows) {
			got = append(got, strings.TrimLeft(r[0], "/!"))
		}
		assert.True(t, slices.IsSorted(got), "%s: %v", g.title, got)
	}
	// Custom commands after the built-ins: the bundled ones, then this
	// project's, sorted.
	custom := out[strings.Index(out, "Custom commands"):]
	bundled, project := strings.Index(custom, "\nBuilt into Blitz\n"), strings.Index(custom, "\nThis project's\n")
	require.True(t, bundled >= 0 && project > bundled, "sources out of order:\n%s", custom)
	assert.Less(t, strings.Index(custom, "/alpha"), strings.Index(custom, "/zeta"), "project commands unsorted:\n%s", custom)
}

func TestCustomGroups(t *testing.T) {
	cmds := []api.CommandInfo{
		{Name: "z", Source: "skill"}, {Name: "b", Source: "project"}, {Name: "a", Source: "project"},
		{Name: "gh", Source: "mcp:github"}, {Name: "x", Source: "plugin tools"}, {Name: "r", Source: "bundled"}, {Name: "w", Source: "weird"},
	}
	var got []string
	for _, g := range customGroups(cmds) {
		var names []string
		for _, c := range g.cmds {
			names = append(names, c.Name)
		}
		got = append(got, g.source+":"+strings.Join(names, ","))
	}
	assert.Equal(t, []string{"bundled:r", "project:a,b", "plugin:x", "skill:z", "mcp:gh", "weird:w"}, got)
	assert.Equal(t, "weird", sourceTitle("weird"), "a source without words is shown as it is")
	k, d := sourceKind("plugin tools")
	assert.Equal(t, [2]string{"plugin", "tools"}, [2]string{k, d})
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansi.ReplaceAllString(s, "") }
