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
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/observability"
)

// logFollowEvery is how often blitz service logs -f looks for new records
// (a variable for tests).
var logFollowEvery = time.Second

// newServiceLogsCommand is blitz service logs: the service's log, read from
// its files, so it works when the service doesn't (spec_067_cli_parity
// CLP-46; the desktop app's log viewer).
func newServiceLogsCommand(g *globalFlags) *cobra.Command {
	var day, level, grep string
	var limit int
	var days, follow, asJSON bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show the service's log: the newest day's records, newest last (--level, --grep, -f to keep watching)",
		Example: "  blitz service logs\n  blitz service logs --level warn --grep mcp\n" +
			"  blitz service logs --days\n  blitz service logs -f",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(g)
			if err != nil {
				return err
			}
			dir := config.ExpandHome(cfg.Log.Dir)
			if _, on, err := observability.ParseLevel(cfg.Log.Level); err != nil || !on || dir == "" {
				return withCode(exitUsage, fmt.Errorf("the service keeps no log: log.level is off (blitz config set log.level info turns it on)"))
			}
			out := cmd.OutOrStdout()
			if days {
				list, err := observability.LogDays(dir)
				if err != nil {
					return err
				}
				if asJSON {
					return writeJSON(out, nonNil(list))
				}
				for _, d := range list {
					fmt.Fprintln(out, d)
				}
				return nil
			}
			q := observability.LogQuery{Day: day, MinLevel: strings.ToUpper(level), Text: grep, Limit: limit}
			page, err := observability.ReadLog(dir, q)
			if err != nil {
				return withCode(exitUsage, err)
			}
			if asJSON {
				return writeJSON(out, page)
			}
			if len(page.Entries) == 0 && !follow {
				fmt.Fprintln(out, i18n.T("logs.none", "path", cmp.Or(page.Path, dir)))
				return nil
			}
			entries := slices.Clone(page.Entries)
			slices.Reverse(entries) // oldest first, as a log reads
			last := printLogEntries(out, entries, time.Time{})
			if page.Matched > len(page.Entries) {
				fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("logs.more", "shown", len(page.Entries), "matched", page.Matched))
			}
			for follow {
				select {
				case <-cmd.Context().Done():
					return nil
				case <-time.After(logFollowEvery):
				}
				q.Day = "" // the newest day, which may have turned
				page, err := observability.ReadLog(dir, q)
				if err != nil {
					continue
				}
				entries := slices.Clone(page.Entries)
				slices.Reverse(entries)
				last = printLogEntries(out, entries, last)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&day, "day", "", "The day to read, YYYY-MM-DD (default: the newest)")
	f.StringVar(&level, "level", "", "Only this level and above: debug, info, warn or error")
	f.StringVar(&grep, "grep", "", "Only records with every one of these words, in any field")
	f.IntVarP(&limit, "lines", "n", 200, "At most this many records")
	f.BoolVar(&days, "days", false, "List the days there are logs for")
	f.BoolVarP(&follow, "follow", "f", false, "Keep showing new records until Ctrl+C")
	f.BoolVar(&asJSON, "json", false, "Print JSON")
	_ = cmd.RegisterFlagCompletionFunc("level", cobra.FixedCompletions([]string{"debug", "info", "warn", "error"}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

// printLogEntries prints the records after since, oldest first, one line
// each: time, level, message, then its fields; the newest time printed.
func printLogEntries(out io.Writer, entries []observability.LogEntry, since time.Time) time.Time {
	last := since
	for _, e := range entries {
		if !e.Time.After(since) {
			continue
		}
		color := tui.Dim
		switch e.Level {
		case "WARN":
			color = tui.Yellow
		case "ERROR":
			color = tui.Red
		}
		var attrs []string
		for _, a := range e.Attrs {
			attrs = append(attrs, a.Key+"="+a.Value)
		}
		line := e.Time.Local().Format("15:04:05") + " " + color + fmt.Sprintf("%-5s", e.Level) + tui.Reset + " " + e.Msg
		if len(attrs) > 0 {
			line += " " + tui.Dim + strings.Join(attrs, " ") + tui.Reset
		}
		fmt.Fprintln(out, line)
		if e.Time.After(last) {
			last = e.Time
		}
	}
	return last
}
