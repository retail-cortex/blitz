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

// newEnterprisePolicyCommand is blitz enterprise policy: the person's
// effective policy, each setting with who decided it, and the rules and
// guardrails it applies to them (spec_053 EN-39); "catalog" as before.
func newEnterprisePolicyCommand(g *globalFlags) *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:     "policy",
		Aliases: []string{"catalog"},
		Short:   "What your organization's policy lets you do, who decided each setting, and its rules and guardrails for you",
		Long: `Lists your organization's policy as it applies to you: each setting, its
value, and who decided it (the default, your organization, one of your
groups, or an admin for you alone), locked or not; then the rules and
guardrails it applies to you. Your organization's admins change it in its
admin console. --kind rule or guardrail lists those alone.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if kind == "" {
				settings, err := s.Policy(cmd.Context())
				if err != nil {
					return err
				}
				for _, p := range settings {
					fmt.Fprintln(out, i18n.T("org.policy_line", "path", p.Path, "value", p.ValueJson, "by", policyDecided(p)))
				}
			}
			abs, err := orgWorkspace(g)
			if err != nil {
				return err
			}
			res, err := s.OrgItems(cmd.Context(), abs, kind)
			if err != nil {
				return err
			}
			if len(res.Items) == 0 {
				fmt.Fprintln(out, i18n.T("org.catalog_none"))
				return nil
			}
			for _, it := range res.Items {
				line := it.Kind + "/" + it.Name
				if it.DecidedBy != "" {
					line += i18n.T("org.item_decided", "by", it.DecidedBy)
				} else if len(it.Groups) > 0 {
					line += i18n.T("org.item_for", "groups", strings.Join(it.Groups, ", "))
				}
				fmt.Fprintln(out, line)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "rule or guardrail")
	return cmd
}

// newEnterpriseRefreshCommand is blitz enterprise refresh: the
// organization's catalog and the person's policy fetched now (EN-38a).
func newEnterpriseRefreshCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Fetch your organization's policy and catalog now, rather than waiting for the next change",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			changed, err := s.RefreshOrganization(cmd.Context())
			if err != nil {
				return err
			}
			if changed {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("org.refreshed"))
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("org.refreshed_same"))
			}
			return nil
		},
	}
}

// newEnterpriseConsentCommand is blitz enterprise consent: whether the
// organization's connectors that query as the person may, and where to
// allow it; --revoke forgets it (EN-76).
func newEnterpriseConsentCommand() *cobra.Command {
	var revoke bool
	cmd := &cobra.Command{
		Use:   "consent",
		Short: "Let your organization's connectors that query as you do so, with your Google account (or --revoke)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := orgSettings(cmd)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if revoke {
				if err := s.RevokeConnectorConsent(cmd.Context()); err != nil {
					return err
				}
				fmt.Fprintln(out, i18n.T("org.consent_revoked"))
				return nil
			}
			c, err := s.ConnectorConsent(cmd.Context())
			if err != nil {
				return err
			}
			switch {
			case len(c.Connectors) == 0:
				fmt.Fprintln(out, i18n.T("org.consent_none"))
			case c.Consented:
				fmt.Fprintln(out, i18n.T("org.consent_done", "connectors", strings.Join(c.Connectors, ", ")))
			case c.Url == "":
				fmt.Fprintln(out, i18n.T("org.consent_unavailable"))
			default:
				fmt.Fprintln(out, i18n.T("org.consent_open", "connectors", strings.Join(c.Connectors, ", "), "url", c.Url))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&revoke, "revoke", false, "forget your consent: those connectors ask again")
	return cmd
}

// policyDecided is who decided a setting, as a line says it.
func policyDecided(p *pb.PolicySetting) string {
	by := i18n.T("org.decided_" + cmp.Or(p.Level, "global"))
	if p.Level == "group" || p.Level == "person" {
		by += " (" + strings.Join(p.By, ", ") + ")"
	}
	if p.Locked {
		by += i18n.T("org.decided_locked")
	}
	return by
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
