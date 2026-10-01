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
	"errors"
	"fmt"
	"os"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/worktree"
	"github.com/spf13/cobra"
)

// enterWorktree makes the run's workspace a new git worktree
// (--worktree[=name], --ref) of the repository --dir (or the working
// directory) is in (spec_parity_027 PAR-PAR-10). The workspace lock and
// checkpoints are the worktree's own.
func enterWorktree(g *globalFlags) error {
	base := g.dir
	if base == "" {
		base, _ = os.Getwd()
	}
	name := g.worktree
	if name == "new" {
		name = ""
	}
	w, err := worktree.Create(base, name, g.ref)
	if errors.Is(err, worktree.ErrNotRepo) {
		return withCode(exitUsage, fmt.Errorf("--worktree: %w", err))
	}
	if err != nil {
		return fmt.Errorf("--worktree: %w", err)
	}
	g.dir = w.Path
	fmt.Fprintf(os.Stderr, "%sIn the worktree %s, on the branch %s (blitz worktrees remove %s when done).%s\n", tui.Dim, w.Path, w.Branch, w.Name, tui.Reset)
	return nil
}

// newWorktreesCommand is `blitz worktrees list|remove|prune` (PAR-PAR-12).
func newWorktreesCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "worktrees", Short: "List and remove the git worktrees Blitz made"}
	dir := func() string {
		if g.dir != "" {
			return g.dir
		}
		d, _ := os.Getwd()
		return d
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List Blitz's worktrees of this repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			all, err := worktree.List(dir())
			if err != nil {
				return withCode(exitUsage, err)
			}
			out := cmd.OutOrStdout()
			if len(all) == 0 {
				fmt.Fprintln(out, "No worktrees. Start one with: blitz --worktree[=name]")
				return nil
			}
			for _, w := range all {
				state := ""
				switch {
				case w.Missing:
					state = " (folder gone: blitz worktrees prune)"
				case worktree.Dirty(w):
					state = " (changes not committed)"
				}
				fmt.Fprintf(out, "%s\t%s\t%s%s\n", w.Name, w.Branch, w.Path, state)
			}
			return nil
		},
	}
	var force bool
	remove := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a worktree (its branch stays, to merge or delete)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := worktree.Remove(dir(), args[0], force)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed the worktree %s; its branch %s stays.\n", w.Name, w.Branch)
			return nil
		},
	}
	remove.Flags().BoolVar(&force, "force", false, "Remove it with changes not committed")
	prune := &cobra.Command{
		Use:   "prune",
		Short: "Forget worktrees whose folders are gone",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := worktree.Prune(dir()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Pruned.")
			return nil
		},
	}
	cmd.AddCommand(list, remove, prune)
	return cmd
}
