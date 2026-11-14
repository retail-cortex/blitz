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
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/i18n"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// missingErr says why the service hasn't got the job name, and what to
// do, as Studio does. It's an error, so English like the CLI's others
// (spec_020 CLI-30).
func missingErr(name string, m *client.ServiceJobMissing) error {
	switch m.Reason {
	case "outside":
		return fmt.Errorf("the service runs jobs from %s; %s's WORKER.md is somewhere else, so the service doesn't see it: move it there", m.Detail, name)
	case "not_pushed":
		return fmt.Errorf("%s's changes aren't on the workspace's main yet, so the service doesn't have them. Put them there: blitz enterprise jobs push %s", name, name)
	case "saving":
		switch m.SyncState {
		case "offline":
			return fmt.Errorf("sync can't reach the organization's service, so %s waits on this computer; it goes when the service is back", name)
		case "signed_out":
			return fmt.Errorf("sign in to your organization to put %s on its service: blitz enterprise login", name)
		case "conflicts":
			return fmt.Errorf("sync kept both versions of a file in this workspace; settle them and %s goes to the service", name)
		case "refused":
			return fmt.Errorf("the organization's service turned down this workspace's changes, so %s isn't on its main: %s", name, m.Detail)
		}
		return fmt.Errorf("sync is putting %s on the organization's service: it goes a few seconds after the last change, between agent turns. Try again shortly", name)
	case "not_service":
		return fmt.Errorf("the service read %s from main but doesn't run it: %s", name, m.Detail)
	}
	if m.Detail != "" {
		return fmt.Errorf("the service hasn't read %s from the workspace's main yet: %s", name, m.Detail)
	}
	return fmt.Errorf("the service hasn't read %s from the workspace's main yet. Try again shortly", name)
}

// newEnterpriseJobsCommand is blitz enterprise jobs: the jobs your
// organization's service runs (spec_053 EN-42, EN-43), as Studio › Jobs
// and the status bar show them.
func newEnterpriseJobsCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "The jobs your organization's service runs: list, runs, run now, on or off, files, and answers",
		Long: `Lists the jobs your organization's service runs in your workspaces (those
whose WORKER.md says runs_on: service). A job is named by its name, with
--workspace <handle> when two workspaces have one of that name.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			jobs, err := s.ServiceJobs(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(jobs) == 0 {
				fmt.Fprintln(out, i18n.T("org.jobs_none"))
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, i18n.T("org.jobs_columns"))
			for _, j := range jobs {
				state, next := i18n.T("org.job_on"), "-"
				if !j.Enabled {
					state = i18n.T("org.job_off")
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
		s, err := orgSettings(cmd)
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
			// A job of this workspace's: say why the service hasn't got it.
			if dir, err := orgWorkspace(g); err == nil && workspace == "" {
				if _, err := os.Stat(filepath.Join(dir, ".agents", "workers", name, "WORKER.md")); err == nil {
					if m, err := s.WhyNotServiceJob(cmd.Context(), dir, name); err == nil && m != nil {
						return nil, client.ServiceJob{}, withCode(exitUsage, missingErr(name, m))
					}
				}
			}
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
				fmt.Fprintln(out, i18n.T("org.runs_none"))
			}
			for _, r := range list {
				fmt.Fprintf(out, "%s  %s  %s (%s)", r.Created.Local().Format("Jan 2 15:04"), r.ID, r.Status, r.Trigger)
				if r.CostUSD > 0 {
					fmt.Fprintf(out, "  $%.4f", r.CostUSD)
				}
				fmt.Fprintln(out)
				if r.Pending != nil {
					fmt.Fprintln(out, i18n.T("org.run_waits", "tool", r.Pending.Tool, "detail", r.Pending.Detail, "run", r.ID))
				}
				for _, line := range []string{r.Note, r.Output} {
					if line != "" {
						fmt.Fprintln(out, "    "+strings.ReplaceAll(line, "\n", "\n    "))
					}
				}
				switch {
				case r.Branch != "" && r.Merged:
					fmt.Fprintln(out, i18n.T("org.run_on_main", "branch", r.Branch))
				case r.Branch != "":
					fmt.Fprintln(out, i18n.T("org.run_on_branch", "branch", r.Branch, "job", j.Name, "run", r.ID))
					if r.MergeNote != "" {
						fmt.Fprintln(out, "      "+r.MergeNote)
					}
				}
				if r.Artifacts != "" && r.Status != "queued" {
					fmt.Fprintln(out, i18n.T("org.run_files", "urn", r.Artifacts))
				}
			}
			return nil
		},
	}
	push := &cobra.Command{
		Use:   "push <job>",
		Short: "Put a job of this workspace's on its main, for the service to run (a workspace sync doesn't keep in step)",
		Long: `Commits the job's folder (.agents/workers/<job>/) in the workspace you're
