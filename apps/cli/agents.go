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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"github.com/spf13/cobra"
)

// Background runs (spec_027_parity PAR-PAR-20): blitz --bg starts a turn in
// the service and returns; blitz agents lists them, and attach, logs and
// stop work with one.

// startBackground starts prompt as a background run in cfg's workspace
// (blitz --bg) and says how to follow it.
func startBackground(ctx context.Context, out io.Writer, cfg *config.Config, t api.Turn, imagePaths []string) error {
	// Background runs live in the shared service: started when it isn't
	// running, as for every other command that needs it (N26 cli-behaviour).
	sock, err := ensureService(ctx)
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(config.ExpandHome(cfg.Tools.WorkspaceDir))
	if err != nil {
		return err
	}
	r, err := client.Attach(ctx, sock, dir, nil)
	if err != nil {
		return err
	}
	// --image and @image mentions go with the run, as they do with a turn
	// (N26 api-client-config#8).
	if t.Images, err = r.LoadAttachments(imagePaths, t.Text, func(msg string) { fmt.Fprintf(os.Stderr, "!  %s\n", msg) }); err != nil {
		return withCode(exitUsage, fmt.Errorf("--image %w", err))
	}
	run, err := r.StartBackground(ctx, t)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, i18n.T("agents.started", "id", run.ID, "workspace", run.Workspace, "session", run.SessionID))
	fmt.Fprintln(out, i18n.T("agents.follow_hint", "id", run.ID))
	return nil
}

func newAgentsCommand() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "List the background runs in the Blitz service (blitz --bg), in every workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sock, err := ensureService(cmd.Context())
			if err != nil {
				return err
			}
			list, err := client.AttachBackground(sock).List(cmd.Context())
			if err != nil {
				return err
			}
			printRuns(cmd.OutOrStdout(), list, all, time.Now())
			return nil
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Include the runs that ended")
	return cmd
}

// printRuns writes the runs as a table: those still going, or all.
func printRuns(out io.Writer, list []api.BackgroundRun, all bool, now time.Time) {
	shown := 0
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, i18n.T("agents.columns"))
	for _, r := range list {
		if !all && !r.Active() {
			continue
		}
		shown++
		state := r.State
		if r.Waiting > 0 {
			state = i18n.T("agents.waiting", "count", r.Waiting)
		}
		end := now
		if !r.Ended.IsZero() {
			end = r.Ended
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t$%.2f\t%s\t%s\n", r.ID, state, end.Sub(r.Started).Round(time.Second), r.CostUSD,
			textutil.SanitizeTerminal(homeRel(r.Workspace)), textutil.SanitizeTerminal(textutil.Ellipsize(r.Prompt, 50)))
	}
	if shown == 0 {
		msg := i18n.T("agents.none_going")
		if all {
			msg = i18n.T("agents.none")
		}
		fmt.Fprintln(out, msg)
		return
	}
	tw.Flush()
}

// homeRel writes a directory under the home directory as ~/….
func homeRel(dir string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return dir
}

func newLogsCommand() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <id>",
		Short: "Show what a background run has done (with -f, as it goes on)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sock, err := ensureService(cmd.Context())
			if err != nil {
				return err
			}
			ctx, stop := interruptible(cmd.Context())
			defer stop()
			p := tui.NewPrinter(tui.PrinterOptions{Out: cmd.OutOrStdout(), Markdown: stdoutIsTerminal(), Width: terminalWidth()})
			p.Begin()
			res, ended, err := client.AttachBackground(sock).Logs(ctx, args[0], follow, p.Handle)
			p.End()
			switch {
			case errors.Is(err, api.ErrUnknownRun):
				return withCode(exitUsage, err)
			case ctx.Err() != nil:
				return nil
			case ended && err != nil:
				fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", i18n.T("agents.run_failed", "error", err.Error()))
			case ended:
				if line := tui.UsageLine(res.Before, res.After); line != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", line)
				}
			case err != nil:
				return err
			default:
				fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", i18n.T("agents.still_running", "id", args[0]))
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Keep showing the run until it ends")
	return cmd
}

func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <id>",
		Short: "Stop a background run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sock, err := ensureService(cmd.Context())
			if err != nil {
				return err
			}
			run, err := client.AttachBackground(sock).Stop(cmd.Context(), args[0])
			if errors.Is(err, api.ErrUnknownRun) {
				return withCode(exitUsage, err)
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("agents.stopped", "id", run.ID, "state", run.State, "session", run.SessionID, "workspace", run.Workspace))
			return nil
		},
	}
}

func newAttachCommand(o *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "attach <id>",
		Short: "Open a background run's session in the REPL, following the run and answering what it asks until it ends",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sock, err := ensureService(cmd.Context())
			if err != nil {
				return err
			}
			run, err := client.AttachBackground(sock).Find(cmd.Context(), args[0])
			if errors.Is(err, api.ErrUnknownRun) {
				return withCode(exitUsage, err)
			}
			if err != nil {
				return err
			}
			o.global.dir, o.resume, o.interactive = run.Workspace, run.SessionID, true
			if run.Active() {
				o.attachRun = run.ID
			}
			return runRoot(cmd, o, nil)
		},
	}
}

// interruptible is ctx, cancelled by Ctrl+C.
func interruptible(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}
