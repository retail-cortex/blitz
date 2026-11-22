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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/docx"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/spf13/cobra"
)

// newSessionsCommand is `blitz sessions export <id>`: a stored session as
// Markdown, from the service (spec_027_parity PAR-SES-11).
func newSessionsCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "sessions", Short: "Work with saved sessions"}
	var out string
	export := &cobra.Command{
		Use:   "export <session-id>",
		Short: "Write a session as Markdown (or Word, with -o name.docx): prompts, answers and tool calls, secrets masked",
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
			data, err := exportData(md, out)
			if err != nil {
				return err
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("session.cli_wrote", "path", out))
			return nil
		},
	}
	export.Flags().StringVarP(&out, "output", "o", "", "The file to write, a .docx name for a Word document (default: standard output)")
	usage := &cobra.Command{
		Use:   "usage <session-id>",
		Short: "List what a session spent: each model call, its tokens and cost, and the totals by model and purpose",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			calls, totals, err := r.SessionLedgerOf(args[0])
			if err != nil {
				return withCode(exitUsage, err)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), sessionUsageText(args[0], calls, totals))
			return err
		},
	}
	var all, asJSON, full bool
	var turn int
	list := &cobra.Command{
		Use:   "list",
		Short: "List this workspace's saved sessions (--all: every workspace's), newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			list, err := r.ListSessions(all)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), nonNil(list))
			}
			if len(list) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("sessions.none"))
				return nil
			}
			for _, s := range list {
				title := cmp.Or(s.Title, s.Snapshot, "-")
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", s.ID, title, i18n.N("session.messages", s.MessageCount))
			}
			return nil
		},
	}
	list.Flags().BoolVarP(&all, "all", "a", false, "Every workspace's sessions")
	list.Flags().BoolVar(&asJSON, "json", false, "Print JSON")
	reasoning := &cobra.Command{
		Use:   "reasoning <session-id>",
		Short: "Show a turn's reasoning as a tree: its model calls, the steps they shared, tools and sub-agents (the last turn unless --turn)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			got, err := r.Reasoning(args[0], turn-1)
			if err != nil {
				return withCode(exitUsage, err)
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), got)
			}
			tui.PrintReasoning(cmd.OutOrStdout(), got, full)
			return nil
		},
	}
	reasoning.Flags().IntVar(&turn, "turn", 0, "The turn, from 1 (default: the last)")
	reasoning.Flags().BoolVar(&full, "full", false, "Each step's text too")
	reasoning.Flags().BoolVar(&asJSON, "json", false, "Print JSON")
	cmd.AddCommand(export, usage, list, reasoning)
	return cmd
}

// sessionUsageText is a session's ledger as `blitz sessions usage` prints
// it (spec_056 CA-125): each call, then the totals by model and purpose.
func sessionUsageText(id string, calls []api.Call, t api.Totals) string {
	if len(calls) == 0 {
		return i18n.T("session.usage_none", "id", id) + "\n"
	}
	var b strings.Builder
	fmt.Fprintln(&b, i18n.T("cost.title", "id", id))
	for _, c := range calls {
		when := "-"
		if !c.Time.IsZero() {
			when = c.Time.Local().Format(time.DateTime)
		}
		model := cmp.Or(c.Model, "-")
		cost := i18n.T("session.usage_no_price")
		if c.Usage.Unpriced == 0 {
			cost = fmt.Sprintf("$%.4f", c.Usage.CostUSD+c.Usage.SearchCostUSD)
		}
		fmt.Fprintf(&b, "  %s  %-10s  %-28s  %s\n", when, i18n.T("cost.purpose."+cmp.Or(c.Purpose, api.PurposeTurn)), model,
			i18n.T("session.usage_tokens", "input", c.Usage.Input, "output", c.Usage.Output, "cost", cost))
	}
	u := t.All
	switch {
	case u.Unpriced == 0 && u.Priced:
		fmt.Fprintf(&b, "  %s\n", i18n.T("cost.estimate", "cost", fmt.Sprintf("$%.4f", u.CostUSD+u.SearchCostUSD)))
	default:
		fmt.Fprintf(&b, "  %s\n", i18n.T("cost.partial", "cost", fmt.Sprintf("$%.4f", u.CostUSD+u.SearchCostUSD), "count", u.Unpriced))
	}
	for _, line := range tui.CostBreakdown(t) {
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

// exportData is a session's Markdown as the file out wants it: a Word
// document for a .docx name (FS-75), else the Markdown itself.
func exportData(md, out string) ([]byte, error) {
	if strings.EqualFold(filepath.Ext(out), ".docx") {
		return docx.Render([]byte(md), docx.Options{})
	}
	return []byte(md), nil
}
