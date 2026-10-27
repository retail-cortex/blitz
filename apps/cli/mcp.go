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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/mcpauth"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/spf13/cobra"
)

// newMCPCommand is `blitz mcp`: the MCP servers of the global settings,
// added, listed, shown, removed, enabled and disabled in place, and
// signed in to (spec_parity_027 PAR-MCP-01, -04). With --workspace, the
// workspace's own instead (--dir's, else the current directory's: its
// settings in ~/.blitz, never the project), as Studio › Connectors edits
// them (spec_mcp_009 MCP-50): list shows every server it runs with its
// scope, disable and enable turn a global one off or on there alone, and
// make-global moves one of its own to every workspace.
func newMCPCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "mcp", Short: "Add, list and manage MCP servers"}
	var inWorkspace bool
	cmd.PersistentFlags().BoolVarP(&inWorkspace, "workspace", "w", false, "This workspace's own servers (--dir, else the current directory), not the global ones")
	ws := func() (string, error) {
		if !inWorkspace {
			return "", nil
		}
		return mcpWorkspace(g)
	}

	var env, headers, agents []string
	var prefix string
	add := &cobra.Command{
		Use:   "add <name> <command> [args…] | add <name> <url>",
		Short: "Add an MCP server: a command to run (stdio), or an http(s) URL",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s := config.MCPServerConfig{Name: args[0], Prefix: prefix, Agents: agents}
			if len(args) == 2 && regexp.MustCompile(`^https?://`).MatchString(args[1]) {
				s.URL = args[1]
			} else {
				s.Command, s.Args = args[1], args[2:]
			}
			var err error
			if s.Env, err = pairs(env, "="); err != nil {
				return withCode(exitUsage, fmt.Errorf("--env: %w", err))
			}
			if s.Headers, err = pairs(headers, ":"); err != nil {
				return withCode(exitUsage, fmt.Errorf("--header: %w", err))
			}
			if s.URL == "" && len(s.Headers) > 0 {
				return withCode(exitUsage, errors.New("--header is for http servers"))
			}
			dir, err := ws()
			if err != nil {
				return err
			}
			path, err := config.AddMCPServer(g.config, dir, s)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added MCP server %q to %s.\n", s.Name, path)
			restartHint(cmd.OutOrStdout())
			return nil
		},
	}
	add.Flags().StringArrayVar(&env, "env", nil, "An environment variable for the server, K=V (repeatable)")
	add.Flags().StringArrayVar(&headers, "header", nil, "A header for an http server, K:V (repeatable)")
	add.Flags().StringVar(&prefix, "prefix", "", "Name the server's tools prefix__tool")
	add.Flags().StringSliceVar(&agents, "agents", nil, "The agents offered its tools (default: the main agent; * for all)")

	list := &cobra.Command{
		Use:   "list",
		Short: "List the MCP servers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := ws()
			if err != nil {
				return err
			}
			list, err := mcpServers(g, dir)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(out, "No MCP servers. Add one with: blitz mcp add <name> <command|url>")
				return nil
			}
			for _, s := range list {
				state := ""
				switch {
				case s.OffHere:
					state = " (off here)"
				case s.Disabled:
					state = " (disabled)"
				default:
					if _, err := mcpauth.Load(secrets.Default(config.ConfigDir(g.config)), s.Name); err == nil {
						state = " (signed in)"
					}
				}
				if dir == "" {
					fmt.Fprintf(out, "%s%s\t%s\n", s.Name, state, target(s))
				} else {
					fmt.Fprintf(out, "%s%s\t%s\t%s\n", s.Name, state, target(s), s.Scope)
				}
			}
			return nil
		},
	}

	get := &cobra.Command{
		Use:   "get <name>",
		Short: "Show an MCP server's settings (environment values masked)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := ws()
			if err != nil {
				return err
			}
			s, err := findMCPServer(g, dir, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "name:     %s\n", s.Name)
			if dir != "" {
				fmt.Fprintf(out, "scope:    %s\n", s.Scope)
			}
			if s.Command != "" {
				fmt.Fprintf(out, "command:  %s\n", target(s))
			} else {
				fmt.Fprintf(out, "url:      %s\n", s.URL)
			}
			fmt.Fprintf(out, "enabled:  %v\n", !s.Disabled)
			if s.OffHere {
				fmt.Fprintln(out, "          (off in this workspace; on elsewhere)")
			}
			for _, k := range sortedKeys(s.Env) {
				fmt.Fprintf(out, "env:      %s=%s\n", k, mask(s.Env[k]))
			}
			for _, k := range sortedKeys(s.Headers) {
				fmt.Fprintf(out, "header:   %s: %s\n", k, mask(s.Headers[k]))
			}
			if s.Prefix != "" {
				fmt.Fprintf(out, "prefix:   %s\n", s.Prefix)
			}
			if len(s.Agents) > 0 {
				fmt.Fprintf(out, "agents:   %s\n", strings.Join(s.Agents, ", "))
			}
			return nil
		},
	}

	edit := func(use, short, done string, f func(dir, name string) (string, error)) *cobra.Command {
		return &cobra.Command{
			Use:   use + " <name>",
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				dir, err := ws()
				if err != nil {
					return err
				}
				path, err := f(dir, args[0])
				if errors.Is(err, config.ErrNoMCPServer) {
					return withCode(exitUsage, err)
				}
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s MCP server %q in %s.\n", done, args[0], path)
				restartHint(cmd.OutOrStdout())
				return nil
			},
		}
	}
	remove := edit("remove", "Remove an MCP server", "Removed", func(dir, n string) (string, error) { return config.RemoveMCPServer(g.config, dir, n) })
	enable := edit("enable", "Start an MCP server again (with --workspace, a global one there)", "Enabled", func(dir, n string) (string, error) { return config.SetMCPServerEnabled(g.config, dir, n, true) })
	disable := edit("disable", "Keep an MCP server configured without starting it (with --workspace, a global one off there alone)", "Disabled", func(dir, n string) (string, error) {
		return config.SetMCPServerEnabled(g.config, dir, n, false)
	})
	makeGlobal := &cobra.Command{
		Use:   "make-global <name>",
		Short: "Move one of this workspace's servers to the global settings, for every workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := mcpWorkspace(g) // always a workspace's
			if err != nil {
				return err
			}
			path, err := config.MakeMCPServerGlobal(g.config, dir, args[0])
			if errors.Is(err, config.ErrNoMCPServer) {
				return withCode(exitUsage, err)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Moved MCP server %q to %s: every workspace has it now.\n", args[0], path)
			restartHint(cmd.OutOrStdout())
			return nil
		},
	}

	login := &cobra.Command{
		Use:   "login <name>",
		Short: "Sign in to an MCP server over HTTP (OAuth), in a browser",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := ws()
			if err != nil {
				return err
			}
			s, err := findMCPServer(g, dir, args[0])
			if err != nil {
				return err
			}
			if s.URL == "" {
				return withCode(exitUsage, fmt.Errorf("%s runs a command: only http servers sign in", s.Name))
			}
			out := cmd.OutOrStdout()
			store := secrets.Default(config.ConfigDir(g.config))
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
			defer cancel()
			// Pasted lines, for a browser on another machine (SSH). The
			// input is taken now, and a line read after the sign-in ends
			// is dropped, not left blocking.
			pasted := make(chan string)
			in := cmd.InOrStdin()
			go func() {
				defer close(pasted)
				sc := bufio.NewScanner(in)
				for sc.Scan() {
					select {
					case pasted <- sc.Text():
					case <-ctx.Done():
						return
					}
				}
			}()
			show := func(u string) {
				fmt.Fprintf(out, "Open this address to sign in to %s:\n\n  %s\n\nWaiting for the browser. If it's on another machine, paste the address it ends on here.\n", s.Name, u)
				openBrowser(u)
			}
			if err := mcpauth.Login(ctx, store, s.Name, s.URL, show, pasted); err != nil {
				return fmt.Errorf("signing in to %s: %w", s.Name, err)
			}
			fmt.Fprintf(out, "Signed in to %s; the sign-in is kept in the %s.\n", s.Name, store.Kind())
			restartHint(out)
			return nil
		},
	}
	logout := &cobra.Command{
		Use:   "logout <name>",
		Short: "Forget the sign-in to an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mcpauth.Forget(secrets.Default(config.ConfigDir(g.config)), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Signed out of %s.\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(add, list, get, remove, enable, disable, makeGlobal, login, logout)
	return cmd
}

