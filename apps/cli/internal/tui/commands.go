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
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/skillreg"
)

// ErrExit is returned by HandleCommand when the user asks to quit.
var ErrExit = errors.New("exit")

// HandleCommand processes slash commands. Returns true if input was a slash command.
func HandleCommand(ctx context.Context, input string, app *App) (bool, error) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return false, nil
	}

	parts := strings.Fields(trimmed[1:])
	if len(parts) == 0 {
		return true, nil
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]
	if cmd == "show" { // Python's /show: the settings, like /set with no arguments
		cmd, args = "set", nil
	}

	switch cmd {
	case "help":
		printHelp(app)

	case "agents":
		fmt.Printf("\n%s%s:%s\n", Bold, i18n.T("agents.title"), Reset)
		for _, a := range app.Workspace.ListAgents() {
			marker := "  "
			if a.Active {
				marker = "› "
			}
			pin := ""
			if a.PinnedModel != "" {
				pin = fmt.Sprintf(" %s[pinned %s]%s", Cyan, safe(a.PinnedModel), Reset)
			}
			fmt.Printf("%s %s%s%s (%s)%s: %s\n", marker, Bold, safe(a.DisplayName), Reset, safe(a.Name), pin, safe(a.Description))
		}
		fmt.Println()

	case "agent":
		if len(args) == 0 {
			if name, ok := pickAgent(ctx, app); ok {
				return HandleCommand(ctx, "/agent "+name, app)
			}
			a := app.Workspace.ActiveAgent()
			fmt.Println(i18n.T("agent.current", "name", Bold+safe(a.DisplayName)+Reset, "id", safe(a.Name)))
			return true, nil
		}
		a, err := app.Workspace.SetAgent(ctx, args[0])
		if err != nil {
			fmt.Printf("%s✗ %s%s\n", Red, i18n.T("agent.switch_failed", "error", safe(err.Error())), Reset)
		} else {
			fmt.Printf("%s %s%s\n", Green, i18n.T("agent.switched", "name", Bold+safe(a.DisplayName)), Reset)
		}

	case "model":
		if len(args) == 0 {
			if ref, ok := pickModel(ctx, app); ok {
				return HandleCommand(ctx, "/model "+ref, app)
			}
			m := app.Workspace.Model()
			fmt.Println(i18n.T("model.current", "model", Cyan+safe(m.Name)+Reset, "provider", m.Provider))
			return true, nil
		}
		pin, err := app.Workspace.SetModel(ctx, args[0])
		if err != nil {
			fmt.Printf("%s✗ %s%s\n", Red, i18n.T("model.switch_failed", "error", safe(err.Error())), Reset)
			return true, nil
		}
		fmt.Printf("%s %s%s\n", Green, i18n.T("model.set", "model", Cyan+safe(args[0])), Reset)
		if pin != "" {
			fmt.Printf("%s%s%s\n", Dim, i18n.T("pin.active_pinned", "agent", app.Workspace.ActiveAgent().Name, "model", safe(pin)), Reset)
		}

	case "skills":
		handleSkillsCommand(args, app)

	case "session":
		handleSessionCommand(args, app)

	case "set", "config":
		cmdConfig(ctx, args, app)

	case "mode":
		cmdMode(args, app)

	case "effort":
		cmdEffort(ctx, args, app)

	case "permissions", "perms":
		cmdPermissions(args, app)

	case "clear":
		fmt.Print("\033[H\033[2J")

	case "sandbox":
		fmt.Printf("\n%s%s:%s\n", Bold, i18n.T("sandbox.title"), Reset)
		for _, line := range app.Workspace.SandboxSummary() {
			fmt.Printf("  %s\n", safe(line))
		}
		fmt.Println()

	case "tasks":
		cmdTasks(args, app)

	case "cd":
		cmdCd(ctx, args, app)

	case "trust":
		p := app.Workspace.ProjectSettings()
		fmt.Println()
		ShowProject(os.Stdout, p)
		if len(p.Pending) > 0 && p.Hash != "" && app.Input != nil {
			DecideTrust(ctx, app.Input, os.Stdout, app.Workspace, p)
		}
		fmt.Println()

	case "license":
		which := ""
		if len(args) > 0 {
			which = strings.ToLower(args[0])
		}
		text, err := LicenseText(which, "/license")
		if err != nil {
			fmt.Printf("%s%s%s\n", Red, err, Reset)
			break
		}
		fmt.Println()
		if err := Page(os.Stdout, text); err != nil {
			fmt.Printf("%s%s%s\n", Red, err, Reset)
		}
		fmt.Println()

	case "exit", "quit":
		return true, ErrExit

	default:
		if handleExtraCommand(ctx, cmd, args, app) {
			return true, nil
		}
		if isCustomCommand(app, cmd) {
			return false, nil // the REPL runs it as a turn
		}
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("command.unknown", "command", safe(cmd)), Reset)
	}

	return true, nil
}

