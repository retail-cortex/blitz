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
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/spf13/cobra"
)

// errSpecErrors fails `blitz spec review` when a check found an error, so
// CI can gate on the specification (spec_057_spec_driven SDD-06).
var errSpecErrors = errors.New("the specification has errors")

// errSpecDrift fails `blitz spec verify --ci` when an approved requirement
// drifted (SDD-31, SDD-40).
var errSpecDrift = errors.New("approved requirements drifted")

// newSpecCommand is `blitz spec list|review|approve|verify|impact`: the
// project's specification (.agents/specs, as /spec writes it), listed in
// order, reviewed, approved, verified and its changes' impact scanned, as
// Studio › Specs does (SDD-01..03, SDD-06, SDD-11, SDD-12, SDD-20, SDD-21).
func newSpecCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "spec", Short: "List, review, approve and verify the project's specification (.agents/specs), and scan what its changes reach"}
	list := &cobra.Command{
		Use:   "list",
		Short: "List the specs in order, with their status and what the checks found",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			specs, err := r.ListSpecs()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(specs) == 0 {
				fmt.Fprintln(out, i18n.T("spec.cli_none"))
				return nil
			}
			for _, s := range specs {
				fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", s.File, orDash(s.Status), orDash(s.Title), specCounts(s))
			}
			return nil
		},
	}
	var judge bool
	review := &cobra.Command{
		Use:   "review [file...]",
		Short: "Review specs (every one without arguments); --judge asks the model too, which costs",
		Long:  "Review checks each spec without a model: its title, status, what it depends on, its requirements' IDs, vague words, \"Done when\" and open questions, and that the index lists every spec. --judge adds the model's review (what can't be checked, ambiguity, contradictions, missing cases) and prints what it cost. It exits 1 when a check finds an error.",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				specs, err := r.ListSpecs()
				if err != nil {
					return err
				}
				for _, s := range specs {
					args = append(args, s.Path)
				}
			}
			out := cmd.OutOrStdout()
			failed := false
			cost := 0.0
			for _, a := range args {
				p := specArg(a)
				text, err := os.ReadFile(filepath.Join(r.Dir(), filepath.FromSlash(p)))
				if err != nil {
					return withCode(exitUsage, err)
				}
				rev, err := r.ReviewSpec(cmd.Context(), p, string(text), judge)
				if err != nil {
					return err
				}
				cost += rev.CostUSD
				failed = printSpecReview(out, p, rev) || failed
			}
			if judge {
				fmt.Fprintln(out, i18n.T("spec.cli_cost", "cost", fmt.Sprintf("$%.4f", cost)))
			}
			if failed {
				return withCode(exitFailure, errSpecErrors)
			}
			return nil
		},
	}
	review.Flags().BoolVar(&judge, "judge", false, "ask the model to review too (it costs)")
	var force bool
	approve := &cobra.Command{
		Use:   "approve <file>",
		Short: "Approve a spec: Status: approved, its baseline and its requirements recorded in .agents/.ledger",
		Long:  "Approve writes Status: approved in the spec, keeps its text as the baseline in .agents/.ledger/baselines and records each requirement's fingerprint, so later changes show; approving a changed spec again keeps a diff in .agents/.ledger/diffs. A spec with errors or open questions needs --force.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			a, err := r.ApproveSpec(cmd.Context(), specArg(args[0]), force)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, i18n.T("spec.cli_approved", "spec", specArg(args[0])))
			if a.Diff != "" {
				fmt.Fprintln(out, i18n.T("spec.cli_diff", "path", a.Diff))
			}
			return nil
		},
	}
	approve.Flags().BoolVar(&force, "force", false, "approve with errors or open questions")
	var yes, weigh, ci bool
	verify := &cobra.Command{
		Use:   "verify [file...]",
		Short: "Find each requirement's ID in the work (every spec without arguments); --judge has the model judge it",
		Long:  "Verify looks for each requirement's ID in the workspace's files and commit messages and records it in .agents/.ledger as traced (code, a test or a commit names it) or untraced. --judge has the model judge each requirement from that evidence (implemented, partial, missing, contradicted): without --yes it only prints what that would cost.",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				specs, err := r.ListSpecs()
				if err != nil {
					return err
				}
				for _, s := range specs {
					if s.Number > 0 {
						args = append(args, s.Path)
					}
				}
			}
			out := cmd.OutOrStdout()
			estimate := weigh && !yes
			total := 0.0
			drifted := 0
			for _, a := range args {
				v, err := r.VerifySpec(cmd.Context(), specArg(a), weigh, estimate)
				if err != nil {
					return err
				}
				if v.Problem != "" {
					fmt.Fprintf(out, "%s: %s\n", specArg(a), v.Problem)
				}
				if estimate {
					total += v.EstimatedUSD
					continue
				}
				total += v.CostUSD
				for _, q := range v.Requirements {
					printRequirement(out, q)
				}
				for _, d := range v.Drift {
					fmt.Fprintln(out, i18n.T("spec.cli_drift."+d.Why, "spec", specArg(a), "id", d.ID))
					drifted++
				}
			}
			switch {
			case estimate:
				fmt.Fprintln(out, i18n.T("spec.cli_estimate", "cost", fmt.Sprintf("$%.4f", total)))
			case weigh:
				fmt.Fprintln(out, i18n.T("spec.cli_cost", "cost", fmt.Sprintf("$%.4f", total)))
			}
			if ci && drifted > 0 {
				return withCode(exitFailure, errSpecDrift)
			}
			return nil
		},
	}
	verify.Flags().BoolVar(&weigh, "judge", false, "have the model judge each requirement (it costs; prints the estimate without --yes)")
	verify.Flags().BoolVar(&yes, "yes", false, "with --judge: spend it")
	verify.Flags().BoolVar(&ci, "ci", false, "exit 1 when an approved requirement drifted: changed since approval, no longer found in the work, or judged missing or contradicted")
	implement := &cobra.Command{
		Use:   "implement <file>",
		Short: "Print the prompt that has an agent implement a spec's remaining requirements, tests first",
		Long:  "Implement prints the prompt for the spec's requirements not implemented yet (by its last verification), in order, tests first from its Done when, with each requirement's ID to cite in the code, the tests and the commit message, so verify traces them. Run it with: blitz \"$(blitz spec implement <file>)\". What the spec waits for is said on stderr.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			plan, err := r.PlanSpec(specArg(args[0]))
			if err != nil {
				return err
			}
			if len(plan.BlockedBy) > 0 {
				nums := make([]string, len(plan.BlockedBy))
				for i, d := range plan.BlockedBy {
					nums[i] = fmt.Sprintf("%03d", d)
				}
				fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("spec.cli_waits", "specs", strings.Join(nums, ", ")))
			}
			if plan.Prompt == "" {
				fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("spec.cli_nothing_left"))
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), plan.Prompt)
			return nil
		},
	}
	var impactYes, impactJudge bool
	impact := &cobra.Command{
		Use:   "impact <file>",
		Short: "What a spec's changes since its approval reach in the work; --judge has the model write the fix",
		Long:  "Impact records the spec's change (Status: changed, a diff in .agents/.ledger/diffs), then follows each changed requirement from where it was found to the declarations that hold it and their uses (the language server's references, else a text search), and searches for the new requirements. --judge has the model write the report (a summary, the steps to fix it, tests first, and conflicts with other specs) into .agents/.ledger/impact: without --yes it only prints what that would cost.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := attachWorkspace(cmd.Context(), g)
			if err != nil {
				return err
			}
			p := specArg(args[0])
			rec, err := r.RecordSpecChange(cmd.Context(), p)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if rec.Diff != "" {
				fmt.Fprintln(out, i18n.T("spec.cli_diff", "path", rec.Diff))
			}
			estimate := impactJudge && !impactYes
			im, err := r.ImpactSpec(cmd.Context(), p, impactJudge, estimate)
			if err != nil {
				return err
			}
			if len(im.Changes) == 0 {
				fmt.Fprintln(out, i18n.T("spec.cli_unchanged"))
				return nil
			}
			printImpact(out, im)
			switch {
			case im.Problem != "":
				fmt.Fprintf(out, "%s: %s\n", p, im.Problem)
			case estimate:
				fmt.Fprintln(out, i18n.T("spec.cli_impact_estimate", "cost", fmt.Sprintf("$%.4f", im.EstimatedUSD)))
			case impactJudge:
				fmt.Fprintln(out, i18n.T("spec.cli_report", "path", im.Report, "cost", fmt.Sprintf("$%.4f", im.CostUSD)))
			}
			return nil
		},
	}
	impact.Flags().BoolVar(&impactJudge, "judge", false, "have the model write the report (it costs; prints the estimate without --yes)")
	impact.Flags().BoolVar(&impactYes, "yes", false, "with --judge: spend it")
	cmd.AddCommand(list, review, approve, verify, impact, implement)
	return cmd
}

