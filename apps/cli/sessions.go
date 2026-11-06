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
	cmd.AddCommand(export, usage)
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
