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

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func helpOf(t *testing.T, args ...string) string {
	t.Helper()
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs(append(args, "--help"))
	require.NoError(t, root.Execute())
	return out.String()
}

// `blitz --help` shows the commands and the flags in groups, in order,
// each sorted by name; no command is left ungrouped.
func TestHelpGroups(t *testing.T) {
	root := newRootCommand()
	for _, c := range root.Commands() {
		if c.IsAvailableCommand() || c.Name() == "help" {
			assert.NotEmpty(t, c.GroupID, "%s is in no group", c.Name())
		}
	}
	out := helpOf(t)
	assert.NotContains(t, out, "Additional Commands:")
	assert.NotContains(t, out, "\nFlags:\n", "the root's flags are in groups")
	last := 0
	for _, title := range []string{"Runs and sessions:", "The workspace:", "Models and extensions:", "Setup and maintenance:",
		"Session flags:", "Agent and model flags:", "One-shot run flags:", "Permissions and sandbox flags:", "Instructions and plugins flags:", "Other flags:"} {
		i := strings.Index(out, "\n"+title+"\n")
		require.Greater(t, i, last, "%q missing or out of order:\n%s", title, out)
		last = i
	}
	// Sorted within a group: --agency, --agent, --effort, --model.
	model := out[strings.Index(out, "Agent and model flags:"):]
	model = model[:strings.Index(model, "\n\n")]
	assert.Less(t, strings.Index(model, "--agency"), strings.Index(model, "--agent "))
	assert.Less(t, strings.Index(model, "--effort"), strings.Index(model, "--model"))
	assert.NotContains(t, out, "--trust-workspace", "a hidden flag stays hidden")
}

// Every flag a group names exists: a renamed flag would fall into Other.
func TestFlagGroupsNameRealFlags(t *testing.T) {
	flags := newRootCommand().LocalFlags()
	for _, g := range flagGroups {
		for _, f := range g.flags {
			assert.NotNil(t, flags.Lookup(f), "%s: no flag --%s", g.title, f)
		}
	}
}

// exec's run flags are grouped too; other commands keep cobra's list.
func TestSubcommandHelp(t *testing.T) {
	exec := helpOf(t, "exec")
	assert.Contains(t, exec, "\nAgent and model flags:\n")
	models := helpOf(t, "models")
	assert.Contains(t, models, "\nFlags:\n")
	assert.NotContains(t, models, "Other flags:")
	var none cobra.Command
	assert.Empty(t, flagsHelp(&none), "no flags, no section")
}
