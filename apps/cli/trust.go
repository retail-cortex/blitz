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
	"fmt"
	"os"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/spf13/cobra"
)

// newTrustCommand is `blitz trust [dir] [--revoke]`: a workspace's
// project settings (.blitz/settings.toml), and trusting or declining the
// part that runs code or loosens a policy (spec_project_config_031).
func newTrustCommand(g *globalFlags) *cobra.Command {
	var revoke bool
	cmd := &cobra.Command{
		Use:   "trust [dir]",
		Short: "Show a workspace's project settings, and trust or decline them",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				g.dir = args[0]
			}
			cfg, err := loadConfig(g)
			if err != nil {
				return err
			}
			if cfg.Tools.WorkspaceDir == "" {
				cfg.Tools.WorkspaceDir, _ = os.Getwd()
			}
			out := cmd.OutOrStdout()
			if revoke {
				if err := engine.ForgetProjectTrust(cfg); err != nil {
					return err
				}
				fmt.Fprintln(out, i18n.T("project.revoked"))
				return nil
			}
			t, err := projectTruster(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			p := t.ProjectSettings()
			tui.ShowProject(out, p)
			if len(p.Pending) > 0 && p.Hash != "" && tui.StdinIsTerminal() {
				tui.DecideTrust(cmd.Context(), tui.NewLineReader(os.Stdin, out), out, t, p)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&revoke, "revoke", false, "Forget the decision, so it's asked again")
	return cmd
}

// projectTruster is the running service's workspace, which reopens it
// with a decision, or else the settings on disk.
func projectTruster(ctx context.Context, cfg *config.Config) (tui.ProjectTruster, error) {
	if sock := socket.DefaultSocket(); socket.Running(sock) {
		return client.Attach(ctx, sock, cfg.Tools.WorkspaceDir, nil)
	}
	return localProject{cfg}, nil
}

// localProject reads and trusts project settings on disk, without opening
// the workspace.
type localProject struct{ cfg *config.Config }

func (l localProject) Dir() string { return l.cfg.Tools.WorkspaceDir }

func (l localProject) ProjectSettings() api.ProjectSettings {
	p, err := engine.ReviewProject(l.cfg)
	if err != nil {
		return api.ProjectSettings{State: api.TrustNone, Problems: []string{err.Error()}}
	}
	return p
}

func (l localProject) TrustProject(hash string, trusted bool) error {
	return engine.TrustProject(l.cfg, hash, trusted)
}
