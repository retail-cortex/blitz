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
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/retail-cortex/blitz/pkg/client"
)

// newEnterpriseJobsCommand is blitz enterprise jobs: the jobs your
// organization's service runs (spec_053 EN-42, EN-43), as Studio › Jobs
// and the status bar show them.
func newEnterpriseJobsCommand() *cobra.Command {
	settings := func(cmd *cobra.Command) (*client.Settings, error) {
		sock, err := ensureService(cmd.Context())
		if err != nil {
			return nil, err
		}
		return client.AttachSettings(sock), nil
	}
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "The jobs your organization's service runs: list, runs, run now, on or off, files, and answers",
		Long: `Lists the jobs your organization's service runs in your workspaces (those
whose WORKER.md says runs_on: service). A job is named by its name, with
--workspace <handle> when two workspaces have one of that name.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := settings(cmd)
			if err != nil {
				return err
			}
			jobs, err := s.ServiceJobs(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(jobs) == 0 {
				fmt.Fprintln(out, "Your organization's service runs no jobs in your workspaces. Set runs_on: service in a job's WORKER.md and push it to main.")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "WORKSPACE\tJOB\tSCHEDULE\tSTATE\tNEXT")
			for _, j := range jobs {
				state, next := "on", "-"
				if !j.Enabled {
					state = "off"
				}
				if !j.NextRun.IsZero() {
					next = j.NextRun.Local().Format("Jan 2 15:04")
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", j.Workspace, j.Name, j.Schedule, state, next)
			}
			return tw.Flush()
		},
	}
	var workspace string
	cmd.PersistentFlags().StringVar(&workspace, "workspace", "", "the job's workspace (its handle), when the name isn't enough")
	find := func(cmd *cobra.Command, name string) (*client.Settings, client.ServiceJob, error) {
		s, err := settings(cmd)
		if err != nil {
			return nil, client.ServiceJob{}, err
		}
		jobs, err := s.ServiceJobs(cmd.Context())
		if err != nil {
			return nil, client.ServiceJob{}, err
		}
		var found []client.ServiceJob
		for _, j := range jobs {
			if j.Name == name && (workspace == "" || j.Workspace == workspace) {
				found = append(found, j)
			}
		}
		switch len(found) {
		case 0:
			return nil, client.ServiceJob{}, withCode(exitUsage, fmt.Errorf("your organization's service runs no job %q in your workspaces", name))
		case 1:
			return s, found[0], nil
		}
		var in []string
		for _, j := range found {
			in = append(in, j.Workspace)
		}
		return nil, client.ServiceJob{}, withCode(exitUsage, fmt.Errorf("%q is in %s: say which with --workspace", name, strings.Join(in, ", ")))
	}
	runs := &cobra.Command{
		Use:   "runs <job>",
		Short: "A job's latest runs on the service: how each went, its branch, its files' name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, j, err := find(cmd, args[0])
			if err != nil {
				return err
			}
			list, err := s.ServiceJobRuns(cmd.Context(), j.Dir, j.Name)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(out, "It hasn't run yet.")
			}
			for _, r := range list {
				fmt.Fprintf(out, "%s  %s  %s (%s)", r.Created.Local().Format("Jan 2 15:04"), r.ID, r.Status, r.Trigger)
				if r.CostUSD > 0 {
					fmt.Fprintf(out, "  $%.4f", r.CostUSD)
				}
				fmt.Fprintln(out)
				if r.Pending != nil {
					fmt.Fprintf(out, "    waits for an answer: %s %s (blitz enterprise jobs approve|deny %s)\n", r.Pending.Tool, r.Pending.Detail, r.ID)
				}
				for _, line := range []string{r.Note, r.Output} {
					if line != "" {
						fmt.Fprintln(out, "    "+strings.ReplaceAll(line, "\n", "\n    "))
					}
				}
				if r.Branch != "" {
					fmt.Fprintln(out, "    branch: "+r.Branch)
				}
				if r.Artifacts != "" && r.Status != "queued" {
					fmt.Fprintln(out, "    files: blitz enterprise jobs files "+r.Artifacts)
				}
			}
			return nil
		},
	}
	runNow := &cobra.Command{
		Use:   "run <job>",
		Short: "Start a run of a job on the service now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, j, err := find(cmd, args[0])
			if err != nil {
				return err
			}
			r, err := s.RunServiceJob(cmd.Context(), j.ID)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ %s: run %s, %s. See it: blitz enterprise jobs runs %s\n", j.Name, r.ID, r.Status, j.Name)
			return nil
		},
	}
	switchTo := func(on bool) *cobra.Command {
		use, short := "off <job>", "Stop a job running at its times on the service"
		if on {
			use, short = "on <job>", "Run a job at its times on the service again"
		}
		return &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				s, j, err := find(cmd, args[0])
				if err != nil {
					return err
				}
				if err := s.SetServiceJobEnabled(cmd.Context(), j.ID, on); err != nil {
					return err
				}
				state := "off"
				if on {
					state = "on"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "✓ %s is %s.\n", j.Name, state)
				return nil
			},
		}
	}
	var save bool
	files := &cobra.Command{
		Use:   "files <urn:blitz:artifacts:…>",
		Short: "The files a run left (its out/ folder and its log); --save saves them in Downloads",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := settings(cmd)
			if err != nil {
				return err
			}
			list, err := s.ServiceRunFiles(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(out, "It left no files.")
			}
			for _, f := range list {
				if !save {
					fmt.Fprintf(out, "%s  %d bytes\n", f.Path, f.Size)
					continue
				}
				path, err := s.SaveServiceRunFile(cmd.Context(), f.URN)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "✓ %s → %s\n", f.Path, path)
			}
			return nil
		},
	}
	files.Flags().BoolVar(&save, "save", false, "save each in your Downloads folder")
	pending := &cobra.Command{
		Use:   "pending",
		Short: "The runs waiting for your answer: what each asks to do",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := settings(cmd)
			if err != nil {
				return err
			}
			list, err := s.ServiceApprovals(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(out, "Nothing waits for your answer.")
			}
			for _, r := range list {
				fmt.Fprintf(out, "%s  %s in %s, since %s\n", r.ID, r.JobName, r.Workspace, r.Created.Local().Format(time.DateTime))
				if r.Pending != nil {
					fmt.Fprintf(out, "    asks to %s: %s\n", r.Pending.Tool, r.Pending.Detail)
				}
				if r.Output != "" {
					fmt.Fprintln(out, "    "+strings.ReplaceAll(r.Output, "\n", "\n    "))
				}
			}
			if len(list) > 0 {
				fmt.Fprintln(out, "Answer: blitz enterprise jobs approve|deny <run> [--note …]")
			}
			return nil
		},
	}
	var note string
	answer := func(approve bool) *cobra.Command {
		use, short := "deny <run>", "Deny what a paused run asks for; it goes on without"
		if approve {
			use, short = "approve <run>", "Approve what a paused run asks for; it goes on and does it, once"
		}
		c := &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				s, err := settings(cmd)
				if err != nil {
					return err
				}
				r, err := s.AnswerServiceRun(cmd.Context(), args[0], approve, note)
				if err != nil {
					return err
				}
				word := "Denied"
				if approve {
					word = "Approved"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "✓ %s: the run goes on (%s).\n", word, r.Status)
				return nil
			},
		}
		c.Flags().StringVar(&note, "note", "", "a note for the job's agent")
		return c
	}
	cmd.AddCommand(runs, runNow, switchTo(true), switchTo(false), files, pending, answer(true), answer(false))
	return cmd
}
