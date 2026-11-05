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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/spf13/cobra"
)

func newServiceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Start the Blitz service at login (install, uninstall, restart, status)",
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
		&cobra.Command{Use: "restart", Short: "Restart the service, so it reads changed settings, servers and plugins", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return serviceRestart(cmd.Context(), cmd.OutOrStdout()) }},
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
	fmt.Fprintln(out, i18n.T("service.installed", "path", path))
	fmt.Fprintln(out, i18n.T("service.installed_runs", "program", bin))
	if keys := keysOnlyInEnvironment(); len(keys) > 0 {
		fmt.Fprintln(out, i18n.N("service.keys_in_env", len(keys), "keys", strings.Join(keys, ", "), "file", filepath.Join(config.ConfigDir(""), ".env.toml")))
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
	fmt.Fprintln(out, i18n.T("service.uninstalled"))
	return nil
}

// serviceRestart restarts the per-user service (N26 cli-2#2): through its
// login item when it has one; else the running one is asked to stop and
// started again as blitz starts it on demand. A service that isn't
// running has nothing to restart: the next one reads the change.
func serviceRestart(ctx context.Context, out io.Writer) error {
	sock := socket.DefaultSocket()
	if loginitem.Installed() {
		if err := loginitem.Restart(); err != nil {
			return err
		}
		if err := waitService(ctx, sock, nil); err != nil {
			return err
		}
		fmt.Fprintln(out, i18n.T("service.restarted"))
		return nil
	}
	if !socket.Running(sock) {
		fmt.Fprintln(out, i18n.T("service.not_running"))
		return nil
	}
	if err := shutdownService(ctx, sock); err != nil {
		return fmt.Errorf("stopping the Blitz service: %w", err)
	}
	if err := waitStopped(ctx, sock); err != nil {
		return err
	}
	if _, err := ensureService(ctx); err != nil {
		return err
	}
	fmt.Fprintln(out, i18n.T("service.restarted"))
	return nil
}

// shutdownService asks the service at sock to stop
// (WorkspaceService.Shutdown).
func shutdownService(ctx context.Context, sock string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c := pb.NewWorkspaceServiceClient(socket.Client(sock), socket.BaseURL)
	_, err := c.Shutdown(ctx, connect.NewRequest(&pb.ShutdownRequest{}))
	return err
}

// waitStopped waits for the service on sock to stop answering, for up to
// startWait (its turns get a few seconds to end).
func waitStopped(ctx context.Context, sock string) error {
	deadline := time.Now().Add(startWait)
	for socket.Running(sock) {
		if time.Now().After(deadline) {
			return fmt.Errorf("the Blitz service on %s didn't stop within %s", sock, startWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil
}

func serviceStatus(out io.Writer) error {
	path, err := loginitem.Path()
	if err != nil {
		return withCode(exitUsage, err)
	}
	installed := i18n.T("service.item_missing")
	if loginitem.Installed() {
		installed = i18n.T("service.item_installed", "path", path)
	}
	running := i18n.T("service.stopped")
	if sock := socket.DefaultSocket(); socket.Running(sock) {
		running = i18n.T("service.answering", "socket", sock)
	}
	fmt.Fprintln(out, i18n.T("service.status_item", "state", installed))
	fmt.Fprintln(out, i18n.T("service.status_service", "state", running))
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

// serviceBinary finds blitzd: beside this blitz (as released and bundled),
// in its Bazel runfiles (bazel run), else on PATH.
func serviceBinary() (string, error) { return loginitem.FindService(serviceDirs()...) }

// serviceDirs are where serviceBinary looks before PATH; tests narrow it.
var serviceDirs = loginitem.ServiceDirs

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
