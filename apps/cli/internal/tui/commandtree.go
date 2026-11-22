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
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/i18n"
)

// Source names what a command's argument can be, for completion: the
// completer asks the function registered for it (Completer.Source), or
// knows it itself (themes, modes, config keys, paths).
type Source string

// The argument sources.
const (
	SrcAgents        Source = "agents"         // the agents' names
	SrcPinned        Source = "pinned"         // agents with a pinned model
	SrcModels        Source = "models"         // every provider's chat models
	SrcModelSettings Source = "model_settings" // models in use or with settings
	SrcSessions      Source = "sessions"       // saved sessions and snapshots
	SrcThemes        Source = "themes"
	SrcStyles        Source = "styles"
	SrcPersonalities Source = "personalities"
	SrcLocales       Source = "locales"
	SrcSkills        Source = "skills"
	SrcTasks         Source = "tasks"     // background tasks' IDs
	SrcProcesses     Source = "processes" // background processes' IDs
	SrcConfig        Source = "config"    // key=value of /config
	SrcPaths         Source = "paths"     // workspace files and folders
	SrcDirs          Source = "dirs"      // folders only
	SrcCommands      Source = "commands"  // the slash commands, for /help
)

// Command is one slash command or sub-command, declared once for the
// dispatcher's check, /help and completion (spec_067_cli_parity CLP-10).
type Command struct {
	Name    string
	Aliases []string
	// Group is /help's group (an i18n key) for a top-level command.
	Group string
	// Usage is what follows the name in /help ("[name]").
	Usage string
	// Help is the description's i18n key ("" for a sub-command that has
	// none of its own).
	Help string
	Subs []*Command
	// Args are what the words after the command (and its sub-commands)
	// are, in order; the last repeats.
	Args []Source
	// Choices are fixed words the next argument can be.
	Choices []string
	Flags   []string
	// Turn marks a command the REPL itself runs (as a turn, or before the
	// dispatcher): /btw, /init, /plan, /goal, /search, … update.
	Turn bool
}

// sub declares a sub-command.
func sub(name string, args ...Source) *Command { return &Command{Name: name, Args: args} }