in, pushes the workspace to its main on your organization's service, and has
the service read it now.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			dir, err := orgWorkspace(g)
			if err != nil {
				return err
			}
			m, err := s.PushServiceJob(cmd.Context(), dir, args[0])
			if err != nil {
				return err
			}
			if m != nil {
				return withCode(exitUsage, missingErr(args[0], m))
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("org.job_pushed", "name", args[0]))
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
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("org.job_started", "name", j.Name, "run", r.ID, "status", r.Status))
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
				key := "org.job_switched_off"
				if on {
					key = "org.job_switched_on"
				}
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T(key, "name", j.Name))
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
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			list, err := s.ServiceRunFiles(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(out, i18n.T("org.files_none"))
			}
			for _, f := range list {
				if !save {
					fmt.Fprintln(out, i18n.T("org.file_size", "path", f.Path, "size", f.Size))
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
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			list, err := s.ServiceApprovals(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(out, i18n.T("org.pending_none"))
			}
			for _, r := range list {
				fmt.Fprintln(out, i18n.T("org.pending_run", "run", r.ID, "job", r.JobName, "workspace", r.Workspace, "time", r.Created.Local().Format(time.DateTime)))
				if r.Pending != nil {
					fmt.Fprintln(out, i18n.T("org.pending_asks", "tool", r.Pending.Tool, "detail", r.Pending.Detail))
				}
				if r.Output != "" {
					fmt.Fprintln(out, "    "+strings.ReplaceAll(r.Output, "\n", "\n    "))
				}
			}
			if len(list) > 0 {
				fmt.Fprintln(out, i18n.T("org.pending_answer"))
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
				s, err := orgSettings(cmd)
				if err != nil {
					return err
				}
				r, err := s.AnswerServiceRun(cmd.Context(), args[0], approve, note)
				if err != nil {
					return err
				}
				key := "org.answer_denied"
				if approve {
					key = "org.answer_approved"
				}
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T(key, "status", r.Status))
				return nil
			},
		}
		c.Flags().StringVar(&note, "note", "", "a note for the job's agent")
		return c
	}
	take := &cobra.Command{
		Use:   "take <job> [run]",
		Short: "Add a run's changes, kept on its own branch, to the workspace's main and this folder (its latest such run by default)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, j, err := find(cmd, args[0])
			if err != nil {
				return err
			}
			id := ""
			if len(args) == 2 {
				id = args[1]
			} else {
				list, err := s.ServiceJobRuns(cmd.Context(), j.Dir, j.Name)
				if err != nil {
					return err
				}
				for _, r := range list {
					if r.Branch != "" && !r.Merged {
						id = r.ID
						break
					}
				}
				if id == "" {
					fmt.Fprintln(cmd.OutOrStdout(), i18n.T("org.take_none", "name", j.Name))
					return nil
				}
			}
			r, err := s.TakeServiceRun(cmd.Context(), j.Dir, j.ID, id)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("org.taken", "name", j.Name, "run", r.ID, "branch", r.Branch))
			return nil
		},
	}
	watch := &cobra.Command{
		Use:   "watch",
		Short: "Follow the runs going now on the service, as they start, change and end (Ctrl+C stops)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			out := cmd.OutOrStdout()
			return s.WatchServiceRuns(ctx, func(runs []client.GoingServiceRun) { printGoingRuns(out, runs) })
		},
	}
	cmd.AddCommand(runs, push, runNow, take, switchTo(true), switchTo(false), files, pending, answer(true), answer(false), watch)
	return cmd
}

