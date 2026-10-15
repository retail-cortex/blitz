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
	"strings"
	"text/tabwriter"

	"github.com/retail-cortex/blitz/pkg/skillreg"
	"github.com/spf13/cobra"
)

// newSkillsCommand is `blitz skills`: skill registries, and the skills
// installed from them (spec_skills_013 SK-90..SK-96).
func newSkillsCommand() *cobra.Command {
	return newSkillsCommandWith(skillreg.Default())
}

func newSkillsCommandWith(store *skillreg.Store) *cobra.Command {
	cmd := &cobra.Command{Use: "skills", Short: "Install skills from registries, such as Castor, and keep them up to date"}

	registry := &cobra.Command{Use: "registry", Aliases: []string{"registries"}, Short: "Add, list and remove skill registries"}
	var name string
	var tokenStdin bool
	add := &cobra.Command{
		Use:   "add <url|index.json>",
		Short: "Add a registry: an https URL (or a local file) serving a skills index",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token := ""
			if tokenStdin {
				t, err := readToken(cmd.InOrStdin())
				if err != nil {
					return err
				}
				token = t
			}
			idx, err := store.Add(cmd.Context(), name, args[0], token)
			if err != nil {
				return withCode(exitUsage, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s: %d skills. Search them with: blitz skills search\n", idx.Name, len(idx.Skills))
			return nil
		},
	}
	add.Flags().StringVar(&name, "name", "", "What to call it (default: the index's name)")
	add.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read an access token from standard input; it's kept in the system keychain")

	list := &cobra.Command{
		Use:   "list",
		Short: "List the registries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			regs, err := store.Registries()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(regs) == 0 {
				fmt.Fprintln(out, "No registries. Add one with: blitz skills registry add <url>")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			for _, r := range regs {
				fmt.Fprintf(tw, "%s\t%s\n", r.Name, r.URL)
			}
			return tw.Flush()
		},
	}
	remove := &cobra.Command{
		Use:   "remove <name>",
		Short: "Forget a registry and its token (skills installed from it stay)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := store.Remove(args[0]); err != nil {
				return withCode(exitUsage, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s.\n", args[0])
			return nil
		},
	}
	registry.AddCommand(add, list, remove)

	search := &cobra.Command{
		Use:   "search [words…]",
		Short: "Find skills in the registries (all of them without words)",
		RunE: func(cmd *cobra.Command, args []string) error {
			found, err := store.Search(cmd.Context(), strings.Join(args, " "))
			out := cmd.OutOrStdout()
			if len(found) == 0 && err == nil {
				fmt.Fprintln(out, "No skills found.")
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			for _, f := range found {
				fmt.Fprintf(tw, "%s/%s\t%s\t%s\n", f.Registry, f.Name, f.Version, f.Description)
			}
			return errors.Join(tw.Flush(), err)
		},
	}

	var project bool
	workspace := func() (string, error) {
		if !project {
			return "", nil
		}
		return os.Getwd()
	}
	install := &cobra.Command{
		Use:   "install <registry/name|name>",
		Short: "Install a skill, checking its archive's hash",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := workspace()
			if err != nil {
				return err
			}
			i, err := store.Install(cmd.Context(), args[0], dir)
			if err != nil {
				return withCode(exitUsage, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s %s from %s in %s. Sessions see it at once.\n", i.Name, i.Version, i.Registry, i.Dir)
			return nil
		},
	}
	install.Flags().BoolVar(&project, "project", false, "Install it in this folder's .agents/skills, for everyone working in it")

	update := &cobra.Command{
		Use:   "update [name…]",
		Short: "Update skills installed from registries (all without names)",
		RunE: func(cmd *cobra.Command, args []string) error {
			done, err := store.Update(cmd.Context(), args...)
			out := cmd.OutOrStdout()
			for _, i := range done {
				fmt.Fprintf(out, "Updated %s to %s.\n", i.Name, i.Version)
			}
			if len(done) == 0 && err == nil {
				fmt.Fprintln(out, "Everything is up to date.")
			}
			return err
		},
	}

	uninstall := &cobra.Command{
		Use:   "uninstall <name>",
		Short: "Remove a skill installed from a registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			i, err := store.Uninstall(args[0], dir)
			if err != nil {
				return withCode(exitUsage, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from %s.\n", i.Name, i.Dir)
			return nil
		},
	}

	installed := &cobra.Command{
		Use:   "list",
		Short: "List the skills installed from registries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			all, err := store.Installed()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(all) == 0 {
				fmt.Fprintln(out, "No skills installed from registries. Find some with: blitz skills search")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			for _, i := range all {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", i.Name, i.Version, i.Registry, i.Dir)
			}
			return tw.Flush()
		},
	}

	cmd.AddCommand(registry, search, install, update, uninstall, installed)
	return cmd
}

// readToken reads a token from the first line of r.
func readToken(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	t := strings.TrimSpace(line)
	if t == "" {
		return "", withCode(exitUsage, errors.New("no token on standard input"))
	}
	return t, nil
}
