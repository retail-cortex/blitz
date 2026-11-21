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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/agentpkg"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/enterprise"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/packages"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/pkg/secretscan"
	"github.com/spf13/cobra"
)

// blitz pkg (spec_065 PK-10 to PK-16): a workspace's agents, skills,
// workflows, rules, guardrails and evals as packages, Castor's cstr
// generalized: added from an organization's registry, GitHub or a folder
// with a lockfile, verified, checked, compiled, searched and published.

func newPkgCommand(g *globalFlags) *cobra.Command {
	var registryURL string
	cmd := &cobra.Command{
		Use:   "pkg",
		Short: "Your workspace's agents, skills and workflows as packages: add, verify, validate, compile, search, publish",
		Long: `Works with the workspace's assets (.agents: agents, skills, workflows,
rules, guardrails and evals) as packages, as Castor's cstr does for skills.
Packages come from your organization's registry (blitz://<host>/<kind>/<name>@<range>),
GitHub (github://<owner>/<repo>[@<ref>]/<folder>) or a folder (file://…),
each recorded in .agents/blitz.lock with its version and digest.

The registry is your organization's: through your sign-in, or, for CI, a
registry token in BLITZ_REGISTRY_TOKEN with --registry (or the
organization you're connected to).`,
	}
	cmd.PersistentFlags().StringVar(&registryURL, "registry", "", "Your organization's service, for BLITZ_REGISTRY_TOKEN (default: the one you're connected to)")
	reg := func(cmd *cobra.Command) (*enterprise.Registry, error) { return pkgRegistry(g, registryURL) }
	cmd.AddCommand(newPkgAddCommand(g, reg), newPkgVerifyCommand(g), newPkgValidateCommand(g), newPkgCompileCommand(g), newPkgListCommand(g),
		newPkgSearchCommand(reg), newPkgPublishCommand(g, reg))
	return cmd
}

// pkgRegistry is the organization's registry: a token's, from the
// environment, or the signed-in person's; nil without either.
func pkgRegistry(g *globalFlags, registryURL string) (*enterprise.Registry, error) {
	cfg, err := loadConfig(g)
	if err != nil {
		return nil, err
	}
	u := registryURL
	if u == "" {
		u = cfg.Enterprise.URL
	}
	if tok := os.Getenv(enterprise.RegistryTokenEnv); tok != "" {
		if u == "" {
			return nil, errors.New(i18n.T("pkg.registry_where"))
		}
		n, err := enterprise.Normalize(u)
		if err != nil {
			return nil, err
		}
		return &enterprise.Registry{URL: n, Token: tok}, nil
	}
	if cfg.Enterprise.URL == "" {
		return nil, nil
	}
	n, err := enterprise.Normalize(cfg.Enterprise.URL)
	if err != nil {
		return nil, err
	}
	c := enterprise.Shared(n, func() secrets.Store { return secrets.Default(config.ConfigDir("")) }, enterprise.Options{})
	if !c.Status().SignedIn {
		return nil, nil
	}
	return &enterprise.Registry{Client: c}, nil
}

// pkgRoot is the workspace's assets folder: -d's, else this folder's.
func pkgRoot(g *globalFlags) string {
	ws := g.dir
	if ws == "" {
		ws = "."
	}
	return packages.Root(ws)
}

func newPkgAddCommand(g *globalFlags, reg func(*cobra.Command) (*enterprise.Registry, error)) *cobra.Command {
	var kind string
	var force bool
	cmd := &cobra.Command{
		Use:   "add <address>",
		Short: "Add a package to the workspace: blitz://, github:// or a folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := pkgRoot(g)
			o := packages.Options{Kind: kind, Force: force, GitHubToken: os.Getenv("GITHUB_TOKEN"), GitHubBase: os.Getenv("BLITZ_GITHUB_ARCHIVES")}
			if strings.HasPrefix(args[0], "blitz://") {
				r, err := reg(cmd)
				if err != nil {
					return err
				}
				if r != nil {
					o.Registry = r
				}
			}
			added, err := packages.Add(cmd.Context(), root, args[0], o)
			for _, e := range added {
				line := i18n.T("pkg.added", "asset", e.Kind+"/"+e.Name, "uri", e.URI)
				if e.Version != "" {
					line += " · " + e.Version
				}
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "What a single .md file is: agent, rule, guardrail or eval (default: its folder's, else an agent)")
	cmd.Flags().BoolVar(&force, "force", false, "Replace an asset of the workspace's own of that name")
	return cmd
}

func newPkgVerifyCommand(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Check the packages the workspace added are as they were added (.agents/blitz.lock); exits 1 when one isn't",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root := pkgRoot(g)
			results, err := packages.Verify(root)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(results) == 0 {
				fmt.Fprintln(out, i18n.T("pkg.none_locked"))
				return nil
			}
			bad := 0
			for _, r := range results {
				mark := map[string]string{packages.Verified: "✓", packages.Modified: "✗", packages.Missing: "✗"}[r.Status]
				fmt.Fprintf(out, "%s %s · %s · %s\n", mark, r.Key, i18n.T("pkg.status."+r.Status), r.URI)
				if r.Status != packages.Verified {
					bad++
				}
			}
			if bad > 0 {
				return withCode(exitFailure, errors.New(i18n.T("pkg.verify_failed", "count", bad)))
			}
			return nil
		},
	}
}

