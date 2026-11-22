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
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
)

// checkKey asks the service whether provider takes key (a variable for
// tests: they never call a provider).
var checkKey = func(ctx context.Context, provider, key string) (client.KeyCheck, error) {
	sock, err := ensureService(ctx)
	if err != nil {
		return client.KeyCheck{}, err
	}
	return client.AttachSettings(sock).CheckAPIKey(ctx, "", provider, key)
}

// listProvider lists provider's models with the saved settings: whether
// they work (a variable for tests).
var listProvider = func(ctx context.Context, dir, provider string) (client.ProviderModels, error) {
	sock, err := ensureService(ctx)
	if err != nil {
		return client.ProviderModels{}, err
	}
	list, err := client.AttachSettings(sock).ListModels(ctx, dir, provider)
	if err != nil || len(list) == 0 {
		return client.ProviderModels{}, cmp.Or(err, fmt.Errorf("no listing for %s", provider))
	}
	return list[0], nil
}

// newProvidersCommand is blitz providers: the model providers in one place
// (spec_067_cli_parity CLP-41), list, add (its key checked before it's
// saved), check, use and remove, as Settings › Providers does.
func newProvidersCommand(g *globalFlags) *cobra.Command {
	var workspace, asJSON, noCheck, yes, keepDefault bool
	var model, auth, project, location, profile string
	scope := func() (string, error) {
		if !workspace {
			return "", nil
		}
		return workspaceDir(g)
	}
	named := func(args []string) (string, error) {
		p := strings.ToLower(args[0])
		if !config.ValidProvider(p) {
			return "", withCode(exitUsage, fmt.Errorf("unknown provider %q (%s)", args[0], config.ProviderList()))
		}
		return p, nil
	}
	providerArg := func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return config.Providers, cobra.ShellCompDirectiveNoFileComp
	}
	cmd := &cobra.Command{Use: "providers", Aliases: []string{"provider"}, Short: "The model providers: list, add (the key checked first), check, use and remove"}

	list := &cobra.Command{
		Use:   "list",
		Short: "List the providers set up, how each signs in (never the key) and which one is in use",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := scope()
			if err != nil {
				return err
			}
			info, err := config.Describe(g.config, dir)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), info)
			}
			out := cmd.OutOrStdout()
			for _, p := range info.Providers {
				mark := "  "
				if p.Name == info.Provider {
					mark = "* "
				}
				how := string(p.KeySource)
				if p.Auth != "" {
					how = p.Auth
				}
				if p.KeyMissing {
					how += i18n.T("config.key_missing")
				}
				fmt.Fprintf(out, "%s%-17s %-12s %s\n", mark, p.Name, how, p.Model)
			}
			fmt.Fprintln(out, i18n.T("providers.legend", "path", info.Path))
			return nil
		},
	}
	list.Flags().BoolVar(&asJSON, "json", false, "Print JSON")

	add := &cobra.Command{
		Use:   "add <provider>",
		Short: "Set a provider up: its key (asked, hidden, and checked before it's saved) or how it signs in, and make it the one in use",
		Example: "  blitz providers add gemini\n  blitz providers add anthropic --model claude-sonnet-4-6 --keep-default\n" +
			"  blitz providers add gemini --auth adc --project my-project --location us-central1",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: providerArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := named(args)
			if err != nil {
				return err
			}
			dir, err := scope()
			if err != nil {
				return err
			}
			choice := config.ProviderChoice{Provider: p, Model: model, KeepDefault: keepDefault}
			if auth != "" && auth != config.AuthAPIKey {
				choice.Auth = &config.ProviderAuth{Method: auth, ProjectID: project, Location: location, Profile: profile}
			} else if slices.Contains(config.KeyedProviders, p) {
				key, err := readSecret(cmd, i18n.T("config.key_prompt", "provider", p))
				if err != nil {
					return err
				}
				if strings.TrimSpace(key) == "" {
					return withCode(exitUsage, errors.New("no key was typed: nothing was saved"))
				}
				if !noCheck {
					c, err := checkKey(cmd.Context(), p, key)
					if err != nil {
						return err
					}
					if !c.Works {
						return withCode(exitFailure, fmt.Errorf("%s didn't take the key (%s): nothing was saved", p, c.Error))
					}
					fmt.Fprintln(cmd.OutOrStdout(), i18n.N("providers.key_works", len(c.Models)))
				}
				choice.Key = key
			}
			path, err := config.SetProvider(g.config, dir, choice)
			if err != nil {
				return withCode(exitUsage, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("providers.added", "provider", p, "path", path))
			return nil
		},
	}
	add.Flags().StringVar(&model, "model", "", "The provider's model to use (default: its default)")
	add.Flags().StringVar(&auth, "auth", "", "How it signs in: api_key (the default), adc (Google Cloud) or oauth")
	add.Flags().StringVar(&project, "project", "", "With --auth adc: the Google Cloud project")
	add.Flags().StringVar(&location, "location", "", "With --auth adc: the region")
	add.Flags().StringVar(&profile, "profile", "", "With --auth oauth: the ant profile (Anthropic)")
	add.Flags().BoolVar(&keepDefault, "keep-default", false, "Set it up without making it the one in use")
	add.Flags().BoolVar(&noCheck, "no-check", false, "Save the key without asking the provider first")

	check := &cobra.Command{
		Use:               "check <provider>",
		Short:             "Ask a provider set up whether it answers, with the models it offers",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: providerArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := named(args)
			if err != nil {
				return err
			}
			dir, err := workspaceDir(g)
			if err != nil {
				return err
			}
			pm, err := listProvider(cmd.Context(), dir, p)
			if err != nil {
				return err
			}
			if pm.Err != "" {
				return withCode(exitFailure, fmt.Errorf("%s: %s", p, pm.Err))
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.N("providers.works", len(pm.Models), "provider", p))
			return nil
		},
	}

	use := &cobra.Command{
		Use:               "use <provider> [model]",
		Short:             "Make a provider (and one of its models) the one in use",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: providerArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := named(args)
			if err != nil {
				return err
			}
			dir, err := scope()
			if err != nil {
				return err
			}
			path, err := config.SetValue(g.config, dir, "llm.provider", p)
			if err != nil {
				return withCode(exitUsage, err)
			}
			if len(args) == 2 {
				if path, err = config.SetValue(g.config, dir, "blitz.default_model", p+"/"+args[1]); err != nil {
					return withCode(exitUsage, err)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("providers.using", "provider", p, "path", path))
			return nil
		},
	}

	remove := &cobra.Command{
		Use:               "remove <provider>",
		Aliases:           []string{"rm", "delete"},
		Short:             "Remove a provider's API key (a workspace's: it then uses the global one)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: providerArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := named(args)
			if err != nil {
				return err
			}
			dir, err := scope()
			if err != nil {
				return err
			}
			if !removeConfirmed(cmd, yes, i18n.T("providers.remove_ask", "provider", p)) {
				return withCode(exitFailure, errNotRemoved)
			}
			path, err := config.RemoveAPIKey(g.config, dir, p)
			if err != nil {
				return withCode(exitUsage, err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("providers.removed", "provider", p, "path", path))
			return nil
		},
	}
	remove.Flags().BoolVarP(&yes, "yes", "y", false, "Don't ask first")

	for _, c := range []*cobra.Command{list, add, use, remove} {
		c.Flags().BoolVarP(&workspace, "workspace", "w", false, "This workspace's own setting (--dir, else the current folder)")
	}
	cmd.AddCommand(list, add, check, use, remove)
	return cmd
}
