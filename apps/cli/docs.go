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
	"strings"

	"github.com/spf13/cobra"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/userdocs"
)

// newDocsCommand is blitz docs: the docs of this release, offline
// (spec_067_cli_parity CLP-21).
func newDocsCommand(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "docs [page | words…]",
		Short: "Read the docs of this release: a page by name, or the sections that match your words",
		Long: "Blitz carries its docs, so they work offline and match the version you run. With nothing, the pages by section; " +
			"with a page's name (mcp, guide/models), the page; with other words, the sections that match them.",
		Example: "  blitz docs\n  blitz docs mcp\n  blitz docs add a database connection",
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			var names []string
			for _, p := range userdocs.Docs().Pages {
				if !strings.HasSuffix(p.Path, "_index") {
					names = append(names, p.Path)
				}
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			pretty := stdoutIsTerminal()
			theme := "auto"
			if cfg, err := config.Load(g.config); err == nil {
				theme = cfg.UI.Theme
			}
			tui.ShowDocs(cmd.OutOrStdout(), "blitz docs", strings.Join(args, " "), func(md string) string {
				return tui.RenderMarkdown(md, themeFor(theme), terminalWidth(), pretty)
			})
			return nil
		},
	}
}
