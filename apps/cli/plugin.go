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
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/plugins"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// newPluginCommand is `blitz plugin`: install, list, enable, disable,
// remove and update plugins, add marketplaces, and import other agents'
// plugins (spec_parity_027 PAR-PLG-02..04).
func newPluginCommand() *cobra.Command {
	return newPluginCommandWith(plugins.Default())
}

func newPluginCommandWith(store *plugins.Store) *cobra.Command {
	cmd := &cobra.Command{Use: "plugin", Aliases: []string{"plugins"}, Short: "Install and manage plugins: skills, commands, agents, hooks and MCP servers"}
	var yes bool

	install := &cobra.Command{
		Use:   "install <dir|file.zip|url|git repo|name[@marketplace]>",
		Short: "Install a plugin, after showing what it adds",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := fetchPlugin(cmd, store, args[0])
			if err != nil {
				return err
			}
			defer st.Discard()
			return installStaged(cmd, store, st, yes)
		},
	}
	install.Flags().BoolVarP(&yes, "yes", "y", false, "Install without asking")

	update := &cobra.Command{
		Use:   "update [name…]",
		Short: "Reinstall plugins from where they came from (all without names)",
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := store.List()
			if err != nil {
				return err
			}
			var errs []error
			for _, i := range list {
				if len(args) > 0 && !contains(args, i.Name) {
					continue
				}
				st, err := fetchPlugin(cmd, store, i.Source)
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", i.Name, err))
					continue
				}
				if st.Version == i.Version {
					if h, _ := plugins.Hash(st.Dir); h == i.Hash {
						fmt.Fprintf(cmd.OutOrStdout(), "%s %s is up to date.\n", i.Name, i.Version)
						st.Discard()
						continue
					}
				}
				err = installStaged(cmd, store, st, yes)
				st.Discard()
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", i.Name, err))
				}
			}
			for _, a := range args {
				if _, err := store.Get(a); err != nil {
					errs = append(errs, err)
				}
			}
			return errors.Join(errs...)
		},
	}
	update.Flags().BoolVarP(&yes, "yes", "y", false, "Update without asking")

	list := &cobra.Command{
		Use:   "list",
		Short: "List the installed plugins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			all, err := store.List()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(all) == 0 {
				fmt.Fprintln(out, "No plugins installed. Install one with: blitz plugin install <dir|url|git repo>")
				return nil
			}
			for _, i := range all {
				state := "enabled"
				if !i.Enabled {
					state = "disabled"
				}
				if h, err := plugins.Hash(store.PluginDir(i)); err != nil || h != i.Hash {
					state += ", changed since installed: not loaded"
				}
				fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", i.Name, i.Version, state, i.Source)
			}
			return nil
		},
	}

	show := &cobra.Command{
		Use:   "show <name>",
		Short: "Show what an installed plugin adds",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			i, err := store.Get(args[0])
			if err != nil {
				return withCode(exitUsage, err)
			}
			p, err := plugins.Read(store.PluginDir(i))
			if err != nil {
				return err
			}
			printPlugin(cmd.OutOrStdout(), p)
			fmt.Fprintf(cmd.OutOrStdout(), "\nfrom %s, %s\n", i.Source, i.Hash)
			return nil
		},
	}

	toggle := func(use, short string, on bool) *cobra.Command {
		return &cobra.Command{
			Use:   use + " <name>",
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := store.SetEnabled(args[0], on); err != nil {
					if errors.Is(err, plugins.ErrNotInstalled) {
						return withCode(exitUsage, err)
					}
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %sd for every workspace (a project's [plugins] enable or disable wins there).\n", args[0], use)
				restartHint(cmd.OutOrStdout())
				return nil
			},
		}
	}
	remove := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"uninstall"},
		Short:   "Remove an installed plugin",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := store.Remove(args[0]); err != nil {
				if errors.Is(err, plugins.ErrNotInstalled) {
					return withCode(exitUsage, err)
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s.\n", args[0])
			restartHint(cmd.OutOrStdout())
			return nil
		},
	}

	market := &cobra.Command{Use: "marketplace", Short: "Add, list and remove plugin marketplaces"}
	market.AddCommand(&cobra.Command{
		Use:   "add <url|git repo|dir>",
		Short: "Add a marketplace: a marketplace.toml listing plugins with their hashes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := store.AddMarketplace(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Added the marketplace %s, with %d plugins:\n", m.Name, len(m.Plugins))
			for _, e := range m.Plugins {
				fmt.Fprintf(out, "  %s %s\t%s\n", e.Name, e.Version, e.Description)
			}
			fmt.Fprintf(out, "Install one with: blitz plugin install <name>@%s\n", m.Name)
			return nil
		},
	}, &cobra.Command{
		Use:   "list",
		Short: "List the marketplaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := store.Marketplaces()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No marketplaces. Add one with: blitz plugin marketplace add <url>")
			}
			for _, k := range list {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", k.Name, k.URL)
			}
			return nil
		},
	}, &cobra.Command{
		Use:   "remove <name>",
		Short: "Forget a marketplace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := store.RemoveMarketplace(args[0]); err != nil {
				return withCode(exitUsage, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed the marketplace %s.\n", args[0])
			return nil
		},
	})

	var out string
	importCmd := &cobra.Command{
		Use:   "import claude|gemini <dir>",
		Short: "Convert a Claude Code plugin or a Gemini CLI extension into a Blitz plugin",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var r *plugins.Report
			var err error
			dst := out
			if dst == "" {
				dst = filepath.Base(filepath.Clean(args[1])) + "-blitz"
			}
			switch args[0] {
			case "claude":
				r, err = plugins.ImportClaude(args[1], dst)
			case "gemini":
				r, err = plugins.ImportGemini(args[1], dst)
			default:
				return withCode(exitUsage, fmt.Errorf("import claude or gemini, not %q", args[0]))
			}
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Made the plugin %s %s in %s:\n", r.Plugin.Name, r.Plugin.Version, r.Out)
			for _, a := range r.Added {
				fmt.Fprintf(w, "  + %s\n", a)
			}
			if len(r.Skipped) > 0 {
				fmt.Fprintln(w, "Not converted:")
				for _, s := range r.Skipped {
					fmt.Fprintf(w, "  - %s\n", s)
				}
			}
			fmt.Fprintf(w, "Try it with --plugin-dir %s, or install it: blitz plugin install %s\n", r.Out, r.Out)
			return nil
		},
	}
	importCmd.Flags().StringVarP(&out, "out", "o", "", "Where to write the plugin (default <dir>-blitz)")

	cmd.AddCommand(install, update, list, show, toggle("enable", "Load a plugin in every workspace", true),
		toggle("disable", "Stop loading a plugin", false), remove, market, importCmd)
	return cmd
}

