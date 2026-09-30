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
// signed in to (spec_parity_027 PAR-MCP-01, -04).
func newMCPCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "mcp", Short: "Add, list and manage MCP servers"}

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
			path, err := config.AddMCPServer(g.config, s)
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
			cfg, err := config.Load(g.config)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(cfg.MCP.Servers) == 0 {
				fmt.Fprintln(out, "No MCP servers. Add one with: blitz mcp add <name> <command|url>")
				return nil
			}
			for _, s := range cfg.MCP.Servers {
				state := ""
				if s.Disabled {
					state = " (disabled)"
				} else if _, err := mcpauth.Load(secrets.Default(config.ConfigDir(g.config)), s.Name); err == nil {
					state = " (signed in)"
				}
				fmt.Fprintf(out, "%s%s\t%s\n", s.Name, state, target(s))
			}
			return nil
		},
	}

	get := &cobra.Command{
		Use:   "get <name>",
		Short: "Show an MCP server's settings (environment values masked)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := findMCPServer(g, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "name:     %s\n", s.Name)
			if s.Command != "" {
				fmt.Fprintf(out, "command:  %s\n", target(s))
			} else {
				fmt.Fprintf(out, "url:      %s\n", s.URL)
			}
			fmt.Fprintf(out, "enabled:  %v\n", !s.Disabled)
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

	edit := func(use, short, done string, f func(name string) (string, error)) *cobra.Command {
		return &cobra.Command{
			Use:   use + " <name>",
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				path, err := f(args[0])
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
	remove := edit("remove", "Remove an MCP server", "Removed", func(n string) (string, error) { return config.RemoveMCPServer(g.config, n) })
	enable := edit("enable", "Start an MCP server again", "Enabled", func(n string) (string, error) { return config.SetMCPServerDisabled(g.config, n, false) })
	disable := edit("disable", "Keep an MCP server configured without starting it", "Disabled", func(n string) (string, error) {
		return config.SetMCPServerDisabled(g.config, n, true)
	})

	login := &cobra.Command{
		Use:   "login <name>",
		Short: "Sign in to an MCP server over HTTP (OAuth), in a browser",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := findMCPServer(g, args[0])
			if err != nil {
				return err
			}
			if s.URL == "" {
				return withCode(exitUsage, fmt.Errorf("%s runs a command: only http servers sign in", s.Name))
			}
			out := cmd.OutOrStdout()
			store := secrets.Default(config.ConfigDir(g.config))
			// Pasted lines, for a browser on another machine (SSH).
			pasted := make(chan string)
			go func() {
				sc := bufio.NewScanner(cmd.InOrStdin())
				for sc.Scan() {
					pasted <- sc.Text()
				}
				close(pasted)
			}()
			show := func(u string) {
				fmt.Fprintf(out, "Open this address to sign in to %s:\n\n  %s\n\nWaiting for the browser. If it's on another machine, paste the address it ends on here.\n", s.Name, u)
				openBrowser(u)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
			defer cancel()
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

	cmd.AddCommand(add, list, get, remove, enable, disable, login, logout)
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

func findMCPServer(g *globalFlags, name string) (config.MCPServerConfig, error) {
	cfg, err := config.Load(g.config)
	if err != nil {
		return config.MCPServerConfig{}, err
	}
	for _, s := range cfg.MCP.Servers {
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