// specArg is a spec's path: a bare name is in .agents/specs.
func specArg(a string) string {
	if strings.Contains(a, "/") {
		return a
	}
	return path.Join(".agents/specs", a)
}

// printRequirement prints a requirement's line: its spec and ID, its
// state (with what changed since approval), and where it was found or why.
func printRequirement(out io.Writer, q api.SpecRequirement) {
	state := orDash(q.State)
	if q.Change != "" {
		state += " (" + q.Change + ")"
	}
	where := q.Reason
	if where == "" && len(q.Evidence) > 0 {
		e := q.Evidence[0]
		where = e.Path
		if e.Line > 0 {
			where = fmt.Sprintf("%s:%d", e.Path, e.Line)
		}
	}
	fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", q.Spec, q.ID, state, where)
}

// printSpecReview prints p's findings as file:line: severity: message;
// it reports whether one is an error.
func printSpecReview(out io.Writer, p string, rev api.SpecReview) bool {
	failed := false
	for _, f := range rev.Findings {
		at := p
		if f.Line > 0 {
			at = fmt.Sprintf("%s:%d", p, f.Line)
		}
		who := ""
		if f.Judged {
			who = " (model)"
		}
		fmt.Fprintf(out, "%s: %s%s: %s\n", at, f.Severity, who, f.Message)
		failed = failed || f.Severity == "error"
	}
	if rev.Problem != "" {
		fmt.Fprintf(out, "%s: %s\n", p, rev.Problem)
	}
	return failed
}

