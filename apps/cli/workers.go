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
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/apps/cli/internal/tui"
	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/spf13/cobra"
)

// workerOps are a workspace's workers, in the service.
type workerOps interface {
	ListWorkers() ([]api.WorkerInfo, error)
	EnableWorker(name, hash string) (api.WorkerInfo, error)
	DisableWorker(name string) (api.WorkerInfo, error)
	RunWorker(ctx context.Context, name string, on func(api.Event)) (api.Run, error)
	WorkerRuns(name string, limit int) ([]api.Run, error)
	UndoWorkerRun(runID string, force bool) ([]string, error)
	RunningWorkerRuns() ([]api.Run, error)
	StopWorkerRun(runID string) error
	MoveToService(ctx context.Context, name string, force bool) ([]string, bool, error)
}

// openWorkers reaches the workspace's workers in the Blitz service (started
// when it isn't running), which owns them. Nothing needs closing: the
// client holds no connection of its own.
func openWorkers(ctx context.Context, g *globalFlags) (workerOps, error) {
	cfg, err := loadConfig(g)
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(config.ExpandHome(cfg.Tools.WorkspaceDir))
	if err != nil {
		return nil, err
	}
	sock, err := ensureService(ctx)
	if err != nil {
		return nil, err
	}
	return client.AttachWorkers(sock, dir), nil
}

func newWorkersCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "jobs",
		Aliases: []string{"workers"}, // their name until v0.7.2
		Short:   "List, enable and run the workspace's jobs (.agents/workers/<name>/WORKER.md)",
		Long: `Jobs are workflows a workspace defines in .agents/workers/<name>/WORKER.md
(or workers/<name>/WORKER.md) and the
Blitz service runs on their schedules, unattended. A job runs only once
enabled, pinned to the content you reviewed: editing it disables it until it is
enabled again. It may do only what its permissions allow; anything else is
refused and recorded. (Jobs were called workers until v0.7.2: 'blitz workers'
still works.)`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return workersList(cmd, g) },
	}
	var yes bool
	enable := &cobra.Command{
		Use:   "enable <name>",
		Short: "Show a job and enable it as shown",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return workersEnable(cmd, g, args[0], yes) },
	}
	enable.Flags().BoolVarP(&yes, "yes", "y", false, "Enable without asking")
	var limit int
	runs := &cobra.Command{
		Use:   "runs <name>",
		Short: "Show a job's recent runs",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return workersRuns(cmd, g, args[0], limit) },
	}
	runs.Flags().IntVarP(&limit, "limit", "n", 10, "How many runs to show")
	var force bool
	undo := &cobra.Command{
		Use:   "undo <run-id>",
		Short: "Put back the files a run changed, as they were before it",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return workersUndo(cmd, g, args[0], force) },
	}
	undo.Flags().BoolVarP(&force, "force", "f", false, "Overwrite files changed since the run")
	var anyway bool
	move := &cobra.Command{
		Use:   "move <name>",
		Short: "Move a job to your organization's service, saying first what can't run there",
		Long: `Moves a job in one of your organization's workspaces to its service
(runs_on: service), which runs it at its times whether or not this computer
is on. First it says what keeps the job from running there as it runs here
(a local MCP server, a file outside the workspace, a localhost address); it
moves only when nothing does, or with --anyway. Push it to the workspace's
main for the service to pick it up (a workspace with sync on does that).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return workersMove(cmd, g, args[0], anyway) },
	}
	move.Flags().BoolVar(&anyway, "anyway", false, "Move it even with problems")
	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "List the workspace's jobs", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return workersList(cmd, g) }},
		enable,
		&cobra.Command{Use: "disable <name>", Short: "Stop a job from running", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error { return workersDisable(cmd, g, args[0]) }},
		&cobra.Command{Use: "run <name>", Short: "Run an enabled job now and show what it does", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error { return workersRun(cmd, g, args[0]) }},
		runs,
		&cobra.Command{Use: "stop <name|run-id>", Short: "Stop a job's run that's going now", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error { return workersStop(cmd, g, args[0]) }},
		undo,
		move,
	)
	return cmd
}

// workersStop stops the running run of the worker named, or the run with
// that ID.
func workersStop(cmd *cobra.Command, g *globalFlags, which string) error {
	ops, err := openWorkers(cmd.Context(), g)
	if err != nil {
		return err
	}
	runs, err := ops.RunningWorkerRuns()
	if err != nil {
		return workerErr(err)
	}
	for _, r := range runs {
		if r.Worker == which || r.ID == which {
			if err := ops.StopWorkerRun(r.ID); err != nil {
				return workerErr(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("jobs.stopping", "name", r.Worker, "run", r.ID))
			return nil
		}
	}
	return withCode(exitUsage, fmt.Errorf("no run of %q is going in this workspace", which))
}

func workersUndo(cmd *cobra.Command, g *globalFlags, runID string, force bool) error {
	ops, err := openWorkers(cmd.Context(), g)
	if err != nil {
		return err
	}
	restored, err := ops.UndoWorkerRun(runID, force)
	if len(restored) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), i18n.T("jobs.restored", "files", strings.Join(restored, ", ")))
	}
	if errors.Is(err, api.ErrUndoConflict) || errors.Is(err, api.ErrNothingToUndo) {
		return withCode(exitUsage, err)
	}
	return err
}

func workersList(cmd *cobra.Command, g *globalFlags) error {
	ops, err := openWorkers(cmd.Context(), g)
	if err != nil {
		return err
	}
	list, err := ops.ListWorkers()
	if err != nil {
		return workerErr(err)
	}
	out := cmd.OutOrStdout()
	if len(list) == 0 {
		fmt.Fprintln(out, i18n.T("jobs.none"))
		return nil
	}
	for _, w := range list {
		next := ""
		if !w.Next.IsZero() {
			next = i18n.T("jobs.next", "time", w.Next.Local().Format("Mon Jan 2 15:04"))
		}
		fmt.Fprintf(out, "%-20s %-9s %s (%s)%s\n", w.Name, w.State, w.Schedule, w.Cron, next)
		if w.Description != "" {
			fmt.Fprintf(out, "    %s\n", w.Description)
		}
		for _, p := range w.Problems {
			fmt.Fprintf(out, "    ! %s\n", p)
		}
	}
	return nil
}

// describeWorker prints what enabling a worker approves.
func describeWorker(out io.Writer, w api.WorkerInfo) {
	fmt.Fprintf(out, "%s  (%s)\n", w.Name, w.Path)
	if w.Description != "" {
		fmt.Fprintf(out, "  %s\n", w.Description)
	}
	fmt.Fprintln(out, i18n.T("jobs.describe_schedule", "schedule", w.Schedule, "cron", w.Cron, "timezone", w.Timezone))
	if w.Agent != "" {
		fmt.Fprintln(out, i18n.T("jobs.describe_agent", "value", w.Agent))
	}
	if w.Model != "" {
		fmt.Fprintln(out, i18n.T("jobs.describe_model", "value", w.Model))
	}
	perms := i18n.T("jobs.no_permissions")
	if len(w.Permissions) > 0 {
		perms = strings.Join(w.Permissions, ", ")
	}
	fmt.Fprintln(out, i18n.T("jobs.describe_may", "value", perms))
	fmt.Fprintln(out, i18n.T("jobs.describe_limits", "value", limitsText(w.Limits)))
	for _, p := range w.Problems {
		fmt.Fprintf(out, "  ! %s\n", p)
	}
	fmt.Fprintln(out, i18n.T("jobs.describe_content", "value", w.Hash))
}

// limitsText says a job's limits, each 0 (none) as "no limit" rather than
// a limit of nothing (N26 cli-2#24).
func limitsText(l api.Limits) string {
	turns, cost, timeout := i18n.T("jobs.no_turn_limit"), i18n.T("jobs.no_cost_limit"), i18n.T("jobs.no_time_limit")
	if l.MaxTurns > 0 {
		turns = i18n.T("jobs.model_calls", "count", l.MaxTurns)
	}
	if l.MaxCostUSD > 0 {
		cost = fmt.Sprintf("$%.2f", l.MaxCostUSD)
	}
	if l.Timeout > 0 {
		timeout = l.Timeout.String()
	}
	return turns + ", " + cost + ", " + timeout
}

func findWorker(list []api.WorkerInfo, name string) (api.WorkerInfo, bool) {
	for _, w := range list {
		if w.Name == name {
			return w, true
		}
	}
	return api.WorkerInfo{}, false
}

func workersEnable(cmd *cobra.Command, g *globalFlags, name string, yes bool) error {
	ops, err := openWorkers(cmd.Context(), g)
	if err != nil {
		return err
	}
	list, err := ops.ListWorkers()
	if err != nil {
		return workerErr(err)
	}
	w, found := findWorker(list, name)
	if !found {
		return withCode(exitUsage, fmt.Errorf("no job %q (see 'blitz jobs')", name))
	}
	if w.State == api.StateInvalid {
		describeWorker(cmd.OutOrStdout(), w)
		return withCode(exitUsage, fmt.Errorf("job %q can't be enabled until WORKER.md is fixed", name))
	}
	out := cmd.OutOrStdout()
	describeWorker(out, w)
	fmt.Fprintf(out, "\n%s\n", i18n.T("jobs.read_first", "path", w.Path))
	if !yes {
		fmt.Fprint(out, i18n.T("jobs.confirm"))
		answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return errors.New("not enabled")
		}
	}
	// The hash shown is the one enabled: an edit since fails.
	on, err := ops.EnableWorker(name, w.Hash)
	if err != nil {
		return workerErr(err)
	}
	next := i18n.T("jobs.when_service_runs")
	if !on.Next.IsZero() {
		next = on.Next.Local().Format("Mon Jan 2 15:04")
	}
	fmt.Fprintln(out, i18n.T("jobs.enabled", "name", name, "next", next))
	if !loginitem.Installed() { // a service started on demand stops when idle
		fmt.Fprintln(out, i18n.T("jobs.service_hint"))
	}
	return nil
}

func workersDisable(cmd *cobra.Command, g *globalFlags, name string) error {
	ops, err := openWorkers(cmd.Context(), g)
	if err != nil {
		return err
	}
	if _, err := ops.DisableWorker(name); err != nil {
		return workerErr(err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), i18n.T("jobs.disabled", "name", name))
	return nil
}

func workersRun(cmd *cobra.Command, g *globalFlags, name string) error {
	ops, err := openWorkers(cmd.Context(), g)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	printer := tui.NewPrinter(tui.PrinterOptions{Out: out})
	run, err := ops.RunWorker(cmd.Context(), name, printer.Handle)
	if err != nil {
		return workerErr(err)
	}
	fmt.Fprintln(out)
	printRun(out, run)
	if run.Status != api.RunSucceeded {
		return withCode(exitFailure, fmt.Errorf("the run %s", run.Status))
	}
	return nil
}

func workersRuns(cmd *cobra.Command, g *globalFlags, name string, limit int) error {
	ops, err := openWorkers(cmd.Context(), g)
	if err != nil {
		return err
	}
	runs, err := ops.WorkerRuns(name, limit)
	if err != nil {
		return workerErr(err)
	}
	if len(runs) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), i18n.T("jobs.no_runs", "name", name))
	}
	for _, r := range runs {
		printRun(cmd.OutOrStdout(), r)
	}
	return nil
}

func printRun(out io.Writer, r api.Run) {
	how := i18n.T("jobs.scheduled")
	if r.Manual {
		how = i18n.T("jobs.manual")
	}
	fmt.Fprintln(out, i18n.T("jobs.run_line", "time", r.Started.Local().Format("2006-01-02 15:04"), "status", fmt.Sprintf("%-9s", r.Status), "how", how,
		"duration", r.Duration.Round(time.Second), "cost", fmt.Sprintf("$%.4f", r.CostUSD), "calls", r.Calls, "run", r.ID, "session", r.SessionID))
	if len(r.Files) > 0 {
		fmt.Fprintln(out, i18n.T("jobs.run_changed", "files", strings.Join(r.Files, ", "), "run", r.ID))
	}
	for _, f := range r.Refusals {
		fmt.Fprintln(out, i18n.T("jobs.run_refused", "detail", f.Detail, "tool", f.Tool))
	}
	if r.Error != "" {
		fmt.Fprintf(out, "    %s\n", r.Error)
	}
}

// workerErr gives worker errors the usage exit code where the request,
// not the system, was at fault.
func workerErr(err error) error {
	for _, e := range []error{api.ErrUnknownWorker, api.ErrWorkerNotEnabled, api.ErrRunInProgress, api.ErrWorkersDisabled, api.ErrHashMismatch} {
		if errors.Is(err, e) {
			return withCode(exitUsage, err)
		}
	}
	return err
}

// workersMove moves a job to the organization's service (spec_053 EN-40,
// EN-41), saying what keeps it from running there.
func workersMove(cmd *cobra.Command, g *globalFlags, name string, anyway bool) error {
	ops, err := openWorkers(cmd.Context(), g)
	if err != nil {
		return err
	}
	problems, moved, err := ops.MoveToService(cmd.Context(), name, anyway)
	if err != nil {
		return workerErr(err)
	}
	out := cmd.OutOrStdout()
	for _, p := range problems {
		fmt.Fprintln(out, "! "+p)
	}
	if !moved {
		return withCode(exitFailure, fmt.Errorf("%s stays on this computer: fix what's above, or move it with --anyway", name))
	}
	fmt.Fprintln(out, i18n.T("jobs.moved", "name", name))
	return nil
}
