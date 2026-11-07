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

// newSpecCommand is `blitz spec list|review|approve|verify`: the project's
// specification (.agents/specs, as /spec writes it), listed in order,
// reviewed, approved and verified, as Studio › Specs does (SDD-01..03,
// SDD-06, SDD-11, SDD-12).
func newSpecCommand(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "spec", Short: "List, review, approve and verify the project's specification (.agents/specs)"}
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
	var yes, weigh bool
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
			}
			switch {
			case estimate:
				fmt.Fprintln(out, i18n.T("spec.cli_estimate", "cost", fmt.Sprintf("$%.4f", total)))
			case weigh:
				fmt.Fprintln(out, i18n.T("spec.cli_cost", "cost", fmt.Sprintf("$%.4f", total)))
			}
			return nil
		},
	}
	verify.Flags().BoolVar(&weigh, "judge", false, "have the model judge each requirement (it costs; prints the estimate without --yes)")
	verify.Flags().BoolVar(&yes, "yes", false, "with --judge: spend it")
	cmd.AddCommand(list, review, approve, verify)
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