// specCounts says what a spec holds and what its checks found.
func specCounts(s api.SpecSummary) string {
	parts := []string{i18n.N("spec.cli_items", s.Items)}
	if s.Errors > 0 {
		parts = append(parts, i18n.N("spec.cli_errors", s.Errors))
	}
	if s.Warnings > 0 {
		parts = append(parts, i18n.N("spec.cli_warnings", s.Warnings))
	}
	if s.OpenQuestions > 0 {
		parts = append(parts, i18n.N("spec.cli_questions", s.OpenQuestions))
	}
	return strings.Join(parts, ", ")
}

// orDash is s, or - for nothing.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// printImpact prints an impact scan: the changes, the declarations
// followed, the places reached, then the model's summary, steps and
// conflicts when it wrote them.
func printImpact(out io.Writer, im api.SpecImpact) {
	for _, c := range im.Changes {
		fmt.Fprintf(out, "%s\t%s\t%s\n", c.ID, c.Kind, cmpOr(c.New, c.Old))
	}
	for _, s := range im.Symbols {
		fmt.Fprintf(out, "%s:%d\t%s\t%s\n", s.Path, s.Line, s.Name, i18n.N("spec.cli_uses", s.Uses))
	}
	for _, p := range im.Places {
		at := p.Path
		if p.Line > 0 {
			at = fmt.Sprintf("%s:%d", p.Path, p.Line)
		}
		fmt.Fprintf(out, "%s\t%s\t%s\n", at, p.Kind, p.Why)
	}
	if im.Summary != "" {
		fmt.Fprintf(out, "\n%s\n", im.Summary)
	}
	for i, st := range im.Steps {
		fmt.Fprintf(out, "%d. %s: %s\n", i+1, st.Title, st.Detail)
	}
	for _, c := range im.Conflicts {
		fmt.Fprintf(out, "! %s: %s\n", c.ID, c.Why)
	}
}

// cmpOr is a, or b when a is empty.
func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
