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

package engine

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/evals"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
)

// Drafting an agent's eval cases (spec_evals_042 EV-11): the suggestions
// model proposes cases from what the agent is for and its instructions.

// evalDraftBrief is the brief for drafting cases.
const evalDraftBrief = `You write test cases (an eval) for an AI agent, from its description and instructions. Propose 5 to 8 cases that show whether it does its job: typical requests, edge cases, and requests it should decline or redirect, as its instructions say. Each case has a self-contained prompt a user would send, and the answer expected, written as the agent should reply (short: the substance that must be there). Use type "exact" only for a reply that must be word for word (a format, a code); otherwise "near", scored by meaning, with threshold 0.8 (0.7 for open-ended answers). Scope "fail" for what the agent must get right, "warn" for tone or style, "info" for what's worth watching. Don't repeat cases it already has. Reply with JSON alone: {"cases": [{"id": "short-kebab-id", "name": "a few words", "prompt": "...", "output": "...", "type": "near" or "exact", "threshold": 0.8, "scope": "fail", "warn" or "info"}]}.`

// EvalDraft is the cases drafted, or why there are none.
type EvalDraft struct {
	Cases   []evals.Case
	Problem string
}

// DraftEvalCases has the suggestions model propose cases for agent, beside
// those it has (existing) and as request asks (optional). The cases
// aren't saved; one the model got wrong is left out. An unknown agent is
// an error; a model that fails is the draft's problem.
func (w *Workspace) DraftEvalCases(ctx context.Context, agent, request string, existing []evals.Case) (EvalDraft, error) {
	spec, ok := w.agents.Get(agent)
	if !ok {
		return EvalDraft{}, fmt.Errorf("agent %q isn't defined", agent)
	}
	redact := w.secretRedactor().String
	var b strings.Builder
	fmt.Fprintf(&b, "The agent: %s\nWhat it's for: %s\n", spec.Name, cmp.Or(redact(spec.Description), "(not said)"))
	if len(spec.Tools) > 0 {
		fmt.Fprintf(&b, "Its tools: %s\n", strings.Join(spec.Tools, ", "))
	}
	fmt.Fprintf(&b, "\nIts instructions:\n%s\n", cmp.Or(redact(spec.SystemPrompt), "(none)"))
	if len(existing) > 0 {
		b.WriteString("\nThe cases it has:\n")
		for _, c := range existing {
			fmt.Fprintf(&b, "- %s: %s\n", c.ID, strings.Join(strings.Fields(c.Prompt), " "))
		}
	}
	if r := strings.TrimSpace(request); r != "" {
		fmt.Fprintf(&b, "\nWhat the user asks for: %s\n", redact(r))
	}
	llm, err := w.newModel(ctx, w.cfg, cmp.Or(w.cfg.Suggestions.Model, w.cfg.Permissions.Auto.Model))
	if err != nil {
		return EvalDraft{Problem: ModelErrorSummary(err, w.cfg)}, nil
	}
	reply, served, usage, err := runtime.Ask(ctx, llm, evalDraftBrief, b.String())
	if err != nil {
		return EvalDraft{Problem: ModelErrorSummary(err, w.cfg)}, nil
	}
	if usage != nil {
		slog.Info("eval cases drafted", "model", served, "input_tokens", usage.PromptTokenCount, "output_tokens", usage.CandidatesTokenCount)
	}
	cases, err := parseCaseDraft(reply, existing)
	if err != nil {
		return EvalDraft{Problem: err.Error()}, nil
	}
	return EvalDraft{Cases: cases}, nil
}

// parseCaseDraft reads the model's cases (fences and text around them
// tolerated), keeping those that hold, each with an ID of its own.
func parseCaseDraft(reply string, existing []evals.Case) ([]evals.Case, error) {
	i, j := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if i < 0 || j < i {
		return nil, errors.New("the model's answer had no cases")
	}
	var v struct {
		Cases []struct {
			ID, Name, Prompt, Output, Type, Scope string
			Threshold                             float64
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(reply[i:j+1]), &v); err != nil {
		return nil, errors.New("the model's answer had no cases")
	}
	taken := map[string]bool{}
	for _, c := range existing {
		taken[c.ID] = true
	}
	var out []evals.Case
	for n, d := range v.Cases {
		c := evals.Case{ID: caseID(d.ID, n+1, taken), Name: strings.TrimSpace(d.Name), Prompt: strings.TrimSpace(d.Prompt), Output: strings.TrimSpace(d.Output),
			Type: strings.ToLower(strings.TrimSpace(d.Type)), Threshold: d.Threshold, Scope: strings.ToLower(strings.TrimSpace(d.Scope))}
		if c.Type != evals.Exact {
			c.Type = evals.Near
			if c.Threshold <= 0 || c.Threshold > 1 {
				c.Threshold = evals.DefaultThreshold
			}
		} else {
			c.Threshold = 0
		}
		if !slices.Contains([]string{evals.Fail, evals.Warn, evals.Info}, c.Scope) {
			c.Scope = evals.Fail
		}
		if c.Prompt == "" || c.Output == "" {
			continue
		}
		taken[c.ID] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("the model's answer had no cases")
	}
	return out, nil
}

// caseID is the model's ID for a case made safe (lowercase, hyphens,
// short) and unused, else c<n>.
func caseID(id string, n int, taken map[string]bool) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(id)) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	base := strings.Trim(b.String(), "-")
	if len(base) > 32 {
		base = strings.Trim(base[:32], "-")
	}
	if base == "" {
		base = fmt.Sprintf("c%d", n)
	}
	id = base
	for k := 2; taken[id]; k++ {
		id = fmt.Sprintf("%s-%d", base, k)
	}
	return id
}
