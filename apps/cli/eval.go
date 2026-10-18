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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/spf13/cobra"
)

// blitz eval (spec_evals_042 EV-10): runs one of the workspace's evals,
// for CI: each result as it's scored, then the run; it exits 1 when the
// eval fails.

type evalOptions struct {
	cases []string
	json  bool
}

func newEvalCommand(g *globalFlags) *cobra.Command {
	var o evalOptions
	cmd := &cobra.Command{
		Use:   "eval <name>",
		Short: "Run one of the workspace's evals (.agents/evals/<name>.md)",
		Long: `Run an eval: each case's prompt on each agent the eval names (the active
one when it names none), unattended and without changing files or running
commands, each output scored against the one expected: exact, or near by a
model's judgement or embeddings. Exits 1 when a case scoped fail fails.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runEval(cmd, g, o, args[0]) },
	}
	f := cmd.Flags()
	f.StringSliceVar(&o.cases, "case", nil, "Run only these cases, by ID (comma-separated or repeated)")
	f.BoolVar(&o.json, "json", false, "Print the run as JSON when it's done, instead of each result")
	return cmd
}

func runEval(cmd *cobra.Command, g *globalFlags, o evalOptions, name string) error {
	cfg, err := loadConfig(g)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	warn := func(s string) { fmt.Fprintln(cmd.ErrOrStderr(), s) }
	b, _, _, err := openBackend(ctx, cfg, backendOptions{trustProject: g.trustProject, flags: g}, warn)
	if err != nil {
		return err
	}
	defer b.Close()
	opts := api.EvalOptions{Cases: o.cases}
	if !o.json {
		opts.OnResult = func(r api.EvalResult) { printEvalResult(out, r) }
	}
	run, err := b.RunEval(ctx, name, opts)
	switch {
	case errors.Is(err, api.ErrUnknownEval), errors.Is(err, api.ErrEvalNotRunnable):
		return withCode(exitUsage, err)
	case err != nil:
		return err
	}
	if o.json {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(run); err != nil {
			return err
		}
	}
	summary := evalSummary(run)
	switch run.Status {
	case api.EvalPassed:
		if !o.json {
			fmt.Fprintln(out, i18n.T("eval.passed", "eval", name, "summary", summary, "cost", fmt.Sprintf("$%.4f", run.CostUSD)))
		}
		return nil
	case api.EvalStopped:
		return withCode(exitInterrupted, errors.New(i18n.T("eval.stopped", "eval", name, "summary", summary)))
	}
	return withCode(exitFailure, errors.New(i18n.T("eval.failed", "eval", name, "summary", cmp.Or(run.Error, summary))))
}

// evalIcons mark a result by its status.
var evalIcons = map[string]string{api.EvalPass: "✓", api.EvalFail: "✗", api.EvalWarn: "⚠", api.EvalInfo: "ℹ", api.EvalError: "!"}

// printEvalResult prints a result on a line (its score, for a near case,
// against the threshold), then why it didn't pass and what was refused.
func printEvalResult(out io.Writer, r api.EvalResult) {
	what := r.Case
	if r.Name != "" {
		what += " " + r.Name
	}
	score := ""
	if r.Status != api.EvalError && r.Type == "near" {
		score = fmt.Sprintf(" · %.2f/%.2f", r.Score, r.Threshold)
	}
	fmt.Fprintf(out, "%s %s · %s%s\n", evalIcons[r.Status], what, r.Agent, score)
	if r.Status != api.EvalPass {
		if r.Reason != "" {
			fmt.Fprintf(out, "    %s\n", r.Reason)
		}
		if r.Status != api.EvalError && r.Type == "exact" {
			fmt.Fprintln(out, "    "+i18n.T("eval.got", "output", oneLine(r.Output)))
		}
	}
	for _, x := range r.Refusals {
		fmt.Fprintln(out, "    "+i18n.T("eval.refused", "what", x))
	}
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > 160 {
		s = string([]rune(s)[:159]) + "…"
	}
	return s
}

// evalSummary counts a run's results ("3 passed, 1 warning").
func evalSummary(run api.EvalRun) string {
	counts := run.Counts()
	var parts []string
	for _, s := range []string{api.EvalPass, api.EvalFail, api.EvalWarn, api.EvalInfo, api.EvalError} {
		if n := counts[s]; n > 0 {
			parts = append(parts, i18n.N("eval.count."+s, n))
		}
	}
	return strings.Join(parts, ", ")
}
