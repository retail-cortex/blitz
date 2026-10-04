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
	"path/filepath"
	"text/tabwriter"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/spf13/cobra"
)

// newModelsCommand is `blitz models [provider…]`: the models each
// configured provider offers, from its API, with the prices Blitz knows
// (spec_parity_027 PAR-MOD-03).
func newModelsCommand(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "models [provider…]",
		Short: "List the models your providers offer, with the prices Blitz knows",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(g)
			if err != nil {
				return err
			}
			sock, err := ensureService(cmd.Context())
			if err != nil {
				return err
			}
			dir, err := filepath.Abs(config.ExpandHome(cfg.Tools.WorkspaceDir))
			if err != nil {
				return err
			}
			list, err := client.AttachSettings(sock).ListModels(cmd.Context(), dir, args...)
			if err != nil {
				return err
			}
			if len(args) == 0 && len(list) == 0 {
				return withCode(exitUsage, fmt.Errorf("no provider is set up: run blitz setup, or name one (gemini, anthropic, openai, ollama)"))
			}
			out := cmd.OutOrStdout()
			failed := 0
			for _, pm := range list {
				fmt.Fprintf(out, "%s\n", pm.Provider)
				if pm.Err != "" {
					failed++
					fmt.Fprintf(out, "  can't list its models: %v\n\n", pm.Err)
					continue
				}
				if pm.Note != "" {
					fmt.Fprintf(out, "  (%s)\n", pm.Note)
				}
				tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
				for _, m := range pm.Models {
					price := "-"
					if m.HasPrice {
						price = fmt.Sprintf("$%.2f in, $%.2f out per million tokens", m.InputPerMTok, m.OutputPerMTok)
					}
					fmt.Fprintf(tw, "  %s/%s\t%s\n", pm.Provider, m.ID, price)
				}
				tw.Flush()
				fmt.Fprintln(out)
			}
			if failed > 0 {
				return withCode(exitFailure, fmt.Errorf("%d provider(s) couldn't list their models", failed))
			}
			return nil
		},
	}
}
