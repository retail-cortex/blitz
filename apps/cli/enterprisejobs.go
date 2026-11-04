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
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/retail-cortex/blitz/pkg/client"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// missingText says why the service hasn't got the job name, and what to
// do, as Studio does.
func missingText(name string, m *client.ServiceJobMissing) string {
	switch m.Reason {
	case "outside":
		return fmt.Sprintf("the service runs jobs from %s; %s's WORKER.md is somewhere else, so the service doesn't see it: move it there", m.Detail, name)
	case "not_pushed":
		return fmt.Sprintf("%s's changes aren't on the workspace's main yet, so the service doesn't have them. Put them there: blitz enterprise jobs push %s", name, name)
	case "saving":
		switch m.SyncState {
		case "offline":
			return fmt.Sprintf("sync can't reach the organization's service, so %s waits on this computer; it goes when the service is back", name)
		case "signed_out":
			return fmt.Sprintf("sign in to your organization to put %s on its service: blitz enterprise login", name)
		case "conflicts":
			return fmt.Sprintf("sync kept both versions of a file in this workspace; settle them and %s goes to the service", name)
		case "refused":
			return fmt.Sprintf("the organization's service turned down this workspace's changes, so %s isn't on its main: %s", name, m.Detail)
		}
		return fmt.Sprintf("sync is putting %s on the organization's service: it goes a few seconds after the last change, between agent turns. Try again shortly", name)
	case "not_service":
		return fmt.Sprintf("the service read %s from main but doesn't run it: %s", name, m.Detail)
	}
	if m.Detail != "" {
		return fmt.Sprintf("the service hasn't read %s from the workspace's main yet: %s", name, m.Detail)
	}
	return fmt.Sprintf("the service hasn't read %s from the workspace's main yet. Try again shortly", name)
}

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
			// A job of this workspace's: say why the service hasn't got it.
			if dir, err := os.Getwd(); err == nil && workspace == "" {
				if _, err := os.Stat(filepath.Join(dir, ".agents", "workers", name, "WORKER.md")); err == nil {
					if m, err := s.WhyNotServiceJob(cmd.Context(), dir, name); err == nil && m != nil {
						return nil, client.ServiceJob{}, withCode(exitUsage, errors.New(missingText(name, m)))
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
				switch {
				case r.Branch != "" && r.Merged:
					fmt.Fprintln(out, "    on main (branch: "+r.Branch+")")
				case r.Branch != "":
					fmt.Fprintf(out, "    on its branch only, %s: blitz enterprise jobs take %s %s\n", r.Branch, j.Name, r.ID)
					if r.MergeNote != "" {
						fmt.Fprintln(out, "      "+r.MergeNote)
					}
				}
				if r.Artifacts != "" && r.Status != "queued" {
					fmt.Fprintln(out, "    files: blitz enterprise jobs files "+r.Artifacts)
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
			s, err := settings(cmd)
			if err != nil {
				return err
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			m, err := s.PushServiceJob(cmd.Context(), dir, args[0])
			if err != nil {
				return err
			}
			if m != nil {
				return withCode(exitUsage, errors.New(missingText(args[0], m)))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ %s is on main and the service has it. Run it: blitz enterprise jobs run %s\n", args[0], args[0])
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
					fmt.Fprintf(cmd.OutOrStdout(), "Every run of %s that changed something is on main already.\n", j.Name)
					return nil
				}
			}
			r, err := s.TakeServiceRun(cmd.Context(), j.Dir, j.ID, id)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ The changes of %s's run %s are on main (%s); this folder takes them in.\n", j.Name, r.ID, r.Branch)
			return nil
		},
	}
	cmd.AddCommand(runs, push, runNow, take, switchTo(true), switchTo(false), files, pending, answer(true), answer(false))
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
			sock, err := ensureService(cmd.Context())
			if err != nil {
				return err
			}
			u, err := client.AttachSettings(sock).OrgUsage(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, period := range []struct {
				name string
				rows []*pb.OrgUsed
			}{{"Today", u.Today}, {"This month", u.Month}} {
				fmt.Fprintln(out, period.name+":")
				if len(period.rows) == 0 {
					fmt.Fprintln(out, "  nothing yet")
					continue
				}
				tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				for _, r := range period.rows {
					fmt.Fprintf(tw, "  %s\t%d requests\t%d in\t%d out\t$%.4f\n", r.Model, r.Requests, r.InputTokens, r.OutputTokens, r.CostUsd)
				}
				_ = tw.Flush()
			}
			if len(u.Limits) > 0 {
				fmt.Fprintln(out, "Limits:")
			}
			for _, l := range u.Limits {
				on := "every model"
				if l.Model != "" {
					on = l.Model
				}
				resets := ""
				if l.Resets != nil {
					resets = ", resets " + l.Resets.AsTime().Local().Format("Jan 2 15:04")
				}
				if l.Unit == "tokens" {
					limit := max(l.Tokens, l.InputTokens, l.OutputTokens)
					used := l.Used.GetInputTokens() + l.Used.GetOutputTokens()
					fmt.Fprintf(out, "  %s %s quota on %s: %d of %d tokens used%s\n", l.ScopeKind, l.Period, on, used, limit, resets)
					continue
				}
				fmt.Fprintf(out, "  %s %s budget on %s: $%.2f of $%.2f used ($%.2f left)%s\n", l.ScopeKind, l.Period, on, l.Used.GetCostUsd(), l.AmountUsd,
					max(0, l.AmountUsd-l.Used.GetCostUsd()), resets)
			}
			for _, r := range u.RateLimits {
				fmt.Fprintf(out, "  %s: %s\n", r.Model, rateText(r))
			}
			if u.PromptLogs {
				fmt.Fprintln(out, "Your organization keeps what's sent to its models (full prompt logs include your requests).")
			}
			return nil
		},
	}
}

