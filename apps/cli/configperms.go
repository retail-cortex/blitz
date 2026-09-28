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
	"fmt"
	"io"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/spf13/cobra"
)

// permissionsCommand manages the [permissions] rules in the settings files:
// the global ones, or with --workspace the workspace's own, which add to
// them. Rules are checked before they're saved.
func permissionsCommand(g *globalFlags) *cobra.Command {
	var workspace bool
	scope := func() (string, error) {
		if !workspace {
			return "", nil
		}
		cfg, err := loadConfig(g)
		if err != nil {
			return "", err
		}
		return cfg.Tools.WorkspaceDir, nil
	}
	done := func(cmd *cobra.Command, path string) {
		fmt.Fprintf(cmd.OutOrStdout(), "Updated %s. Workspaces open in the service use it when they reopen (the desktop app's settings apply at once).\n", path)
	}

	perms := &cobra.Command{
		Use:   "permissions",
		Short: "List the permission rules, or change them: allow, ask, deny, remove, check, defaults",
		Long: `Permission rules say which actions run without asking (allow), always ask
(ask) or never run (deny); deny wins over ask, ask over allow. They're kept
in the global settings, and a workspace's own (--workspace) add to them.

A shell rule names a command with any arguments: shell(ls) covers "ls -la",
shell(git log) covers "git log --oneline". With * or ? it's a glob over the
whole command (shell(go test *)), and re: starts a regular expression that
must match the whole command (shell(re:git (log|show)( .*)?)). Other kinds:
write(glob), delete(glob), read(glob) (deny only), web(host), search(provider),
mcp(server:tool), skill(name), agent(name), or a tool name.

Built in, and on unless turned off (defaults off): common read-only commands
are allowed. A command that writes a file through a redirection (>, >>)
always asks.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := scope()
			if err != nil {
				return err
			}
			return listPermissionRules(cmd.OutOrStdout(), g.config, dir)
		},
	}
	add := func(effect string) *cobra.Command {
		return &cobra.Command{
			Use:   effect + " <rule>",
			Short: map[string]string{"allow": "Run matching actions without asking", "ask": "Always ask for matching actions", "deny": "Never run matching actions"}[effect],
			Args:  cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				dir, err := scope()
				if err != nil {
					return err
				}
				path, rule, err := config.AddPermissionRule(g.config, dir, effect, strings.Join(args, " "))
				if err != nil {
					return withCode(exitUsage, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", effect, rule)
				done(cmd, path)
				return nil
			},
		}
	}
	remove := &cobra.Command{
		Use:   "remove <rule>",
		Short: "Remove a rule (any effect) from the settings",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := scope()
			if err != nil {
				return err
			}
			rule := strings.Join(args, " ")
			path, n, err := config.RemovePermissionRule(g.config, dir, rule)
			if err != nil {
				return err
			}
			if n == 0 {
				return withCode(exitUsage, fmt.Errorf("no rule %s in %s (see blitz config permissions%s)", rule, path, map[bool]string{true: " --workspace"}[workspace]))
			}
			done(cmd, path)
			return nil
		},
	}
	var effect string
	check := &cobra.Command{
		Use:   "check <rule> [command, path or name]",
		Short: "Check a rule without saving it, and whether it applies to a sample",
		Example: `  blitz config permissions check 'shell(git log)' 'git log --oneline | head'
  blitz config permissions check 'shell(re:go (test|vet)( .*)?)' 'go vet ./...'`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sample := ""
			if len(args) == 2 {
				sample = args[1]
			}
			c, err := tools.CheckPermissionRule(tools.Effect(effect), args[0], sample)
			if err != nil {
				return withCode(exitUsage, err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s %s: %s\n", effect, c.Rule, describeRule(c))
			if c.Tested {
				fmt.Fprintf(out, "  %q: %s\n", sample, map[bool]string{true: "matches", false: "doesn't match"}[c.Matches])
				if c.Redirect != "" && c.Matches && c.Kind == tools.RuleShell && effect == "allow" {
					fmt.Fprintf(out, "  but it writes %s through a redirection, so it still asks\n", c.Redirect)
				}
			}
			return nil
		},
	}
	check.Flags().StringVar(&effect, "effect", "allow", "allow, ask or deny (an allow rule must cover every command in the sample)")
	defaults := &cobra.Command{
		Use:   "defaults <on|off|inherit>",
		Short: "Turn the built-in read-only rules on or off (inherit: a workspace follows the global setting)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := scope()
			if err != nil {
				return err
			}
			var on *bool
			switch args[0] {
			case "on", "off":
				v := args[0] == "on"
				on = &v
			case "inherit":
			default:
				return withCode(exitUsage, fmt.Errorf("%q: on, off or inherit", args[0]))
			}
			path, err := config.SetReadOnlyDefaults(g.config, dir, on)
			if err != nil {
				return err
			}
			done(cmd, path)
			return nil
		},
	}
	subs := []*cobra.Command{add("allow"), add("ask"), add("deny"), remove, defaults}
	for _, c := range append(subs, perms) {
		c.Flags().BoolVarP(&workspace, "workspace", "w", false, "The workspace's own settings (--dir, else the current directory)")
	}
	perms.AddCommand(append(subs, check)...)
	return perms
}

// describeRule says how a checked rule matches.
func describeRule(c tools.RuleCheck) string {
	switch c.Form {
	case tools.FormPrefix:
		return fmt.Sprintf("the command %q with any arguments", c.Pattern)
	case tools.FormGlob:
		return fmt.Sprintf("commands matching %q (* is any text)", c.Pattern)
	case tools.FormRegex:
		return fmt.Sprintf("commands the regular expression %q matches in full", strings.TrimPrefix(c.Pattern, "re:"))
	case "path":
		return fmt.Sprintf("paths matching %q, and what's under them", c.Pattern)
	}
	return fmt.Sprintf("%s %q", c.Kind, c.Pattern)
}

// listPermissionRules prints a scope's rules: a workspace's own and the
// global ones it adds to, and the built-in rules if they're on.
func listPermissionRules(out io.Writer, prefix, workspace string) error {
	own, path, err := config.ScopePermissions(prefix, workspace)
	if err != nil {
		return err
	}
	effective := own
	print := func(title string, p config.PermissionsConfig) {
		fmt.Fprintf(out, "%s:\n", title)
		n := 0
		for _, e := range []struct {
			effect string
			rules  []string
		}{{"deny", p.Deny}, {"ask", p.Ask}, {"allow", p.Allow}} {
			for _, r := range e.rules {
				fmt.Fprintf(out, "  %-5s %s\n", e.effect, r)
				n++
			}
		}
		if n == 0 {
			fmt.Fprintln(out, "  none")
		}
	}
	if workspace != "" {
		global, gpath, err := config.ScopePermissions(prefix, "")
		if err != nil {
			return err
		}
		print("workspace ("+path+")", own)
		print("global ("+gpath+")", global)
		effective = global.Merge(own)
	} else {
		print("global ("+path+")", own)
	}
	if effective.DefaultsOn() {
		fmt.Fprintf(out, "built-in (read_only_defaults on; off: blitz config permissions defaults off):\n  allow %s\n  ask   %s\n", strings.Join(config.ReadOnlyCommands, ", "), strings.Join(config.ReadOnlyGuards, ", "))
	} else {
		fmt.Fprintln(out, "built-in: off (on: blitz config permissions defaults on)")
	}
	return nil
}