// helpGroups are /help's built-in commands in groups (shown in this
// order, each sorted by command): usage, and the description's key.
var helpGroups = []struct {
	title string // its i18n key
	rows  [][2]string
}{
	{"help.group.session", [][2]string{
		{"/btw <question>", "help.btw"},
		{"/checkpoints", "help.checkpoints"},
		{"/clear", "help.clear"},
		{"/compact [focus]", "help.compact"},
		{"/context", "help.context"},
		{"/copy [n]", "help.copy"},
		{"/cost", "help.cost"},
		{"/export [file]", "help.export"},
		{"/fork [n]", "help.fork"},
		{"/rename <name>", "help.rename"},
		{"/rewind [n [mode]] [--force]", "help.rewind"},
		{"/session list [--all]|new|load <id|name>|save <name>", "help.session"},
		{"/status", "help.status"},
		{"/undo [--force]", "help.undo"},
	}},
	{"help.group.agents", [][2]string{
		{"/agent [name]", "help.agent"},
		{"/agents", "help.agents"},
		{"/effort [minimal|low|medium|high|max|auto]", "help.effort"},
		{"/model [name]", "help.model"},
		{"/model_settings [<model> [key=value…|reset]]", "help.model_settings"},
		{"/pin_model [<agent> <model>]", "help.pin"},
		{"/style [name]", "help.style"},
		{"/unpin <agent>", "help.unpin"},
	}},
	{"help.group.work", [][2]string{
		{"/goal [<condition>|clear]", "help.goal"},
		{"/loop [<interval> <prompt>|stop <n>]", "help.loop"},
		{"/plan <goal>", "help.plan"},
		{"/search web|session <terms>", "help.search"},
		{"/tasks [show|stop <id>]", "help.tasks"},
	}},
	{"help.group.workspace", [][2]string{
		{"!<command>", "help.shell"},
		{"/attach [path|clear]", "help.attach"},
		{"/cd [path]", "help.cd"},
		{"/diff [git]", "help.diff"},
		{"/init", "help.init"},
		{"/memory [reload|add <note>]", "help.memory"},
		{"/reload", "help.reload"},
		{"/paste", "help.paste"},
		{"/trust", "help.trust"},
	}},
	{"help.group.safety", [][2]string{
		{"/approvals [revoke <n>|clear]", "help.approvals"},
		{"/hooks", "help.hooks"},
		{"/mode [default|accept-edits|plan|dont-ask|bypass]", "help.mode"},
		{"/permissions [allow|ask|deny|remove <rule> [--save]]", "help.permissions"},
		{"/sandbox", "help.sandbox"},
	}},
	{"help.group.extensions", [][2]string{
		{"/envs [prune|remove <key>]", "help.envs"},
		{"/mcp", "help.mcp"},
		{"/skills list|show <name>|search <q>", "help.skills"},
		{"/tools", "help.tools"},
	}},
	{"help.group.settings", [][2]string{
		{"/config [key=value [--save]], /set", "help.set"},
		{"/exit, /quit", "help.exit"},
		{"/license [full|third-party]", "help.license"},
		{"/locale [code]", "help.locale"},
		{"/theme [name [--save]]", "help.theme"},
	}},
}

// customSources is the order /help lists custom commands in, by source
// ("mcp" for every MCP server's prompts); others follow, by name.
var customSources = []string{"bundled", "project", "user", "plugin", "skill", "mcp"}

func printHelp(app *App) {
	fmt.Printf("\n%s%s%s\n", Bold, i18n.T("help.title"), Reset)
	for _, g := range helpGroups {
		fmt.Printf("\n%s%s%s\n", Bold, i18n.T(g.title), Reset)
		for _, r := range sortedRows(g.rows) {
			fmt.Printf("  %s%-36s%s %s\n", Bold, r[0], Reset, i18n.T(r[1]))
		}
	}
	if cmds := app.Workspace.ListCommands(); len(cmds) > 0 {
		fmt.Printf("\n%s%s%s\n", Bold, i18n.T("help.custom_title"), Reset)
		for _, g := range customGroups(cmds) {
			fmt.Printf("\n%s%s%s\n", Bold, sourceTitle(g.source), Reset)
			for _, c := range g.cmds {
				usage := "/" + c.Name
				if c.ArgumentHint != "" {
					usage += " " + c.ArgumentHint
				}
				from := ""
				if _, detail := sourceKind(c.Source); detail != "" { // the MCP server, the plugin
					from = fmt.Sprintf(" %s(%s)%s", Dim, safe(detail), Reset)
				}
				fmt.Printf("  %s%-36s%s %s%s\n", Bold, safe(usage), Reset, safe(c.Description), from)
			}
		}
	}
	fmt.Printf("\n  %s%s%s\n\n", Dim, i18n.T("help.input"), Reset)
}