// fetchPlugin gets a plugin by marketplace name, or from a path or URL.
func fetchPlugin(cmd *cobra.Command, store *plugins.Store, src string) (*plugins.Staged, error) {
	st, ok, err := store.FromMarketplace(cmd.Context(), src)
	if ok || err != nil {
		return st, err
	}
	return plugins.Fetch(cmd.Context(), src)
}

// installStaged shows what the plugin adds and installs it once agreed.
func installStaged(cmd *cobra.Command, store *plugins.Store, st *plugins.Staged, yes bool) error {
	out := cmd.OutOrStdout()
	printPlugin(out, st.Plugin)
	if !yes {
		ok, err := confirmPlugin(cmd.InOrStdin(), out, st.RunsCode())
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "Not installed.")
			return nil
		}
	}
	i, err := store.Install(st)
	if err != nil {
		return err
	}
	state := "enabled"
	if !i.Enabled {
		state = "disabled, as before"
	}
	fmt.Fprintf(out, "Installed %s %s (%s).\n", i.Name, i.Version, state)
	restartHint(out)
	return nil
}

func printPlugin(out io.Writer, p *plugins.Plugin) {
	fmt.Fprintf(out, "%s %s", p.Name, p.Version)
	if p.Description != "" {
		fmt.Fprintf(out, ": %s", p.Description)
	}
	fmt.Fprintln(out)
	summary := p.Summary()
	if len(summary) == 0 {
		fmt.Fprintln(out, "  (it adds nothing)")
	}
	for _, s := range summary {
		fmt.Fprintf(out, "  %s\n", s)
	}
}

// confirmPlugin asks before installing; without a terminal it refuses
// (--yes installs anyway).
func confirmPlugin(in io.Reader, out io.Writer, runsCode bool) (bool, error) {
	if f, ok := in.(*os.File); ok && !term.IsTerminal(int(f.Fd())) {
		return false, withCode(exitUsage, errors.New("not a terminal to ask in: add --yes to install"))
	}
	q := "Install it? [y/N] "
	if runsCode {
		q = "It runs code on your machine (above). Install it? [y/N] "
	}
	fmt.Fprint(out, q)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes", nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