// newEnterpriseUsageCommand is blitz enterprise usage: the person's own
// use of the organization's models, and their limits (spec_053 EN-24).
func newEnterpriseUsageCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "Your use of your organization's models today and this month, and what's left of each limit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			u, err := s.OrgUsage(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, period := range []struct {
				name string
				rows []*pb.OrgUsed
			}{{i18n.T("org.usage_today"), u.Today}, {i18n.T("org.usage_month"), u.Month}} {
				fmt.Fprintln(out, period.name)
				if len(period.rows) == 0 {
					fmt.Fprintln(out, i18n.T("org.usage_nothing"))
					continue
				}
				tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				for _, r := range period.rows {
					fmt.Fprintln(tw, i18n.T("org.usage_row", "model", r.Model, "requests", r.Requests, "input", r.InputTokens, "output", r.OutputTokens, "cost", fmt.Sprintf("$%.4f", r.CostUsd)))
				}
				_ = tw.Flush()
			}
			if len(u.Limits) > 0 {
				fmt.Fprintln(out, i18n.T("org.usage_limits"))
			}
			for _, l := range u.Limits {
				on := i18n.T("org.usage_every_model")
				if l.Model != "" {
					on = l.Model
				}
				resets := ""
				if l.Resets != nil {
					resets = i18n.T("org.usage_resets", "time", l.Resets.AsTime().Local().Format("Jan 2 15:04"))
				}
				if l.Unit == "tokens" {
					// Each cap as the gateway counts it: the total against
					// input and output together, the others alone.
					in, outTok := l.Used.GetInputTokens(), l.Used.GetOutputTokens()
					for _, c := range []struct {
						key         string
						limit, used int64
					}{{"org.usage_quota", l.Tokens, in + outTok}, {"org.usage_quota_input", l.InputTokens, in}, {"org.usage_quota_output", l.OutputTokens, outTok}} {
						if c.limit > 0 {
							fmt.Fprintln(out, i18n.T(c.key, "scope", l.ScopeKind, "period", l.Period, "on", on, "used", c.used, "limit", c.limit,
								"left", max(0, c.limit-c.used), "resets", resets))
						}
					}
					continue
				}
				fmt.Fprintln(out, i18n.T("org.usage_budget", "scope", l.ScopeKind, "period", l.Period, "on", on, "used", fmt.Sprintf("$%.2f", l.Used.GetCostUsd()),
					"limit", fmt.Sprintf("$%.2f", l.AmountUsd), "left", fmt.Sprintf("$%.2f", max(0, l.AmountUsd-l.Used.GetCostUsd())), "resets", resets))
			}
			for _, r := range u.RateLimits {
				fmt.Fprintf(out, "  %s: %s\n", r.Model, rateText(r))
			}
			if u.PromptLogs {
				fmt.Fprintln(out, i18n.T("org.usage_prompt_logs"))
			}
			return nil
		},
	}
}

// rateText is a model's rate limits, in words.
func rateText(r *pb.OrgRateLimit) string {
	var parts []string
	for _, p := range []struct {
		n   int32
		key string
	}{{r.PersonRpm, "org.rate_person_rpm"}, {r.PersonTpm, "org.rate_person_tpm"}, {r.Rpm, "org.rate_rpm"}, {r.Tpm, "org.rate_tpm"}} {
		if p.n > 0 {
			parts = append(parts, i18n.T(p.key, "count", p.n))
		}
	}
	return strings.Join(parts, ", ")
}

