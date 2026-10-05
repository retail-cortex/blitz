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
	"os"
	"os/exec"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/spf13/cobra"
)

// newMemoryCommand is `blitz memory list|show|edit|forget`: the notes the
// agent saved in a workspace with its remember tool (spec_parity_027
// PAR-MEM-11), through the Blitz service.
func newMemoryCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "memory", Short: "List, show, edit and forget the notes the agent saved"}
	notes := func(cmd *cobra.Command) ([]api.Note, error) {
		r, err := attachWorkspace(cmd.Context(), g)
		if err != nil {
			return nil, err
		}
		return r.ListNotes()
	}
	// find is the note named name, or the only one whose name starts so.
	find := func(cmd *cobra.Command, name string) (api.Note, error) {
		list, err := notes(cmd)
		if err != nil {
			return api.Note{}, err
		}
		var found []api.Note
		for _, n := range list {
			if n.Name == name {
				return n, nil
			}
			if strings.HasPrefix(n.Name, name) {
				found = append(found, n)
			}
		}
		if len(found) == 1 {
			return found[0], nil
		}
		return api.Note{}, withCode(exitUsage, fmt.Errorf("%w: %q", api.ErrNoNote, name))
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List this workspace's notes, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := notes(cmd)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(out, "No notes: the agent saves them with its remember tool.")
				return nil
			}
			for _, n := range list {
				fmt.Fprintf(out, "%s\t%s\t%s\n", n.Name, n.Kind, firstLine(n.Text, 80))
			}
			return nil
		},
	}
	show := &cobra.Command{
		Use:   "show <name>",
		Short: "Show a note (its name, or the start of it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := find(cmd, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s (%s, %s)\n%s\n\n%s\n", n.Name, n.Kind, n.Time.Format("2006-01-02 15:04"), n.Path, n.Text)
			return nil
		},
	}
	edit := &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit a note in $VISUAL or $EDITOR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := find(cmd, args[0])
			if err != nil {
				return err
			}
			editor := strings.TrimSpace(os.Getenv("VISUAL"))
			if editor == "" {
				editor = strings.TrimSpace(os.Getenv("EDITOR"))
			}
			if editor == "" {
				return withCode(exitUsage, fmt.Errorf("set $VISUAL or $EDITOR, or edit %s", n.Path))
			}
			// The user's own command, run through the shell like git does.
			c := exec.Command("/bin/sh", "-c", editor+` "$1"`, "sh", n.Path)
			c.Stdin, c.Stdout, c.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			return c.Run()
		},
	}
	forget := &cobra.Command{
		Use:     "forget <name>",
		Aliases: []string{"delete", "rm"},
		Short:   "Delete a note",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := find(cmd, args[0])
			if err != nil {
				return err
			}
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			if err := r.ForgetNote(n.Name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Forgot %s.\n", n.Name)
			return nil
		},
	}
	cmd.AddCommand(list, show, edit, forget)
	return cmd
}

// firstLine is s's first line, cut to n characters.
func firstLine(s string, n int) string {
	s, _, _ = strings.Cut(s, "\n")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
