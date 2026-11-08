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

package api

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// Blitz's own commands, which the agent runs with the blitz tool and a
// person with the blitz CLI (spec_060_blitz_tools DRV-01, DRV-02): one
// catalog, so both have the same names and arguments.

// CommandEffect is what a command does, which decides where it may run
// (DRV-70): reads and opens run in plan mode; changes and costs don't.
type CommandEffect string

// The effects.
const (
	EffectReads   CommandEffect = "reads"
	EffectOpens   CommandEffect = "opens"
	EffectChanges CommandEffect = "changes"
	EffectCosts   CommandEffect = "costs"
)

// ActionBlitz is the approval kind of a blitz command (DRV-04), and a
// job's permission kind for one ("blitz:connector save").
const ActionBlitz ActionKind = "blitz"

// CommandArg is one of a command's arguments.
type CommandArg struct {
	Name string
	// Type is string, integer, boolean, object (a JSON object) or list (a
	// list of strings).
	Type        string
	Description string
	Required    bool
	// Values are the values it may take, when they're few.
	Values []string
}

// BlitzCommand is one command of the catalog.
type BlitzCommand struct {
	// Name is the command as typed: its group, then its verb
	// ("connector save"), or the group alone ("doctor").
	Name    string
	Summary string
	Effect  CommandEffect
	Args    []CommandArg
	// BringsCode marks a command that adds something that runs code (an
	// MCP server, a hook, a plugin): always asked, with the code in view
	// (DRV-75).
	BringsCode bool
	// Phase is the spec's phase it came in.
	Phase int
}

// Group is the command's group: its first word.
func (c BlitzCommand) Group() string { g, _, _ := strings.Cut(c.Name, " "); return g }

// Usage is the command and its arguments as help shows them.
func (c BlitzCommand) Usage() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s (%s)", c.Name, c.Summary, c.Effect)
	for _, a := range c.Args {
		req := "optional"
		if a.Required {
			req = "required"
		}
		fmt.Fprintf(&b, "\n  %s (%s, %s): %s", a.Name, a.Type, req, a.Description)
		if len(a.Values) > 0 {
			fmt.Fprintf(&b, " One of: %s.", strings.Join(a.Values, ", "))
		}
	}
	return b.String()
}

// The arguments several commands share.
var (
	argName = CommandArg{Name: "name", Type: "string", Description: "Its name", Required: true}
	argDesc = CommandArg{Name: "description", Type: "string", Description: "One line saying what it is"}
	argPath = CommandArg{Name: "path", Type: "string", Description: "The workspace file, relative to the workspace", Required: true}
)

