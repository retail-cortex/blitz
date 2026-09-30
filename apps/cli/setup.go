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
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/socket"
)

// globalFlags are shared by the root command and subcommands.
type globalFlags struct {
	config, dir, model, agent, agency string
	trustProject                      bool
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
	return cfg, nil
}

// backendOptions are how openBackend opens the workspace.
type backendOptions struct {
	local, streaming bool
	// trustProject trusts the project settings for this run
	// (--trust-project), in a workspace opened here.
	trustProject bool
	// askTrust asks about project settings waiting for a decision and
	// returns "trust", "decline" or "" (no answer); nil when nobody can
	// be asked.
	askTrust func(api.ProjectSettings) string
}

// openBackend attaches to the workspace in the Blitz service when one
// is running (unless local), and otherwise opens it in this process. It
// also returns the interface's translation catalogs, which are always this
// process's, and whether it attached. Project settings waiting for trust
// are asked about first: before the workspace opens here, or in the
// service, which reopens it with the answer.
func openBackend(ctx context.Context, cfg *config.Config, o backendOptions, warn func(string)) (api.Backend, *i18n.Bundle, bool, error) {
	sock := socket.DefaultSocket()
	if !o.local && socket.Running(sock) {
		r, err := client.Attach(ctx, sock, cfg.Tools.WorkspaceDir, warn)
		if err != nil {
			return nil, nil, false, fmt.Errorf("attaching to the Blitz service at %s: %w (--local runs without it)", sock, err)
		}
		locales := engine.SetupLocale(cfg, warn)
		if o.trustProject {
			warn(i18n.T("project.trust_run_attached"))
		}
		if p := r.ProjectSettings(); o.askTrust != nil && tui.NeedsTrustDecision(p) {
			if d := o.askTrust(p); d != "" {
				if err := r.TrustProject(p.Hash, d == "trust"); err != nil {
					warn(err.Error())
				}
			}
		}
		return r, locales, true, nil
	}
	if o.askTrust != nil && !o.trustProject {
		if p, err := engine.ReviewProject(cfg); err == nil && tui.NeedsTrustDecision(p) {
			if d := o.askTrust(p); d != "" {
				if err := engine.TrustProject(cfg, p.Hash, d == "trust"); err != nil {
					warn(err.Error())
				}
			}
		}
	}
	w, err := engine.Open(ctx, cfg, engine.Options{Streaming: o.streaming, Warn: warn, TrustProject: o.trustProject})
	if errors.Is(err, api.ErrWorkspaceBusy) {
		return nil, nil, false, withCode(exitUsage, fmt.Errorf("%w (another blitz has it open)", err))
	}
	if err != nil {
		return nil, nil, false, err
	}
	return w, w.Locales(), false, nil
}