// openBrowser opens u in the system's browser, if there is one.
func openBrowser(u string) {
	name := "xdg-open"
	if goruntime.GOOS == "darwin" {
		name = "open"
	}
	_ = exec.Command(name, u).Start() // no browser: the address is printed
}

// mcpWorkspace is the workspace --workspace means: --dir's, else the
// current directory, canonical.
func mcpWorkspace(g *globalFlags) (string, error) {
	dir := g.dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	return config.CanonicalWorkspace(config.ExpandHome(dir))
}

// mcpServers are the global settings' servers (workspace ""), or every
// server a workspace runs, with its scope: the global ones (those it
// turned off marked so), its own, then its project's.
func mcpServers(g *globalFlags, workspace string) ([]config.MCPServerConfig, error) {
	if workspace == "" {
		cfg, err := config.Load(g.config)
		if err != nil {
			return nil, err
		}
		return cfg.MCP.Servers, nil
	}
	cfg, err := config.LoadWorkspace(g.config, workspace)
	if err != nil {
		return nil, err
	}
	var out []config.MCPServerConfig
	for _, s := range cfg.MCP.Servers {
		if s.Scope == config.MCPScopeGlobal || s.Scope == config.MCPScopeWorkspace {
			out = append(out, s)
		}
	}
	if cfg.Project != nil {
		for _, it := range cfg.Project.Pending {
			if it.Kind == config.ProjectMCP {
				s := config.MCPServerConfig{Name: it.Key, Command: it.Value, Scope: config.MCPScopeProject}
				if strings.HasPrefix(it.Value, "http://") || strings.HasPrefix(it.Value, "https://") {
					s.Command, s.URL = "", it.Value
				}
				out = append(out, s)
			}
		}
	}
	return out, nil
}