// Commands are the REPL's built-in slash commands.
var Commands = []*Command{
	// Session
	{Name: "btw", Group: "help.group.session", Usage: "<question>", Help: "help.btw", Turn: true},
	{Name: "checkpoints", Group: "help.group.session", Help: "help.checkpoints"},
	{Name: "clear", Group: "help.group.session", Help: "help.clear"},
	{Name: "compact", Group: "help.group.session", Usage: "[focus]", Help: "help.compact"},
	{Name: "context", Group: "help.group.session", Help: "help.context"},
	{Name: "copy", Group: "help.group.session", Usage: "[n]", Help: "help.copy"},
	{Name: "cost", Aliases: []string{"usage"}, Group: "help.group.session", Help: "help.cost"},
	{Name: "export", Group: "help.group.session", Usage: "[file]", Help: "help.export", Args: []Source{SrcPaths}},
	{Name: "fork", Group: "help.group.session", Usage: "[n]", Help: "help.fork"},
	{Name: "rename", Group: "help.group.session", Usage: "<name>", Help: "help.rename"},
	{Name: "resume", Group: "help.group.session", Usage: "[id]", Help: "help.resume", Args: []Source{SrcSessions}},
	{Name: "rewind", Group: "help.group.session", Usage: "[n [mode]] [--force]", Help: "help.rewind", Flags: []string{"--force"}},
	{Name: "session", Group: "help.group.session", Usage: "list [--all]|new|load <id|name>|save <name>", Help: "help.session", Subs: []*Command{
		{Name: "list", Flags: []string{"--all"}}, sub("new"), {Name: "load", Aliases: []string{"resume"}, Args: []Source{SrcSessions}}, sub("save"),
	}},
	{Name: "status", Group: "help.group.session", Help: "help.status"},
	{Name: "undo", Group: "help.group.session", Usage: "[--force]", Help: "help.undo", Flags: []string{"--force"}},

	// Agents and models
	{Name: "agent", Group: "help.group.agents", Usage: "[name]", Help: "help.agent", Args: []Source{SrcAgents}},
	{Name: "agents", Group: "help.group.agents", Usage: "[update]", Help: "help.agents", Subs: []*Command{{Name: "update", Turn: true}}},
	{Name: "effort", Group: "help.group.agents", Usage: "[minimal|low|medium|high|max|auto]", Help: "help.effort", Choices: efforts},
	{Name: "model", Group: "help.group.agents", Usage: "[name] [--new] | --refresh", Help: "help.model", Args: []Source{SrcModels}, Flags: []string{"--new", "--refresh"}},
	{Name: "model_settings", Group: "help.group.agents", Usage: "[<model> [key=value…|reset]]", Help: "help.model_settings", Args: []Source{SrcModelSettings}},
	{Name: "pin_model", Aliases: []string{"pin"}, Group: "help.group.agents", Usage: "[<agent> <model>]", Help: "help.pin", Args: []Source{SrcAgents, SrcModels}},
	{Name: "personality", Group: "help.group.agents", Usage: "[name]", Help: "help.personality", Args: []Source{SrcPersonalities}},
	{Name: "style", Group: "help.group.agents", Usage: "[name]", Help: "help.style", Args: []Source{SrcStyles}},
	{Name: "unpin", Group: "help.group.agents", Usage: "<agent>", Help: "help.unpin", Args: []Source{SrcPinned}},

	// Work
	{Name: "data", Group: "help.group.work", Help: "help.data_cli"},
	{Name: "goal", Group: "help.group.work", Usage: "[<condition>|clear]", Help: "help.goal", Turn: true, Subs: []*Command{sub("clear")}},
	{Name: "loop", Group: "help.group.work", Usage: "[<interval> <prompt>|stop <n>]", Help: "help.loop", Subs: []*Command{sub("stop")}},
	{Name: "plan", Group: "help.group.work", Usage: "<goal>", Help: "help.plan", Turn: true},
	{Name: "processes", Aliases: []string{"ps"}, Group: "help.group.work", Usage: "[all|log <id>|stop <id>…|stop flagged|stop all]", Help: "help.processes", Subs: []*Command{
		sub("all"), sub("log", SrcProcesses), {Name: "stop", Args: []Source{SrcProcesses}, Choices: []string{"flagged", "all"}},
	}},
	{Name: "search", Group: "help.group.work", Usage: "[files|documents|chats|notes|all|web|session] <terms>, /search status|reindex", Help: "help.search", Turn: true, Subs: []*Command{
		sub("files"), sub("documents"), sub("chats"), sub("notes"), sub("all"), sub("web"), sub("session"), sub("status"), sub("reindex"),
	}},
	{Name: "tasks", Group: "help.group.work", Usage: "[show|stop <id>]", Help: "help.tasks", Subs: []*Command{sub("show", SrcTasks), sub("stop", SrcTasks)}},

	// Workspace
	{Name: "attach", Group: "help.group.workspace", Usage: "[path|clear]", Help: "help.attach", Args: []Source{SrcPaths}, Subs: []*Command{sub("clear")}},
	{Name: "cd", Group: "help.group.workspace", Usage: "[path]", Help: "help.cd", Args: []Source{SrcDirs}},
	{Name: "diff", Group: "help.group.workspace", Usage: "[git]", Help: "help.diff", Subs: []*Command{sub("git")}},
	{Name: "init", Group: "help.group.workspace", Help: "help.init", Turn: true},
	{Name: "memory", Group: "help.group.workspace", Usage: "[show|reload|add <note>|notes|forget <name>]", Help: "help.memory", Subs: []*Command{
		sub("show"), sub("reload"), sub("add"), sub("notes"), sub("forget"),
	}},
	{Name: "paste", Group: "help.group.workspace", Help: "help.paste"},
	{Name: "reload", Group: "help.group.workspace", Help: "help.reload"},
	{Name: "rules", Group: "help.group.workspace", Usage: "update", Help: "help.rules", Subs: []*Command{{Name: "update", Turn: true}}},
	{Name: "trust", Group: "help.group.workspace", Help: "help.trust"},

	// Safety
	{Name: "approvals", Group: "help.group.safety", Usage: "[revoke <n>|clear]", Help: "help.approvals", Subs: []*Command{sub("revoke"), sub("clear")}},
	{Name: "hooks", Group: "help.group.safety", Help: "help.hooks"},
	{Name: "mode", Group: "help.group.safety", Usage: "[auto|default|accept-edits|plan|dont-ask|bypass|unsafe]", Help: "help.mode", Choices: modes},
	{Name: "permissions", Aliases: []string{"perms"}, Group: "help.group.safety", Usage: "[allow|ask|deny|remove <rule> [--save]]", Help: "help.permissions", Subs: []*Command{
		{Name: "allow", Flags: []string{"--save"}}, {Name: "ask", Flags: []string{"--save"}}, {Name: "deny", Flags: []string{"--save"}}, {Name: "remove", Aliases: []string{"rm"}, Flags: []string{"--save"}},
	}},
	{Name: "sandbox", Group: "help.group.safety", Help: "help.sandbox"},

	// Extensions
	{Name: "envs", Group: "help.group.extensions", Usage: "[prune|remove <key>]", Help: "help.envs", Subs: []*Command{sub("prune"), sub("remove")}},
	{Name: "mcp", Group: "help.group.extensions", Help: "help.mcp"},
	{Name: "skills", Group: "help.group.extensions", Usage: "list|show <name>|search <q>|registry|install <name> [--project]|update", Help: "help.skills", Subs: []*Command{
		sub("list"), sub("show", SrcSkills), sub("search"), sub("registry"), {Name: "install", Flags: []string{"--project"}}, {Name: "update", Turn: true},
	}},
	{Name: "tools", Group: "help.group.extensions", Help: "help.tools"},

	// Settings
	{Name: "config", Aliases: []string{"set", "show"}, Group: "help.group.settings", Usage: "[key=value [--save]]", Help: "help.set", Args: []Source{SrcConfig}, Flags: []string{"--save"}},
	{Name: "docs", Group: "help.group.settings", Usage: "[page|words]", Help: "help.docs_cli"},
	{Name: "exit", Aliases: []string{"quit"}, Group: "help.group.settings", Help: "help.exit"},
	{Name: "help", Group: "help.group.settings", Usage: "[command|keys|question]", Help: "help.help", Args: []Source{SrcCommands}, Choices: []string{"keys"}},
	{Name: "license", Group: "help.group.settings", Usage: "[full|third-party]", Help: "help.license", Subs: []*Command{sub("full"), sub("third-party")}},
	{Name: "locale", Aliases: []string{"lang", "language"}, Group: "help.group.settings", Usage: "[code]", Help: "help.locale", Args: []Source{SrcLocales}},
	{Name: "theme", Group: "help.group.settings", Usage: "[name [--save]]", Help: "help.theme", Args: []Source{SrcThemes}, Flags: []string{"--save"}},
}