func newPkgValidateCommand(g *globalFlags) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check the workspace's assets as Blitz reads them, and what's worth fixing; exits 1 on a problem",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(g)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			// The engine's parsers check them, in the service.
			b, _, _, err := openBackend(ctx, cfg, backendOptions{trustProject: g.trustProject, flags: g}, func(s string) { fmt.Fprintln(cmd.ErrOrStderr(), s) })
			if err != nil {
				return err
			}
			defer b.Close()
			found, err := b.ValidateAssets(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if err := enc.Encode(found); err != nil {
					return err
				}
			}
			problems := 0
			for _, f := range found {
				problems += len(f.Problems)
				if asJSON {
					continue
				}
				for _, p := range f.Problems {
					fmt.Fprintf(out, "✗ %s: %s\n", f.Key, p)
				}
				for _, w := range f.Warnings {
					fmt.Fprintf(out, "⚠ %s: %s\n", f.Key, w)
				}
			}
			if problems > 0 {
				return withCode(exitFailure, errors.New(i18n.T("pkg.validate_failed", "count", problems)))
			}
			if !asJSON && len(found) == 0 {
				fmt.Fprintln(out, i18n.T("pkg.valid"))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print what it found as JSON")
	return cmd
}

func newPkgCompileCommand(g *globalFlags) *cobra.Command {
	var castor bool
	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Compile the workspace's assets into .agents/agents_manifest.json (and Castor's skills_manifest.json with --castor)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root := pkgRoot(g)
			wrote, err := packages.Compile(root, castor)
			for _, p := range wrote {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("pkg.wrote", "path", p))
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&castor, "castor", false, "Also write Castor's skills_manifest.json, for its loaders")
	return cmd
}

func newPkgListCommand(g *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the workspace's assets, and where each package came from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root := pkgRoot(g)
			lock, err := agentpkg.ReadLock(filepath.Join(root, agentpkg.LockFile))
			if err != nil {
				return err
			}
			assets, _ := agentpkg.ReadAssets(root, packages.Kinds...)
			out := cmd.OutOrStdout()
			keys := slices.Sorted(func(yield func(string) bool) {
				for k := range assets {
					if !yield(k) {
						return
					}
				}
			})
			if len(keys) == 0 {
				fmt.Fprintln(out, i18n.T("pkg.none"))
			}
			for _, k := range keys {
				if e, ok := lock.Assets[k]; ok {
					line := k + " · " + e.URI
					if e.Version != "" {
						line += " · " + e.Version
					}
					fmt.Fprintln(out, line)
				} else {
					fmt.Fprintln(out, k+" · "+i18n.T("pkg.own"))
				}
			}
			return nil
		},
	}
}

func newPkgSearchCommand(reg func(*cobra.Command) (*enterprise.Registry, error)) *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "search [words]",
		Short: "Search your organization's registry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := reg(cmd)
			if err != nil {
				return err
			}
			if r == nil {
				return errors.New(i18n.T("pkg.no_registry"))
			}
			q := ""
			if len(args) > 0 {
				q = args[0]
			}
			found, err := r.Search(cmd.Context(), q, kind)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(found) == 0 {
				fmt.Fprintln(out, i18n.T("pkg.found_none"))
			}
			for _, a := range found {
				line := fmt.Sprintf("%s/%s %s  %s", a.Kind, a.Name, a.Version, a.Description)
				if l := a.Label(); l != "" {
					line += "  " + l
				}
				fmt.Fprintf(out, "%s\n    %s\n", line, a.URI)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "agent, skill, workflow, rule or guardrail")
	return cmd
}

func newPkgPublishCommand(g *globalFlags, reg func(*cobra.Command) (*enterprise.Registry, error)) *cobra.Command {
	var bump, note string
	cmd := &cobra.Command{
		Use:   "publish <kind>/<name>",
		Short: "Publish one of the workspace's assets to your organization's registry, through its evals",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, name, ok := strings.Cut(args[0], "/")
			if !ok || kind == "" || name == "" {
				return withCode(exitUsage, errors.New(i18n.T("org.item_arg")))
			}
			root := pkgRoot(g)
			files, why := agentpkg.ReadAsset(root, kind, name)
			if why != "" {
				return fmt.Errorf("%s: %s", args[0], why)
			}
			// Read for secrets before it leaves the computer, as sharing
			// and syncing are (EN-36b; security review packages#3).
			if found := enterprise.PersonalSecrets(files); len(found) > 0 {
				return withCode(exitUsage, fmt.Errorf("%s isn't published: it holds what look like secrets: %s. Remove them, or mark a false alarm with %s on its line", args[0], enterprise.SecretsSummary(found), secretscan.AllowMarker))
			}
			r, err := reg(cmd)
			if err != nil {
				return err
			}
			if r == nil {
				return errors.New(i18n.T("pkg.no_registry"))
			}
			p, err := r.Publish(cmd.Context(), kind, name, files, bump, note)
			if err != nil {
				return err
			}
			if p.Evaluating != nil {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("pkg.evaluating", "asset", args[0]))
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("pkg.published", "asset", args[0], "version", p.Version, "uri", p.URI))
			return nil
		},
	}
	cmd.Flags().StringVar(&bump, "bump", "", "patch (a fix, the default), minor (an addition) or major (a change others must follow)")
	cmd.Flags().StringVar(&note, "note", "", "What changed")
	return cmd
}
