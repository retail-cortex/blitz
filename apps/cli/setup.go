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
	"os"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// globalFlags are shared by the root command and subcommands.
type globalFlags struct {
	config, dir, model, agent, agency string
	trustProject                      bool
	// worktree starts in a new git worktree of that name ("new": one
	// named for the time), from ref (HEAD when "").
	worktree, ref string
	// pluginDirs are plugins loaded for this run (--plugin-dir; local).
	pluginDirs []string
	// addDirs are more read-write roots for this run (--add-dir; local).
	addDirs []string
}

// loadConfig loads trusted configuration and applies flag overrides,
// including --dir as the workspace. The process's working directory is
// left alone: the workspace is always named explicitly.
func loadConfig(f *globalFlags) (*config.Config, error) {
	var dir string
	if f.dir != "" {
		abs, err := filepath.Abs(config.ExpandHome(f.dir))
		if err == nil {
			var info os.FileInfo
			if info, err = os.Stat(abs); err == nil && !info.IsDir() {
				err = fmt.Errorf("%s is not a directory", abs)
			}
		}
		if err != nil {
			return nil, withCode(exitUsage, fmt.Errorf("cannot use --dir: %w", err))
		}
		dir = abs
	}
	// The workspace's own settings (kept in ~/.blitz) over the global ones.
	ws := dir
	if ws == "" {
		ws, _ = os.Getwd()
	}
	cfg, err := config.LoadWorkspace(f.config, ws)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}
	if f.model != "" {
		cfg.Blitz.DefaultModel = f.model
	}
	if f.agent != "" {
		cfg.Blitz.DefaultAgent = f.agent
	}
	if f.agency != "" {
		cfg.Blitz.AgencyLevel = strings.ToLower(f.agency)
	}
	if dir != "" {
		cfg.Tools.WorkspaceDir = dir
	}
	cfg.Plugins.Dirs = append(cfg.Plugins.Dirs, f.pluginDirs...)
	for _, d := range f.addDirs {
		abs, err := filepath.Abs(config.ExpandHome(d))
		if err == nil {
			var info os.FileInfo
			if info, err = os.Stat(abs); err == nil && !info.IsDir() {
				err = fmt.Errorf("%s is not a directory", abs)
			}
		}
		if err != nil {
			return nil, withCode(exitUsage, fmt.Errorf("--add-dir: %w", err))
		}
		// As sandbox.allowed_paths: blocked_paths still apply inside it.
		cfg.Sandbox.AllowedPaths = append(cfg.Sandbox.AllowedPaths, abs)
	}
	return cfg, nil
}

// backendOptions are how openBackend opens the workspace.
type backendOptions struct {
	local, streaming bool
	// trustProject trusts the project settings for this run
	// (--trust-project), in a service of its own.
	trustProject bool
	// appendPrompt is added to the agent's instructions (a run of its own).
	appendPrompt string
	// sessionDir keeps this run's sessions elsewhere
	// (--no-session-persistence: a folder removed at exit).
	sessionDir string
	// flags are the run's flags: --config, and --model, --agent, --agency,
	// --plugin-dir and --add-dir, which make it a run of its own.
	flags *globalFlags
	// askTrust asks about project settings waiting for a decision and
	// returns "trust", "decline" or "" (no answer); nil when nobody can
	// be asked.
	askTrust func(api.ProjectSettings) string
}

// private reports whether the run's settings are its own, so it needs a
// service of its own rather than the shared one.
func (o backendOptions) private() bool {
	g := o.flags
	if g == nil {
		g = &globalFlags{}
	}
	return o.local || o.trustProject || o.appendPrompt != "" || o.sessionDir != "" ||
		g.model != "" || g.agent != "" || g.agency != "" || len(g.pluginDirs) > 0 || len(g.addDirs) > 0
}

// serviceOptions are the private service's settings for this run.
func (o backendOptions) serviceOptions(cfg *config.Config) serviceOptions {
	g := o.flags
	if g == nil {
		g = &globalFlags{}
	}
	so := serviceOptions{Config: g.config, Model: g.model, Agent: g.agent, Agency: g.agency, SessionDir: o.sessionDir, TrustProject: o.trustProject, AppendSystemPrompt: o.appendPrompt}
	for _, d := range g.pluginDirs {
		if abs, err := filepath.Abs(config.ExpandHome(d)); err == nil {
			so.PluginDirs = append(so.PluginDirs, abs)
		}
	}
	for _, d := range g.addDirs {
		if abs, err := filepath.Abs(config.ExpandHome(d)); err == nil {
			so.AddDirs = append(so.AddDirs, abs)
		}
	}
	return so
}

// openBackend attaches to the workspace in a Blitz service: the per-user
// one (started when it isn't running), or one started for this run alone
// when its settings are its own. It also returns the interface's
// translation catalogs, which are always this process's, and whether it
// attached to the shared service. Project settings waiting for trust are
// asked about once attached; the service reopens the workspace with the
// answer.
func openBackend(ctx context.Context, cfg *config.Config, o backendOptions, warn func(string)) (api.Backend, *i18n.Bundle, bool, error) {
	locales := i18n.Setup(config.ExpandHome(cfg.UI.LocalesDir), cfg.UI.Locale, warn)
	private := o.private()
	var sock string
	var err error
	if private {
		if sock, err = startPrivate(ctx, o.serviceOptions(cfg)); err != nil {
			return nil, nil, false, fmt.Errorf("starting a Blitz service for this run: %w", err)
		}
	} else if sock, err = ensureService(ctx); err != nil {
		return nil, nil, false, err
	}
	r, err := client.Attach(ctx, sock, cfg.Tools.WorkspaceDir, warn)
	if private && errors.Is(err, api.ErrWorkspaceBusy) && releaseShared(ctx, cfg.Tools.WorkspaceDir) {
		r, err = client.Attach(ctx, sock, cfg.Tools.WorkspaceDir, warn) // the shared service let it go
	}
	switch {
	case errors.Is(err, api.ErrWorkspaceBusy):
		return nil, nil, false, withCode(exitUsage, fmt.Errorf("%w (another blitz has it open)", err))
	case err != nil:
		return nil, nil, false, fmt.Errorf("attaching to the Blitz service at %s: %w", sock, err)
	}
	if p := r.ProjectSettings(); o.askTrust != nil && !o.trustProject && tui.NeedsTrustDecision(p) {
		if d := o.askTrust(p); d != "" {
			if err := r.TrustProject(p.Hash, d == "trust"); err != nil {
				warn(err.Error())
			}
		}
	}
	return r, locales, !private, nil
}

// releaseShared asks the shared service, when it runs, to close the
// workspace in dir so a run of its own can open it; false when it can't
// (a turn is running there, or it isn't the shared service that has it).
func releaseShared(ctx context.Context, dir string) bool {
	sock := socket.DefaultSocket()
	return socket.Running(sock) && client.Release(ctx, sock, dir) == nil
}

// attachWorkspace attaches to the workspace (--dir, else the current
// directory) in the Blitz service, started when it isn't running.
func attachWorkspace(ctx context.Context, g *globalFlags) (*client.Remote, error) {
	cfg, err := loadConfig(g)
	if err != nil {
		return nil, err
	}
	dir := cfg.Tools.WorkspaceDir
	sock, err := ensureService(ctx)
	if err != nil {
		return nil, err
	}
	return client.Attach(ctx, sock, dir, nil)
}