// newEnterprisePublishCommand is blitz enterprise publish: a workspace's
// items to the organization's catalog, as a review (spec_053 EN-36).
func newEnterprisePublishCommand(g *globalFlags) *cobra.Command {
	var lock, title string
	var groups []string
	cmd := &cobra.Command{
		Use:   "publish [<kind>/<name>...]",
		Short: "Publish a workspace's agents, skills, rules… to your organization's catalog, for an Admin (you too) to approve",
		Long: `Publishes items from a workspace's .agents folder (agent/<name>, skill/<name>,
rule/<name>, guardrail/<name>, command/<name>, connector/<name>) to your
organization's catalog as a review: an Admin (you too) approves it in the admin
console, and the next version carries it. With none named, lists what
can be published and how each applies now. For the organization's Admins.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			abs, err := orgWorkspace(g)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				items, err := s.Publishable(cmd.Context(), abs)
				if err != nil {
					return err
				}
				if len(items) == 0 {
					fmt.Fprintln(out, i18n.T("org.publish_none"))
				}
				for _, it := range items {
					line := fmt.Sprintf("%s/%s  (%s", it.Kind, it.Name, it.Lock)
					if len(it.Groups) > 0 {
						line += i18n.T("org.item_for", "groups", strings.Join(it.Groups, ", "))
					}
					fmt.Fprintln(out, line+")")
					for _, p := range it.Problems {
						fmt.Fprintln(out, "    ! "+p)
					}
				}
				return nil
			}
			var items []*pb.PublishItem
			for _, a := range args {
				kind, name, ok := strings.Cut(a, "/")
				if !ok {
					return withCode(exitUsage, fmt.Errorf("%q: name it as <kind>/<name>, like agent/writer", a))
				}
				items = append(items, &pb.PublishItem{Kind: kind, Name: name, Lock: lock, Groups: groups})
			}
			rv, err := s.PublishToOrganization(cmd.Context(), abs, title, items)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, i18n.T("org.published", "title", rv.Title, "paths", strings.Join(rv.Paths, ", ")))
			return nil
		},
	}
	cmd.Flags().StringVar(&lock, "lock", "default", "locked (nobody changes or switches it off) or default (a workspace or person may)")
	cmd.Flags().StringSliceVar(&groups, "groups", nil, "who it's for (everyone when none)")
	cmd.Flags().StringVar(&title, "title", "", "the review's title (default: what's published)")
	return cmd
}

// newEnterpriseReviewsCommand is blitz enterprise reviews: the catalog's
// reviews (EN-36).
func newEnterpriseReviewsCommand() *cobra.Command {
	var state string
	cmd := &cobra.Command{
		Use:   "reviews",
		Short: "Changes to your organization's catalog: waiting, approved or rejected",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			list, err := s.CatalogReviews(cmd.Context(), state)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(out, i18n.T("org.reviews_none"))
			}
			for _, rv := range list {
				status := rv.State
				if rv.State == "approved" && rv.Version > 0 {
					status = i18n.T("org.review_published", "version", rv.Version)
				}
				fmt.Fprintln(out, i18n.T("org.review_line", "id", rv.Id, "status", status, "email", rv.ByEmail, "title", rv.Title))
				for _, p := range rv.Problems {
					fmt.Fprintln(out, "    ! "+p)
				}
				if rv.Note != "" {
					fmt.Fprintln(out, "    "+rv.Note)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "open, approved or rejected")
	return cmd
}

// newEnterpriseCatalogCommand is blitz enterprise catalog: the
// organization's items a workspace gets, and copying a default one in to
// change (spec_053 EN-34).
func newEnterpriseCatalogCommand(g *globalFlags) *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "The agents, skills, rules… your organization gives this workspace",
		Long: `Lists the organization's catalog items this workspace gets, each locked
(its Admins change it, through a review) or default (the workspace may
have its own instead: "blitz enterprise catalog copy <kind>/<name>"
copies it into .agents to change).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			abs, err := orgWorkspace(g)
			if err != nil {
				return err
			}
			res, err := s.OrgItems(cmd.Context(), abs, kind)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(res.Items) == 0 {
				fmt.Fprintln(out, i18n.T("org.catalog_none"))
				return nil
			}
			fmt.Fprintln(out, i18n.T("org.catalog_version", "version", res.Version))
			for _, it := range res.Items {
				line := fmt.Sprintf("%s/%s  (%s", it.Kind, it.Name, it.Lock)
				if len(it.Groups) > 0 {
					line += i18n.T("org.item_for", "groups", strings.Join(it.Groups, ", "))
				}
				line += ")"
				if it.Overridden {
					line += i18n.T("org.item_overridden")
				}
				fmt.Fprintln(out, line)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "one kind: agent, skill, rule, guardrail, command, mcp or connector")
	copyCmd := &cobra.Command{
		Use:   "copy <kind>/<name>",
		Short: "Copy one of the organization's default items into this workspace, to change there",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, name, ok := strings.Cut(args[0], "/")
			if !ok {
				return withCode(exitUsage, fmt.Errorf("%q: name it as <kind>/<name>, like agent/writer", args[0]))
			}
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			abs, err := orgWorkspace(g)
			if err != nil {
				return err
			}
			paths, err := s.CopyOrgItem(cmd.Context(), abs, k, name)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("org.catalog_copied", "paths", strings.Join(paths, ", ")))
			return nil
		},
	}
	cmd.AddCommand(copyCmd)
	return cmd
}