// rateText is a model's rate limits, in words.
func rateText(r *pb.OrgRateLimit) string {
	var parts []string
	for _, p := range []struct {
		n    int32
		what string
	}{{r.PersonRpm, "requests a minute for you"}, {r.PersonTpm, "tokens a minute for you"}, {r.Rpm, "requests a minute for everyone"}, {r.Tpm, "tokens a minute for everyone"}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.what))
		}
	}
	return strings.Join(parts, ", ")
}

// newEnterprisePublishCommand is blitz enterprise publish: a workspace's
// items to the organization's catalog, as a review (spec_053 EN-36).
func newEnterprisePublishCommand() *cobra.Command {
	var dir, lock, title string
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
			sock, err := ensureService(cmd.Context())
			if err != nil {
				return err
			}
			s := client.AttachSettings(sock)
			abs, err := filepath.Abs(dir)
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
					fmt.Fprintln(out, "Nothing to publish: this workspace's .agents folder has no agents, skills, rules, guardrails, commands or connectors.")
				}
				for _, it := range items {
					line := fmt.Sprintf("%s/%s  (%s", it.Kind, it.Name, it.Lock)
					if len(it.Groups) > 0 {
						line += ", for " + strings.Join(it.Groups, ", ")
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
			fmt.Fprintf(out, "✓ In review: %s (%s). An Admin (you too) approves it in the admin console; the next version carries it.\n", rv.Title, strings.Join(rv.Paths, ", "))
			return nil
		},
	}
	cmd.Flags().StringVarP(&dir, "dir", "d", ".", "the workspace")
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
			sock, err := ensureService(cmd.Context())
			if err != nil {
				return err
			}
			list, err := client.AttachSettings(sock).CatalogReviews(cmd.Context(), state)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(out, "No reviews.")
			}
			for _, rv := range list {
				status := rv.State
				if rv.State == "approved" && rv.Version > 0 {
					status = fmt.Sprintf("published in version %d", rv.Version)
				}
				fmt.Fprintf(out, "%s  %s  by %s: %s\n", rv.Id, status, rv.ByEmail, rv.Title)
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
func newEnterpriseCatalogCommand() *cobra.Command {
	var dir, kind string
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "The agents, skills, rules… your organization gives this workspace",
		Long: `Lists the organization's catalog items this workspace gets, each locked
(its Admins change it, through a review) or default (the workspace may
have its own instead: "blitz enterprise catalog copy <kind>/<name>"
copies it into .agents to change).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sock, err := ensureService(cmd.Context())
			if err != nil {
				return err
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			res, err := client.AttachSettings(sock).OrgItems(cmd.Context(), abs, kind)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(res.Items) == 0 {
				fmt.Fprintln(out, "Your organization's catalog has nothing for you yet.")
				return nil
			}
			fmt.Fprintf(out, "Catalog version %d:\n", res.Version)
			for _, it := range res.Items {
				line := fmt.Sprintf("%s/%s  (%s", it.Kind, it.Name, it.Lock)
				if len(it.Groups) > 0 {
					line += ", for " + strings.Join(it.Groups, ", ")
				}
				line += ")"
				if it.Overridden {
					line += "  this workspace has its own"
				}
				fmt.Fprintln(out, line)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&dir, "dir", "d", ".", "the workspace")
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
			sock, err := ensureService(cmd.Context())
			if err != nil {
				return err
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			paths, err := client.AttachSettings(sock).CopyOrgItem(cmd.Context(), abs, k, name)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Copied to %s. This workspace's copy now takes the place of the organization's.\n", strings.Join(paths, ", "))
			return nil
		},
	}
	copyCmd.Flags().StringVarP(&dir, "dir", "d", ".", "the workspace")
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
changes it: --email on|off, --webhook <https address> ("" for none; a
Slack or Teams incoming webhook shows the notice as it is), and
--every-run on|off (off: failures and runs waiting for your answer only).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sock, err := ensureService(cmd.Context())
			if err != nil {
				return err
			}
			s := client.AttachSettings(sock)
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
				fmt.Fprintln(out, "Email     off")
			case n.EmailAvailable:
				fmt.Fprintln(out, "Email     on")
			default:
				fmt.Fprintln(out, "Email     on, but your organization's service sends no email: add a webhook to hear of runs")
			}
			hook := n.Webhook
			if hook == "" {
				hook = "none"
			}
			fmt.Fprintln(out, "Webhook   "+hook)
			if n.EveryRun {
				fmt.Fprintln(out, "For       every run")
			} else {
				fmt.Fprintln(out, "For       failures and runs waiting for your answer")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&email, "email", true, "email them to you")
	cmd.Flags().StringVar(&webhook, "webhook", "", `an https address to POST them to ("" for none)`)
	cmd.Flags().BoolVar(&everyRun, "every-run", false, "every run's end, not only failures and runs waiting for your answer")
	return cmd
}
