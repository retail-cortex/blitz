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

	"github.com/spf13/cobra"
)

// newSessionsCommand is `blitz sessions export <id>`: a stored session as
// Markdown, from the service (spec_parity_027 PAR-SES-11).
func newSessionsCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "sessions", Short: "Work with saved sessions"}
	var out string
	export := &cobra.Command{
		Use:   "export <session-id>",
		Short: "Write a session as Markdown: prompts, answers and tool calls, secrets masked",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			md, err := r.ExportSession(args[0])
			if err != nil {
				return withCode(exitUsage, err)
			}
			if out == "" || out == "-" {
				_, err = fmt.Fprint(cmd.OutOrStdout(), md)
				return err
			}
			if err := os.WriteFile(out, []byte(md), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s\n", out)
			return nil
		},
	}
	export.Flags().StringVarP(&out, "output", "o", "", "The file to write (default: standard output)")
	cmd.AddCommand(export)
	return cmd
}
