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
	"strings"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/spf13/cobra"
)

// newTrustCommand is `blitz trust [dir] [--revoke]`: a workspace's
// project settings (.blitz/settings.toml), and trusting or declining the
// part that runs code or loosens a policy (spec_031_project_config).
func newTrustCommand(g *globalFlags) *cobra.Command {
	var revoke, code bool
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
			t, err := projectTruster(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("code") {
				// The language servers' use of the folder's own code
				// (spec_037 VE-43a), apart from the project settings.
				if err := t.TrustRepoCode(code); err != nil {
					return err
				}
				fmt.Fprintln(out, i18n.T(map[bool]string{true: "desktop.lsp.code_trusted", false: "desktop.lsp.code_declined"}[code]))
				return nil
			}
			if revoke {
				if err := t.ForgetProjectTrust(); err != nil {
					return err
				}
				fmt.Fprintln(out, i18n.T("project.revoked"))
				return nil
			}
			p := t.ProjectSettings()
			tui.ShowProject(out, p)
			if state, files, err := t.RepoCodeTrust(); err == nil && state != "" {
				key := map[string]string{"ask": "desktop.lsp.code_ask", "trusted": "desktop.lsp.code_state_trusted", "declined": "desktop.lsp.code_state_declined"}[state]
				fmt.Fprintln(out, i18n.T(key, "files", strings.Join(files, ", ")))
				if state != "trusted" {
					fmt.Fprintln(out, i18n.T("cli.trust.code_hint"))
				}
			}
			if len(p.Pending) > 0 && p.Hash != "" && tui.StdinIsTerminal() {
				tui.DecideTrust(cmd.Context(), tui.NewLineReader(os.Stdin, out), out, t, p)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&revoke, "revoke", false, "Forget the decision, so it's asked again")
	cmd.Flags().BoolVar(&code, "code", false, "Let the language servers run the folder's own code (its Bazel driver, its .venv's Python); --code=false: they work without it")
	return cmd
}

// projectTruster is the workspace in the Blitz service (started when it
// isn't running), which reopens it with a decision.
func projectTruster(ctx context.Context, cfg *config.Config) (*client.Remote, error) {
	sock, err := ensureService(ctx)
	if err != nil {
		return nil, err
	}
	return client.Attach(ctx, sock, cfg.Tools.WorkspaceDir, nil)
}