// BlitzCommands is the catalog, in the order help lists it.
var BlitzCommands = []BlitzCommand{
	{Name: "help", Summary: "The commands, or one command's arguments", Effect: EffectReads, Phase: 1,
		Args: []CommandArg{{Name: "command", Type: "string", Description: "A command or group (\"connector save\", \"connector\"); empty: every group"}}},

	{Name: "connector list", Summary: "The connection kinds with their fields, and the connections with whether each is on, trusted, read-only and working", Effect: EffectReads, Phase: 1},
	{Name: "connector save", Summary: "Add or change a connection of this workspace (a database), asked first with the file it writes; a secret goes as ${NAME}, and its value is asked of the person, never given here", Effect: EffectChanges, Phase: 1,
		Args: []CommandArg{
			{Name: "kind", Type: "string", Description: "The connection's kind, from connector list (sqlite, postgres, bigquery, …)", Required: true},
			argName, argDesc,
			{Name: "settings", Type: "object", Description: "The kind's fields, by name (a SQLite database: {\"database\": \"recipes.db\"}, relative to the workspace); a secret as \"${NAME}\""},
			{Name: "read_only", Type: "boolean", Description: "Allow reading only"},
			{Name: "queries", Type: "list", Description: "Saved queries, each a JSON object with name, description, statement and parameters"},
		}},
	{Name: "connector enable", Summary: "Turn a connection on in this workspace", Effect: EffectChanges, Phase: 1, Args: []CommandArg{argName}},
	{Name: "connector disable", Summary: "Turn a connection off in this workspace", Effect: EffectChanges, Phase: 1, Args: []CommandArg{argName}},

	{Name: "settings show", Summary: "The model (the conversation's and the workspace's), image and speech models, effort, permission mode and agent", Effect: EffectReads, Phase: 1},
	{Name: "settings set", Summary: "Change one setting: the model, image or speech model, effort, or the permission mode (only to a stricter one)", Effect: EffectChanges, Phase: 1,
		Args: []CommandArg{
			{Name: "key", Type: "string", Description: "What to change", Required: true, Values: []string{"model", "image_model", "speech_model", "effort", "mode"}},
			{Name: "value", Type: "string", Description: "The new value (a model as provider/name)", Required: true},
		}},
	{Name: "doctor", Summary: "Check Blitz here: the model and its provider, Ollama, the mode, the sandbox, MCP servers, connections, language servers", Effect: EffectReads, Phase: 1},
	{Name: "org policy", Summary: "What your organization's policy allows and locks", Effect: EffectReads, Phase: 1},
	{Name: "org status", Summary: "Your organization: signed in or not, its catalog's version, problems", Effect: EffectReads, Phase: 1},

	{Name: "open url", Summary: "Open a web page for the person in Blitz's browser", Effect: EffectOpens, Phase: 2,
		Args: []CommandArg{{Name: "url", Type: "string", Description: "The address", Required: true}}},
	{Name: "open file", Summary: "Open a workspace file in Blitz's editor", Effect: EffectOpens, Phase: 2,
		Args: []CommandArg{argPath, {Name: "line", Type: "integer", Description: "The line to show"}}},
	{Name: "open studio", Summary: "Open a section of Studio", Effect: EffectOpens, Phase: 2,
		Args: []CommandArg{{Name: "section", Type: "string", Description: "Which section", Required: true, Values: []string{"overview", "specs", "agents", "evals", "skills", "rules", "guardrails", "jobs", "workflows", "connectors", "secrets"}}}},
	{Name: "open settings", Summary: "Open a section of Settings", Effect: EffectOpens, Phase: 2,
		Args: []CommandArg{{Name: "section", Type: "string", Description: "Which section", Required: true, Values: []string{"providers", "connectors", "browser", "appearance", "workspaces", "secrets", "updates"}}}},
	{Name: "open view", Summary: "Open one of Blitz's views", Effect: EffectOpens, Phase: 2,
		Args: []CommandArg{{Name: "view", Type: "string", Description: "Which view", Required: true, Values: []string{"changes", "processes", "pictures"}}}},
	{Name: "picture", Summary: "Open Pictures with a prompt for the person to make it there (its cost shown first); without a Blitz window, use generate_image instead", Effect: EffectOpens, Phase: 2,
		Args: []CommandArg{
			{Name: "prompt", Type: "string", Description: "What to draw", Required: true},
			{Name: "model", Type: "string", Description: "An image model; empty: the workspace's"},
			{Name: "references", Type: "list", Description: "Workspace pictures to start from or match"},
		}},
	{Name: "eval list", Summary: "The evals of this workspace's agents and skills", Effect: EffectReads, Phase: 2},
	{Name: "eval run", Summary: "Run an agent's or a skill's evals (they call models: asked first)", Effect: EffectCosts, Phase: 2,
		Args: []CommandArg{{Name: "name", Type: "string", Description: "The eval set (an agent's or skill's name)", Required: true}}},
	{Name: "eval results", Summary: "An eval set's last runs", Effect: EffectReads, Phase: 2,
		Args: []CommandArg{{Name: "name", Type: "string", Description: "The eval set", Required: true}}},
	{Name: "spec list", Summary: "The project's specs, in order, with their state", Effect: EffectReads, Phase: 2},
	{Name: "spec review", Summary: "Check a spec as written (free); with judge, a model reviews it (costs)", Effect: EffectReads, Phase: 2,
		Args: []CommandArg{{Name: "path", Type: "string", Description: "The spec's file", Required: true}, {Name: "judge", Type: "boolean", Description: "Have a model review it too (asked first: it costs)"}}},
	{Name: "spec verify", Summary: "Trace an approved spec's requirements through the code (free); with judge, a model checks them (costs)", Effect: EffectReads, Phase: 2,
		Args: []CommandArg{{Name: "path", Type: "string", Description: "The spec's file", Required: true}, {Name: "judge", Type: "boolean", Description: "Have a model check each requirement (asked first: it costs)"}}},
	{Name: "spec implement", Summary: "What's left of a spec, as the prompt to carry it out", Effect: EffectReads, Phase: 2,
		Args: []CommandArg{{Name: "path", Type: "string", Description: "The spec's file", Required: true}}},
	{Name: "job list", Summary: "This workspace's scheduled jobs", Effect: EffectReads, Phase: 2},
	{Name: "job create", Summary: "Create a job that runs a prompt on a schedule (as create_job: the person approves it, and it starts paused)", Effect: EffectChanges, Phase: 2,
		Args: []CommandArg{argName, argDesc,
			{Name: "schedule", Type: "string", Description: "When it runs, in plain English", Required: true},
			{Name: "prompt", Type: "string", Description: "What each run does, everything it needs", Required: true},
			{Name: "permissions", Type: "list", Description: "What each run may do without asking, kind:pattern"}}},
	{Name: "memory list", Summary: "What Blitz remembers here (its notes)", Effect: EffectReads, Phase: 2},
	{Name: "memory forget", Summary: "Forget one note", Effect: EffectChanges, Phase: 2, Args: []CommandArg{argName}},
	{Name: "chat new", Summary: "Start a new conversation (from the next message)", Effect: EffectChanges, Phase: 2},
	{Name: "chat export", Summary: "This conversation as Markdown, written to a workspace file (asked first)", Effect: EffectChanges, Phase: 2,
		Args: []CommandArg{{Name: "path", Type: "string", Description: "Where to write it, relative to the workspace", Required: true}}},
	{Name: "chat search", Summary: "Search past conversations", Effect: EffectReads, Phase: 2,
		Args: []CommandArg{{Name: "terms", Type: "string", Description: "What to look for", Required: true}}},
	{Name: "process list", Summary: "The background processes running here", Effect: EffectReads, Phase: 2},
	{Name: "process stop", Summary: "Stop a background process (asked first)", Effect: EffectChanges, Phase: 2,
		Args: []CommandArg{{Name: "id", Type: "integer", Description: "The process's number, from process list", Required: true}}},
	{Name: "approval list", Summary: "The actions always allowed (saved approvals)", Effect: EffectReads, Phase: 2},
	{Name: "approval forget", Summary: "Stop always allowing one (Blitz asks again)", Effect: EffectChanges, Phase: 2,
		Args: []CommandArg{{Name: "key", Type: "string", Description: "The approval, from approval list", Required: true}}},
	{Name: "secret list", Summary: "The secrets' names (never their values)", Effect: EffectReads, Phase: 2},
	{Name: "secret set", Summary: "Set a secret of this workspace: its value is asked of the person, never given here", Effect: EffectChanges, Phase: 2,
		Args: []CommandArg{argName}},

	{Name: "workflow list", Summary: "This workspace's workflows and their steps", Effect: EffectReads, Phase: 3},
	{Name: "workflow save", Summary: "Add or change a workflow (its file, as Studio writes it), asked first", Effect: EffectChanges, Phase: 3,
		Args: []CommandArg{{Name: "workflow", Type: "object", Description: "The workflow as JSON, as workflow list shows one", Required: true}, {Name: "previous", Type: "string", Description: "Its name before, when renaming"}}},
	{Name: "agent list", Summary: "The agents, with their tools and models", Effect: EffectReads, Phase: 3},
	{Name: "agent save", Summary: "Add or change an agent of this workspace (its file), asked first", Effect: EffectChanges, Phase: 3,
		Args: []CommandArg{argName, argDesc,
			{Name: "instructions", Type: "string", Description: "What it does and how", Required: true},
			{Name: "tools", Type: "list", Description: "The tools it may use"},
			{Name: "model", Type: "string", Description: "provider/model; empty: the workspace's"}}},
	{Name: "skill list", Summary: "The skills", Effect: EffectReads, Phase: 3},
	{Name: "skill save", Summary: "Add or change a skill of this workspace (its SKILL.md), asked first", Effect: EffectChanges, Phase: 3,
		Args: []CommandArg{argName, {Name: "description", Type: "string", Description: "When to use it: what makes an agent load it", Required: true},
			{Name: "instructions", Type: "string", Description: "Its body", Required: true}}},

	{Name: "mcp list", Summary: "The MCP servers, on or off, and their state", Effect: EffectReads, Phase: 4},
	{Name: "mcp add", Summary: "Add an MCP server to this workspace: it runs code, so it's asked with the exact command or address", Effect: EffectChanges, BringsCode: true, Phase: 4,
		Args: []CommandArg{argName,
			{Name: "command", Type: "string", Description: "The program that serves it (stdio)"},
			{Name: "args", Type: "list", Description: "Its arguments"},
			{Name: "url", Type: "string", Description: "Or its address (HTTP)"}}},
	{Name: "mcp enable", Summary: "Turn an MCP server on here: it runs code, so it's asked", Effect: EffectChanges, BringsCode: true, Phase: 4, Args: []CommandArg{argName}},
	{Name: "mcp disable", Summary: "Turn an MCP server off here", Effect: EffectChanges, Phase: 4, Args: []CommandArg{argName}},
	{Name: "rule list", Summary: "The workspace's rules", Effect: EffectReads, Phase: 4},
	{Name: "rule save", Summary: "Add or change a rule, asked first", Effect: EffectChanges, Phase: 4,
		Args: []CommandArg{argName, argDesc, {Name: "text", Type: "string", Description: "The rule", Required: true}}},
	{Name: "guardrail list", Summary: "The workspace's guardrails", Effect: EffectReads, Phase: 4},
	{Name: "guardrail save", Summary: "Add or change a guardrail, asked first", Effect: EffectChanges, Phase: 4,
		Args: []CommandArg{argName, argDesc,
			{Name: "stage", Type: "string", Description: "What it checks: the person's message, a tool's result, the answer, or an action", Required: true, Values: []string{"input", "tool_result", "output", "action"}},
			{Name: "action", Type: "string", Description: "What it does when it catches something (block: the default)", Values: []string{"block", "warn"}},
			{Name: "check", Type: "string", Description: "What to catch, in words", Required: true}}},
	{Name: "command list", Summary: "The slash commands", Effect: EffectReads, Phase: 4},
	{Name: "command save", Summary: "Add or change a slash command of this workspace (its file), asked first", Effect: EffectChanges, Phase: 4,
		Args: []CommandArg{argName, argDesc, {Name: "prompt", Type: "string", Description: "The prompt it runs ($ARGUMENTS for what's typed after it)", Required: true}}},
	{Name: "hook list", Summary: "The hooks", Effect: EffectReads, Phase: 4},
	{Name: "plugin list", Summary: "The plugins loaded here", Effect: EffectReads, Phase: 4},
}