// sortedRows is rows by command, without its / or !.
func sortedRows(rows [][2]string) [][2]string {
	out := slices.Clone(rows)
	key := func(r [2]string) string { return strings.TrimLeft(r[0], "/!") }
	slices.SortStableFunc(out, func(a, b [2]string) int { return strings.Compare(key(a), key(b)) })
	return out
}

type commandGroup struct {
	source string
	cmds   []api.CommandInfo
}

// customGroups is cmds by source, in customSources' order then the rest by
// name, each sorted by name.
func customGroups(cmds []api.CommandInfo) []commandGroup {
	bySource := map[string][]api.CommandInfo{}
	for _, c := range cmds {
		src, _ := sourceKind(c.Source)
		bySource[src] = append(bySource[src], c)
	}
	order := slices.Clone(customSources)
	var rest []string
	for src := range bySource {
		if !slices.Contains(order, src) {
			rest = append(rest, src)
		}
	}
	slices.Sort(rest)
	var out []commandGroup
	for _, src := range append(order, rest...) {
		list := bySource[src]
		if len(list) == 0 {
			continue
		}
		slices.SortFunc(list, func(a, b api.CommandInfo) int { return strings.Compare(a.Name, b.Name) })
		out = append(out, commandGroup{src, list})
	}
	return out
}

// sourceKind splits a command's source into its kind and what it names:
// "mcp:github" is ("mcp", "github"), "plugin tools" ("plugin", "tools").
func sourceKind(source string) (kind, detail string) {
	if i := strings.IndexAny(source, ": "); i >= 0 {
		return source[:i], source[i+1:]
	}
	return source, ""
}

// sourceTitle is a custom commands group's heading: the source's own
// words, or the source as it is when it has none.
func sourceTitle(source string) string {
	key := "help.source." + source
	if t := i18n.T(key); t != key {
		return t
	}
	return source
}

// isCustomCommand reports whether name is a custom slash command (a
// command file, a bundled command or a skill), which runs as a turn.
func isCustomCommand(app *App, name string) bool {
	for _, c := range app.Workspace.ListCommands() {
		if c.Name == name {
			return true
		}
	}
	return false
}

// isUpdateCommand reports whether line is /skills update or /agents
// update, with or without a focus: a turn, unlike the rest of /skills and
// /agents.
func isUpdateCommand(line string) bool {
	f := strings.Fields(line)
	return len(f) >= 2 && (strings.EqualFold(f[0], "/skills") || strings.EqualFold(f[0], "/agents")) && strings.EqualFold(f[1], "update")
}