// The words /mode and /effort take.
var (
	modes   = []string{"auto", "default", "accept-edits", "plan", "dont-ask", "bypass", "unsafe"}
	efforts = []string{"minimal", "low", "medium", "high", "max", "auto"}
	agency  = []string{"low", "medium", "high", "extreme"}
)

// FindCommand is the built-in command named name or one of its aliases
// (without its /), or nil.
func FindCommand(name string) *Command { return findIn(Commands, name) }

func findIn(cmds []*Command, name string) *Command {
	name = strings.ToLower(name)
	for _, c := range cmds {
		if c.Name == name || slices.Contains(c.Aliases, name) {
			return c
		}
	}
	return nil
}

// names are a command's name and aliases.
func (c *Command) names() []string { return append([]string{c.Name}, c.Aliases...) }

// nearest is the built-in command closest to name (by edit distance, at
// most a third of its length, and 1 for short names), for "did you
// mean" (CLP-24); "" when none is close.
func nearest(cmds []*Command, name string) string {
	best, bestD := "", max(1, len([]rune(name))/3)+1
	for _, c := range cmds {
		for _, n := range c.names() {
			if d := editDistance(strings.ToLower(name), n); d < bestD {
				best, bestD = c.Name, d
			}
		}
	}
	return best
}

// editDistance is the edit distance between a and b, a swap of two
// letters side by side counting one (optimal string alignment): "modle"
// is one from "model".
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}