// FindBlitzCommand is the catalog's command named name (its words, any
// case and spacing).
func FindBlitzCommand(name string) (BlitzCommand, bool) {
	name = strings.Join(strings.Fields(strings.ToLower(name)), " ")
	i := slices.IndexFunc(BlitzCommands, func(c BlitzCommand) bool { return c.Name == name })
	if i < 0 {
		return BlitzCommand{}, false
	}
	return BlitzCommands[i], true
}

// BlitzGroups are the catalog's groups, in order, each with its commands.
func BlitzGroups() [][]BlitzCommand {
	var out [][]BlitzCommand
	at := map[string]int{}
	for _, c := range BlitzCommands {
		g := c.Group()
		i, ok := at[g]
		if !ok {
			i = len(out)
			at[g] = i
			out = append(out, nil)
		}
		out[i] = append(out[i], c)
	}
	return out
}

// BlitzCommandRunsInPlan reports whether the command (as the blitz tool
// was called with it) may run in plan mode: one that reads or opens
// (DRV-70). An unknown command may: the tool refuses it with help.
func BlitzCommandRunsInPlan(name string) bool {
	c, ok := FindBlitzCommand(name)
	return !ok || c.Effect == EffectReads || c.Effect == EffectOpens
}

// BlitzRun is a command's run for the CLI (blitz do, DRV-02): its result,
// or what the person confirms before it runs (Confirm, with the file it
// writes as Diff, and the secrets it needs, Asks), or help.
type BlitzRun struct {
	Result  any
	Help    string
	Confirm string
	Diff    string
	Asks    []string
}

type secretAskKey struct{}

// WithSecretAnswer marks a question asked with ctx as asking for a secret's
// value: the answer is typed hidden, and is never shown, recorded or given
// to the model (spec_060_blitz_tools DRV-05).
func WithSecretAnswer(ctx context.Context) context.Context {
	return context.WithValue(ctx, secretAskKey{}, true)
}

// SecretAnswer reports whether a question asked with ctx asks for a secret.
func SecretAnswer(ctx context.Context) bool {
	v, _ := ctx.Value(secretAskKey{}).(bool)
	return v
}