func handleSkillsCommand(args []string, app *App) {
	sub := "list"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}

	switch sub {
	case "list":
		all := app.Workspace.ListSkills()
		fmt.Printf("\n%s%s:%s\n", Bold, i18n.T("skills.discovered", "count", len(all)), Reset)
		for _, s := range all {
			tags := ""
			if len(s.Tags) > 0 {
				tags = fmt.Sprintf("[%s]", strings.Join(s.Tags, ", "))
			}
			fmt.Printf("  • %s%s%s %s: %s\n", Bold, safe(s.Name), Reset, Dim+safe(tags)+Reset, safe(s.Description))
			if line := scriptSummary(s); line != "" {
				fmt.Printf("      %s%s%s\n", Dim, safe(line), Reset)
			}
		}
		fmt.Println()

	case "show":
		if len(args) < 2 {
			fmt.Printf("%s%s%s\n", Yellow, i18n.T("skills.show_usage"), Reset)
			return
		}
		s, ok := app.Workspace.Skill(args[1])
		if !ok {
			fmt.Printf("%s✗ %s%s\n", Red, i18n.T("skills.not_found", "name", safe(args[1])), Reset)
			return
		}
		showSkill(s)

	case "search":
		query := ""
		if len(args) > 1 {
			query = strings.Join(args[1:], " ")
		}
		matched := app.Workspace.SearchSkills(query)
		fmt.Printf("\n%s%s:%s\n", Bold, i18n.T("skills.search_results", "query", safe(query), "count", len(matched)), Reset)
		for _, s := range matched {
			fmt.Printf("  • %s%s%s: %s\n", Bold, safe(s.Name), Reset, safe(s.Description))
		}
		searchRegistries(query)
		fmt.Println()

	case "registry", "registries":
		regs, err := skillRegistries().Registries()
		if err != nil {
			fmt.Printf("%s✗ %v%s\n", Red, safe(err.Error()), Reset)
			return
		}
		if len(regs) == 0 {
			fmt.Println(i18n.T("skills.registry_none"))
			return
		}
		fmt.Printf("\n%s%s:%s\n", Bold, i18n.T("skills.registry_title"), Reset)
		for _, r := range regs {
			fmt.Printf("  • %s%s%s %s\n", Bold, safe(r.Name), Reset, Dim+safe(r.URL)+Reset)
		}
		fmt.Println()

	case "install":
		if len(args) < 2 {
			fmt.Printf("%s%s%s\n", Yellow, i18n.T("skills.install_usage"), Reset)
			return
		}
		dir := ""
		if len(args) > 2 && args[2] == "--project" {
			dir = app.Workspace.Dir()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		i, err := skillRegistries().Install(ctx, args[1], dir)
		if err != nil {
			fmt.Printf("%s✗ %s%s\n", Red, safe(err.Error()), Reset)
			return
		}
		fmt.Printf("%s✓ %s%s\n", Green, i18n.T("skills.installed_from", "name", i.Name, "version", i.Version, "registry", i.Registry), Reset)
	}
}

// skillRegistries is the user's registries (a variable for tests).
var skillRegistries = skillreg.Default

// searchRegistries lists what the user's registries offer for query, if
// they added any.
func searchRegistries(query string) {
	store := skillRegistries()
	if regs, _ := store.Registries(); len(regs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	found, err := store.Search(ctx, query)
	fmt.Printf("\n%s%s:%s\n", Bold, i18n.T("skills.registry_hits", "count", len(found)), Reset)
	for _, f := range found {
		fmt.Printf("  • %s%s/%s%s %s: %s\n", Bold, safe(f.Registry), safe(f.Name), Reset, Dim+safe(f.Version)+Reset, safe(f.Description))
	}
	if err != nil {
		fmt.Printf("%s%s%s\n", Yellow, safe(err.Error()), Reset)
	}
	if len(found) > 0 {
		fmt.Printf("%s%s%s\n", Dim, i18n.T("skills.install_hint"), Reset)
	}
}

func handleSessionCommand(args []string, app *App) {
	sub := "list"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}

	switch sub {
	case "list":
		all := len(args) > 1 && (args[1] == "--all" || args[1] == "-a")
		list, err := app.Workspace.ListSessions(all)
		if err != nil {
			fmt.Println(i18n.T("session.list_failed", "error", err))
			return
		}
		title := i18n.T("session.list_title_workspace", "count", len(list))
		if all {
			title = i18n.T("session.list_title_all", "count", len(list))
		}
		fmt.Printf("\n%s%s:%s\n", Bold, title, Reset)
		for _, s := range list {
			snapshot := ""
			if s.Snapshot != "" {
				snapshot = " " + Cyan + "snapshot " + safe(s.Snapshot) + Reset
			}
			fmt.Printf("  • %s%s%s (%s): %s [%s]%s\n", Bold, safe(s.ID), Reset, safe(s.Agent), safe(sessionTitle(s)), i18n.N("session.messages", s.MessageCount), snapshot)
			if all {
				ws := s.Workspace
				if ws == "" {
					ws = i18n.T("session.workspace_unknown")
				}
				fmt.Printf("      %s%s%s\n", Dim, safe(ws), Reset)
			}
		}
		if !all {
			fmt.Printf("  %s%s%s\n", Dim, i18n.T("session.list_all_hint"), Reset)
		}
		fmt.Println()

	case "load", "resume":
		cmdSessionLoad(args[1:], app)

	case "save":
		cmdSessionSave(args[1:], app)

	case "new":
		s, err := app.Workspace.NewSession()
		if err != nil {
			fmt.Printf("%s✗ %s%s\n", Red, i18n.T("session.create_failed", "error", err), Reset)
			return
		}
		fmt.Printf("%s %s%s\n", Green, i18n.T("session.started", "id", s.ID), Reset)
	}
}