// newEnterpriseNoticesCommand is blitz enterprise notices: how the person
// hears of their jobs' runs on the service (the enterprise spec's JB-31).
func newEnterpriseNoticesCommand() *cobra.Command {
	var email, everyRun bool
	var webhook string
	cmd := &cobra.Command{
		Use:   "notices",
		Short: "How you hear of your jobs' runs on your organization's service: email, a webhook, every run or not",
		Long: `Shows how you hear of your jobs' runs on your organization's service, or
changes it: --email or --email=false, --webhook <https address> ("" for
none; a Slack or Teams incoming webhook shows the notice as it is), and
--every-run or --every-run=false (off: failures and runs waiting for your
answer only). Turning one off takes the =false: "--email off" is refused.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			n, err := s.OrgNotices(cmd.Context())
			if err != nil {
				return err
			}
			f := cmd.Flags()
			if f.Changed("email") || f.Changed("webhook") || f.Changed("every-run") {
				if f.Changed("email") {
					n.Email = email
				}
				if f.Changed("webhook") {
					n.Webhook = webhook
				}
				if f.Changed("every-run") {
					n.EveryRun = everyRun
				}
				if n, err = s.SetOrgNotices(cmd.Context(), n); err != nil {
					return err
				}
			}
			out := cmd.OutOrStdout()
			switch {
			case !n.Email:
				fmt.Fprintln(out, i18n.T("org.notices_email_off"))
			case n.EmailAvailable:
				fmt.Fprintln(out, i18n.T("org.notices_email_on"))
			default:
				fmt.Fprintln(out, i18n.T("org.notices_email_unsent"))
			}
			hook := n.Webhook
			if hook == "" {
				hook = i18n.T("org.notices_webhook_none")
			}
			fmt.Fprintln(out, i18n.T("org.notices_webhook", "hook", hook))
			if n.EveryRun {
				fmt.Fprintln(out, i18n.T("org.notices_every_run"))
			} else {
				fmt.Fprintln(out, i18n.T("org.notices_failures"))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&email, "email", true, "email them to you")
	cmd.Flags().StringVar(&webhook, "webhook", "", `an https address to POST them to ("" for none)`)
	cmd.Flags().BoolVar(&everyRun, "every-run", false, "every run's end, not only failures and runs waiting for your answer")
	return cmd
}

// printGoingRuns writes the runs going now on the service, as the
// Processes view shows them (EN-42), each time they change.
func printGoingRuns(w io.Writer, runs []client.GoingServiceRun) {
	fmt.Fprintf(w, "\n%s  %s\n", time.Now().Format("15:04:05"), i18n.N("org.watch_going", len(runs)))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, g := range runs {
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", g.Workspace, g.JobName, g.Run.ID, g.Run.Status, g.Run.Created.Local().Format("15:04"))
	}
	tw.Flush()
}
