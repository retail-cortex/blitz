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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// Rules that gate a turn (spec_gates_041): before the agent runs, each
// before-agent rule judges the prompt; after, each after-agent rule the
// answer and the turn's changes; in order, by the suggestions model.

// maxRuleRevisions bounds the extra passes after-agent rules that revise
// can ask for in one turn.
const maxRuleRevisions = 2

// ruleSubjectLimit bounds what an after-agent rule is shown of the
// answer and of the diff.
const ruleSubjectLimit = 12 << 10

// ProjectRulesDir is where Studio writes the workspace's rules:
// .agents/rules.
func (w *Workspace) ProjectRulesDir() string { return filepath.Join(w.Dir(), ".agents", "rules") }

// ruleDirs are the folders rules are read from: the workspace's rule
// folders (.blitz/rules, .agents/rules, .claude/rules), then the global
// one; a name in an earlier folder wins.
func (w *Workspace) ruleDirs() []string {
	var out []string
	for _, rd := range w.cfg.Memory.RuleDirs {
		out = append(out, filepath.Join(w.Dir(), filepath.FromSlash(rd)))
	}
	if w.cfg.Memory.GlobalRules != "" {
		out = append(out, config.ExpandHome(w.cfg.Memory.GlobalRules))
	}
	return out
}

// Rules are the workspace's rules, gates first (in the order they run),
// then instruction rules, read afresh.
func (w *Workspace) Rules() []gates.Rule {
	if !w.cfg.Memory.Enabled {
		return nil
	}
	return gates.Load(w.ruleDirs()...)
}

// ruleAsker asks the suggestions model (else auto mode's, else the
// workspace's) for rule verdicts.
func (w *Workspace) ruleAsker() gates.Asker {
	return func(ctx context.Context, instruction, prompt string) (string, error) {
		llm, err := w.suggestionsModel(ctx)
		if err != nil {
			return "", fmt.Errorf("%s", ModelErrorSummary(err, w.settings()))
		}
		reply, served, usage, err := runtime.Ask(ctx, llm, instruction, prompt)
		meterUsage(ctx, served, w.engine.EstimateUsage(served, usage))
		return reply, err
	}
}

// JudgeRule judges subject against a rule as written (Studio's Try it):
// a prompt for a before-agent rule, an answer for an after-agent one.
func (w *Workspace) JudgeRule(ctx context.Context, r gates.Rule, subject string) (gates.Verdict, error) {
	return gates.Judge(ctx, w.ruleAsker(), r, w.secretRedactor().String(subject))
}

// auditRule records a rule's verdict in the audit log.
func (w *Workspace) auditRule(ctx context.Context, r gates.Rule, decision, reason string) {
	w.tools.Hooks().Audit().LogContext(ctx, audit.Entry{Kind: audit.KindRule, Tool: "rule:" + r.Name, Detail: reason, Decision: decision})
}

// beforeRules judges the prompt against each before-agent rule, in
// order. A failing block rule refuses the prompt (*api.BlockedError,
// nothing recorded); a warn rule says so; a revise rule's reason goes to
// the agent with the prompt (guidance). A rule that can't be judged says
// why and lets the prompt through.
func (w *Workspace) beforeRules(ctx context.Context, text string, notice func(string, bool)) (guidance string, err error) {
	var lines []string
	for _, r := range gates.Gates(w.Rules(), gates.BeforeAgent, w.engine.ActiveAgent()) {
		v, err := w.JudgeRule(ctx, r, text)
		switch {
		case err != nil:
			w.auditRule(ctx, r, "unjudged", err.Error())
			notice(i18n.T("rule.unjudged", "name", r.Name, "error", err.Error()), true)
		case v.Pass:
			w.auditRule(ctx, r, "pass", v.Reason)
		case r.Action == gates.Block:
			w.auditRule(ctx, r, "block", v.Reason)
			return "", &api.BlockedError{Reason: i18n.T("rule.blocked", "name", r.Name, "reason", v.Reason)}
		case r.Action == gates.Revise:
			w.auditRule(ctx, r, "revise", v.Reason)
			lines = append(lines, fmt.Sprintf("Rule %q: %s", r.Name, v.Reason))
		default:
			w.auditRule(ctx, r, "warn", v.Reason)
			notice(i18n.T("rule.warned", "name", r.Name, "reason", v.Reason), false)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// afterRules judges the turn's answer and the files its latest pass
// changed against each after-agent rule, in order. It returns what the
// failing revise rules ask (the agent's next pass); block and warn rules
// say so in the conversation, block as an error.
func (w *Workspace) afterRules(ctx context.Context, sessionID, answer string, notice func(string, bool)) []string {
	rules := gates.Gates(w.Rules(), gates.AfterAgent, w.engine.ActiveAgent())
	if len(rules) == 0 {
		return nil
	}
	subject := textutil.Ellipsize(strings.TrimSpace(answer), ruleSubjectLimit)
	if diff := w.tools.Checkpoints().LatestTurnDiff(sessionID); diff != "" {
		subject += "\n\nThe changes:\n" + textutil.Ellipsize(diff, ruleSubjectLimit)
	}
	var revise []string
	for _, r := range rules {
		v, err := w.JudgeRule(ctx, r, subject)
		switch {
		case err != nil:
			w.auditRule(ctx, r, "unjudged", err.Error())
			notice(i18n.T("rule.unjudged", "name", r.Name, "error", err.Error()), true)
		case v.Pass:
			w.auditRule(ctx, r, "pass", v.Reason)
		case r.Action == gates.Revise:
			w.auditRule(ctx, r, "revise", v.Reason)
			revise = append(revise, fmt.Sprintf("The rule %q isn't met: %s", r.Name, v.Reason))
		case r.Action == gates.Block:
			w.auditRule(ctx, r, "block", v.Reason)
			notice(i18n.T("rule.failed", "name", r.Name, "reason", v.Reason), true)
		default:
			w.auditRule(ctx, r, "warn", v.Reason)
			notice(i18n.T("rule.warned", "name", r.Name, "reason", v.Reason), false)
		}
	}
	return revise
}

// revisePrompt is what the agent is sent when after-agent rules ask for
// another pass.
func revisePrompt(reasons []string) string {
	return strings.Join(reasons, "\n") + "\n\nRevise your answer (and your changes) so these rules are met."
}

// RuleSpec is what Studio's rule form saves (gates.Spec, by name).
type RuleSpec = gates.Spec

// SaveRule writes a rule in .agents/rules (previous: the rule edited, ""
// for a new one), returning it as read back, or the problems that keep it
// from being saved.
func (w *Workspace) SaveRule(spec RuleSpec, previous string) (gates.Rule, []string, error) {
	path, probs, err := gates.Save(w.ProjectRulesDir(), spec, previous)
	if err != nil || len(probs) > 0 {
		return gates.Rule{}, probs, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return gates.Rule{}, nil, err
	}
	return gates.Parse(spec.Name, path, data), nil, nil
}

// DeleteRule removes a rule from .agents/rules.
func (w *Workspace) DeleteRule(name string) error { return gates.Delete(w.ProjectRulesDir(), name) }

// ReorderRules numbers the named rules in .agents/rules in the order
// given (10, 20, 30…).
func (w *Workspace) ReorderRules(names []string) error {
	return gates.Reorder(w.ProjectRulesDir(), names)
}
