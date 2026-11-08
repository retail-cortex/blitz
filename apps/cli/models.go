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
	"strings"
	"text/tabwriter"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/modelfacts"
	"github.com/spf13/cobra"
)

// newModelsCommand is `blitz models [provider…]`: the models each
// configured provider offers, from its API, with the prices Blitz knows
// (spec_027_parity PAR-MOD-03).
func newModelsCommand(g *globalFlags) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
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
			settings := client.AttachSettings(sock)
			list, err := settings.ListChatModels(cmd.Context(), dir, args...)
			if all {
				list, err = settings.ListModels(cmd.Context(), dir, args...)
			}
			if err != nil {
				return err
			}
			if len(args) == 0 && len(list) == 0 {
				return withCode(exitUsage, fmt.Errorf("no provider is set up: give one a key with blitz config set-key <provider>, or sign in with blitz auth login; or name one (gemini, anthropic, openai, ollama)"))
			}
			out := cmd.OutOrStdout()
			failed := 0
			for _, pm := range list {
				fmt.Fprintf(out, "%s\n", pm.Provider)
				if pm.Err != "" {
					failed++
					fmt.Fprintf(out, "%s\n\n", i18n.T("model.cli_list_failed", "error", pm.Err))
					continue
				}
				if pm.Note != "" {
					fmt.Fprintf(out, "  (%s)\n", pm.Note)
				}
				tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
				for _, m := range pm.Models {
					price := "-"
					if m.HasPrice {
						price = i18n.T("model.cli_price", "input", fmt.Sprintf("$%.2f", m.InputPerMTok), "output", fmt.Sprintf("$%.2f", m.OutputPerMTok))
					}
					line := fmt.Sprintf("  %s/%s\t%s", pm.Provider, m.ID, price)
					if about := modelAbout(m); about != "" {
						line += "\t" + about
					}
					fmt.Fprintln(tw, line)
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
	cmd.Flags().BoolVar(&all, "all", false, "Everything the providers list, not only the models that hold a conversation")
	cmd.AddCommand(newModelsForgetCommand())
	return cmd
}

// newModelsForgetCommand is `blitz models forget <model>`: clears what
// Blitz learned about a model from its refusals (spec_056 CA-14), so the
// model is asked again; <model> is provider/model, or a model's name under
// any provider. The service reads the change at its next call.
func newModelsForgetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <model>",
		Short: "Forget what Blitz learned about a model from its refusals",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gone, err := modelfacts.Open(modelfacts.Path(config.Dir())).Forget(args[0])
			if err != nil {
				return err
			}
			if len(gone) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("model.cli_forgot_none", "model", args[0]))
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("model.cli_forgot", "models", strings.Join(gone, ", ")))
			return nil
		},
	}
}

// modelAbout is what the organization says of one of its models (EN-21):
// its name, whether it's the default, and what it can do.
func modelAbout(m client.ListedModel) string {
	var parts []string
	if m.DisplayName != "" && m.DisplayName != m.ID {
		parts = append(parts, m.DisplayName)
	}
	if m.Default {
		parts = append(parts, i18n.T("model.cli_default"))
	}
	if c := m.Caps; c != nil {
		var can []string
		for _, f := range []struct {
			on   bool
			what string
		}{{c.Tools, i18n.T("model.cli_tools")}, {c.Images, i18n.T("model.cli_images")}, {c.Thinking, i18n.T("model.cli_thinking")}} {
			if f.on {
				can = append(can, f.what)
			}
		}
		if c.Context > 0 {
			can = append(can, i18n.T("model.cli_context", "size", c.Context/1000))
		}
		if len(can) > 0 {
			parts = append(parts, strings.Join(can, ", "))
		}
	}
	return strings.Join(parts, " · ")
}