// ShellUsage is how /help shows running a shell command.
const ShellUsage = "!<command>"

// printCommandHelp is /help <command> [sub-command]: its usage, what it
// does, its sub-commands, flags and choices, and its other names
// (CLP-20).
func printCommandHelp(app *App, args []string) {
	name := strings.TrimPrefix(strings.ToLower(args[0]), "/")
	if name == "keys" {
		fmt.Println()
		printKeyHints()
		return
	}
	cmd := FindCommand(name)
	if cmd == nil {
		if isCustomCommand(app, name) {
			for _, c := range app.Workspace.ListCommands() {
				if c.Name == name {
					fmt.Printf("\n  %s/%s %s%s\n  %s\n\n", Bold, safe(c.Name), safe(c.ArgumentHint), Reset, safe(c.Description))
				}
			}
			return
		}
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("command.unknown", "command", safe(name)), Reset)
		if near := nearest(Commands, name); near != "" {
			fmt.Printf("%s%s%s\n", Dim, i18n.T("command.did_you_mean", "command", "/"+near), Reset)
		}
		return
	}
	usage := "/" + cmd.Name
	if cmd.Usage != "" {
		usage += " " + cmd.Usage
	}
	fmt.Printf("\n  %s%s%s\n", Bold, usage, Reset)
	if cmd.Help != "" {
		fmt.Printf("  %s\n", i18n.T(cmd.Help))
	}
	var subs []string
	for _, s := range cmd.Subs {
		subs = append(subs, s.Name)
	}
	rows := [][2]string{
		{i18n.T("help.detail.subcommands"), strings.Join(subs, ", ")},
		{i18n.T("help.detail.choices"), strings.Join(cmd.Choices, ", ")},
		{i18n.T("help.detail.flags"), strings.Join(cmd.Flags, ", ")},
		{i18n.T("help.detail.aliases"), "/" + strings.Join(cmd.Aliases, ", /")},
	}
	if len(cmd.Aliases) == 0 {
		rows[3][1] = ""
	}
	fmt.Println()
	for _, r := range rows {
		if r[1] != "" {
			fmt.Printf("  %s%-14s%s %s\n", Dim, r[0], Reset, r[1])
		}
	}
	fmt.Printf("  %s%s%s\n\n", Dim, i18n.T("help.detail.tab"), Reset)
}

// unknownSub says a command has no such sub-command, the nearest one it
// has, and its usage (CLP-24), rather than doing nothing.
func unknownSub(command, sub string) {
	cmd := FindCommand(command)
	fmt.Printf("%s%s%s\n", Yellow, i18n.T("command.unknown_sub", "command", "/"+command, "sub", safe(sub)), Reset)
	if cmd == nil {
		return
	}
	if near := nearest(cmd.Subs, sub); near != "" {
		fmt.Printf("%s%s%s\n", Dim, i18n.T("command.did_you_mean", "command", "/"+command+" "+near), Reset)
	}
	fmt.Printf("%s/%s %s%s\n", Dim, cmd.Name, cmd.Usage, Reset)
}
