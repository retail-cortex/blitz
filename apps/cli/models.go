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
	"text/tabwriter"

	"github.com/retail-cortex/blitz/pkg/engine/runtime"
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
			if len(args) == 0 && len(runtime.ConfiguredProviders(cfg)) == 0 {
				return withCode(exitUsage, fmt.Errorf("no provider is set up: run blitz setup, or name one (gemini, anthropic, openai, ollama)"))
			}
			out := cmd.OutOrStdout()
			failed := 0
			for _, pm := range runtime.ListModels(cmd.Context(), cfg, args...) {
				fmt.Fprintf(out, "%s\n", pm.Provider)
				if pm.Err != nil {
					failed++
					fmt.Fprintf(out, "  can't list its models: %v\n\n", pm.Err)
					continue
				}
				tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
				for _, m := range pm.Models {
					price := "-"
					if m.Price != nil {
						price = fmt.Sprintf("$%.2f in, $%.2f out per million tokens", m.Price.InputPerMTok, m.Price.OutputPerMTok)
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
