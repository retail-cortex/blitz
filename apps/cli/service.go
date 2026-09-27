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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/socket"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/spf13/cobra"
)

func newServiceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Start the Blitz service at login (install, uninstall, status)",
		Long: `Installs a login item that runs blitzd, the Blitz service, for this user: a launchd
agent on macOS, a systemd user unit on Linux. The service then runs workers
on schedule and the CLI and desktop app attach to it.

A login item doesn't see your shell's environment: keep API keys in
~/.blitz/.env.toml, not only in exported variables.`,
	}
	cmd.AddCommand(
		&cobra.Command{Use: "install", Short: "Start the service now and at every login", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return serviceInstall(cmd.OutOrStdout()) }},
		&cobra.Command{Use: "uninstall", Short: "Stop the service and remove the login item", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return serviceUninstall(cmd.OutOrStdout()) }},
		&cobra.Command{Use: "status", Short: "Say whether the login item is installed and the service answering", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return serviceStatus(cmd.OutOrStdout()) }},
	)
	return cmd
}

func serviceInstall(out io.Writer) error {
	path, err := loginitem.Path()
	if err != nil {
		return withCode(exitUsage, err)
	}
	bin, err := loginitem.FindService(loginitem.Beside())
	if err != nil {
		return withCode(exitUsage, err)
	}
	if err := loginitem.Install(bin); err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ The Blitz service starts at login (%s).\n", path)
	fmt.Fprintf(out, "   It runs %s; after upgrading Blitz, run 'blitz service install' again.\n", bin)
	if keys := keysOnlyInEnvironment(); len(keys) > 0 {
		fmt.Fprintf(out, "!  %s %s only in your shell's environment, which the service won't see: put %s in %s.\n",
			strings.Join(keys, ", "), plural(len(keys), "is", "are"), plural(len(keys), "it", "them"), filepath.Join(config.ConfigDir(""), ".env.toml"))
	}
	return nil
}

func serviceUninstall(out io.Writer) error {
	if err := loginitem.Uninstall(); err != nil {
		if errors.Is(err, loginitem.ErrUnsupported) {
			return withCode(exitUsage, err)
		}
		return err
	}
	fmt.Fprintln(out, "The Blitz service no longer starts at login.")
	return nil
}

func serviceStatus(out io.Writer) error {
	path, err := loginitem.Path()
	if err != nil {
		return withCode(exitUsage, err)
	}
	installed := "not installed"
	if loginitem.Installed() {
		installed = "installed (" + path + ")"
	}
	running := "not running"
	if sock := socket.DefaultSocket(); socket.Running(sock) {
		running = "answering on " + sock
	}
	fmt.Fprintf(out, "login item: %s\nservice:    %s\n", installed, running)
	return nil
}

// keysOnlyInEnvironment names the API key variables set in the environment
// whose keys the config file doesn't also hold: a login item won't see them.
func keysOnlyInEnvironment() []string {
	vars := []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"}
	set := map[string]string{}
	for _, v := range vars {
		if val := os.Getenv(v); val != "" {
			set[v] = val
		}
	}
	if len(set) == 0 {
		return nil
	}
	// Load the configuration as the service would: without them.
	for v := range set {
		os.Unsetenv(v)
	}
	cfg, err := config.Load("")
	for v, val := range set {
		os.Setenv(v, val)
	}
	if err != nil {
		return nil
	}
	var out []string
	for _, v := range vars {
		if _, ok := set[v]; !ok {
			continue
		}
		have := ""
		switch v {
		case "GEMINI_API_KEY", "GOOGLE_API_KEY":
			have = cfg.LLM.Gemini.APIKey
		case "OPENAI_API_KEY":
			have = cfg.LLM.OpenAI.APIKey
		case "ANTHROPIC_API_KEY":
			have = cfg.LLM.Anthropic.APIKey
		}
		if have == "" {
			out = append(out, v)
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// serviceBinary finds blitzd: beside this blitz (as released and bundled),
// else on PATH.
func serviceBinary() (string, error) { return loginitem.FindService(loginitem.Beside()) }

// newServeCommand keeps "blitz serve" working, for login items installed
// before the service became its own program: it runs blitzd.
func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "serve",
		Short:              "Run the Blitz service (blitzd)",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			bin, err := serviceBinary()
			if err != nil {
				return withCode(exitUsage, err)
			}
			c := exec.CommandContext(cmd.Context(), bin, args...)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := c.Run(); err != nil {
				var exit *exec.ExitError
				if errors.As(err, &exit) {
					return withCode(exit.ExitCode(), fmt.Errorf("blitzd: %w", err))
				}
				return err
			}
			return nil
		},
	}
}