func findMCPServer(g *globalFlags, workspace, name string) (config.MCPServerConfig, error) {
	list, err := mcpServers(g, workspace)
	if err != nil {
		return config.MCPServerConfig{}, err
	}
	for _, s := range list {
		if s.Name == name {
			return s, nil
		}
	}
	return config.MCPServerConfig{}, withCode(exitUsage, fmt.Errorf("%w: %s", config.ErrNoMCPServer, name))
}

// target is what a server runs or reaches.
func target(s config.MCPServerConfig) string {
	if s.URL != "" {
		return s.URL
	}
	return strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
}

// pairs reads K<sep>V flags.
func pairs(list []string, sep string) (map[string]string, error) {
	if len(list) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, p := range list {
		k, v, ok := strings.Cut(p, sep)
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("%q isn't K%sV", p, sep)
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, nil
}

// mask hides most of a value that may be a secret.
func mask(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return v[:2] + strings.Repeat("*", 4) + v[len(v)-2:]
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// restartHint says that a running service reads the change when its
// workspaces open again.
func restartHint(out io.Writer) {
	if socket.Running(socket.DefaultSocket()) {
		fmt.Fprintln(out, "The Blitz service picks it up when it restarts: blitz service restart (or the desktop app's Settings › Service).")
	}
}
