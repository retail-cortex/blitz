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

// `blitz --help` in groups: the commands by what they're for, and the
// run's flags by what they set, each group sorted by name.

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// commandGroups are the help's command groups, in order, and their
// commands (cobra sorts each).
var commandGroups = []struct {
	id, title string
	commands  []string
}{
	{"run", "Runs and sessions:", []string{"agents", "attach", "exec", "logs", "sessions", "stop"}},
	{"workspace", "The workspace:", []string{"eval", "init", "jobs", "memory", "search", "trust", "worktrees"}},
	{"models", "Models and extensions:", []string{"auth", "mcp", "models", "plugin", "skills"}},
	{"setup", "Setup and maintenance:", []string{"completion", "config", "doctor", "help", "license", "security", "service", "update"}},
}

// flagGroups are a run's flag groups, in order, and their flags; flags in
// none come last, under "Other flags".
var flagGroups = []struct {
	title string
	flags []string
}{
	{"Session", []string{"continue", "dir", "fork", "interactive", "name", "no-session-persistence", "ref", "resume", "worktree"}},
	{"Agent and model", []string{"agency", "agent", "effort", "model"}},
	{"One-shot run", []string{"bg", "image", "input-format", "json-schema", "max-cost-usd", "max-turns", "output-format", "plan", "prompt", "timeout"}},
	{"Permissions and sandbox", []string{"add-dir", "allow", "deny", "permission-mode", "trust-project"}},
	{"Instructions and plugins", []string{"append-system-prompt", "append-system-prompt-file", "plugin-dir"}},
}

// groupedFlagsKey marks a command whose flags the help shows in groups.
const groupedFlagsKey = "blitz/grouped-flags"

// groupHelp puts root's commands in their groups and makes the help of
// root, and of each command marked groupedFlagsKey, show flags in groups.
func groupHelp(root *cobra.Command) {
	byName := map[string]string{}
	for _, g := range commandGroups {
		root.AddGroup(&cobra.Group{ID: g.id, Title: g.title})
		for _, c := range g.commands {
			byName[c] = g.id
		}
	}
	root.SetHelpCommandGroupID("setup")
	root.SetCompletionCommandGroupID("setup")
	for _, c := range root.Commands() {
		if id, ok := byName[c.Name()]; ok {
			c.GroupID = id
		}
	}
	if root.Annotations == nil {
		root.Annotations = map[string]string{}
	}
	root.Annotations[groupedFlagsKey] = "yes"
	cobra.AddTemplateFunc("blitzFlags", flagsHelp)
	root.SetUsageTemplate(strings.Replace(root.UsageTemplate(), flagsSection, "{{blitzFlags .}}", 1))
}

// flagsSection is the local flags in cobra's usage template.
const flagsSection = `{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}`

// flagsHelp is cmd's local flags for its help: in flagGroups' groups, each
// sorted by name, for a command marked groupedFlagsKey; else as cobra
// shows them.
func flagsHelp(cmd *cobra.Command) string {
	flags := cmd.LocalFlags()
	if !cmd.HasAvailableLocalFlags() {
		return ""
	}
	if cmd.Annotations[groupedFlagsKey] == "" {
		return "\n\nFlags:\n" + strings.TrimRight(flags.FlagUsages(), " \n")
	}
	group := map[string]int{}
	for i, g := range flagGroups {
		for _, f := range g.flags {
			group[f] = i
		}
	}
	sets := make([]*pflag.FlagSet, len(flagGroups)+1)
	for i := range sets {
		sets[i] = pflag.NewFlagSet("", pflag.ContinueOnError) // sorted by name
	}
	flags.VisitAll(func(f *pflag.Flag) {
		i, ok := group[f.Name]
		if !ok {
			i = len(flagGroups) // other
		}
		sets[i].AddFlag(f)
	})
	var b strings.Builder
	for i, set := range sets {
		usages := strings.TrimRight(set.FlagUsages(), " \n")
		if usages == "" {
			continue // every flag hidden, or none
		}
		title := "Other flags"
		if i < len(flagGroups) {
			title = flagGroups[i].title + " flags"
		}
		b.WriteString("\n\n" + title + ":\n" + usages)
	}
	return b.String()
}
