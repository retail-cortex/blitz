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
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// Workflows (spec_workflows_043): a workflow runs as a turn (/name input),
// its steps in order, each agent step a turn of its own in the session, so
// it streams, asks for approval and keeps its guardrails as any turn does.

// Limits on what a step reads and passes on.
const (
	flowReadLimit   = 256 << 10 // a read_file step's file
	flowStepPreview = 4 << 10   // a step's output in its event
)

// ProjectWorkflowsDir is where the workspace's workflows are: .agents/workflows.
func (w *Workspace) ProjectWorkflowsDir() string {
	return filepath.Join(w.Dir(), ".agents", "workflows")
}

// Workflows are the workspace's workflows, read afresh, by name; one in
// a loop of workflows calling each other, or calling one that isn't there,
// has that as its problem.
func (w *Workspace) Workflows() []flows.Flow {
	all := flows.Load(w.ProjectWorkflowsDir())
	loops := flows.CallLoops(all)
	for i, f := range all {
		var probs []string
		if f.Problem != "" {
			probs = append(probs, f.Problem)
		}
		if loop, ok := loops[f.Name]; ok {
			probs = append(probs, "the workflows call each other in a loop: "+loop)
		}
		for _, n := range f.Nodes {
			if n.Type == flows.Call && n.Workflow != "" && !slices.ContainsFunc(all, func(o flows.Flow) bool { return o.Name == n.Workflow }) {
				probs = append(probs, fmt.Sprintf("step %s: there's no workflow %q", n.ID, n.Workflow))
			}
		}
		all[i].Problem = strings.Join(probs, "; ")
	}
	return all
}

// workflow is the workspace's workflow name.
func (w *Workspace) workflow(name string) (flows.Flow, bool) {
	i := slices.IndexFunc(w.Workflows(), func(f flows.Flow) bool { return f.Name == name })
	if i < 0 {
		return flows.Flow{}, false
	}
	return w.Workflows()[i], true
}

// SaveWorkflow writes a workflow in .agents/workflows, returning it as
// read back, or the problems that keep it from being saved.
func (w *Workspace) SaveWorkflow(f flows.Flow, previous string) (flows.Flow, []string, error) {
	_, probs, err := flows.Save(w.ProjectWorkflowsDir(), f, previous)
	if err != nil || len(probs) > 0 {
		return flows.Flow{}, probs, err
	}
	saved, _ := w.workflow(strings.TrimSpace(f.Name))
	return saved, nil, nil
}

// DeleteWorkflow removes a workflow from .agents/workflows.
func (w *Workspace) DeleteWorkflow(name string) error {
	return flows.Delete(w.ProjectWorkflowsDir(), name)
}

// flowCommand is the workflow a command turn's text names, if it names one.
func (w *Workspace) flowCommand(text string) (flows.Flow, string, bool) {
	name, args, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(text), "/"), " ")
	f, ok := w.workflow(strings.ToLower(name))
	return f, strings.TrimSpace(args), ok
}

// runStep runs one step on its input and returns what it produced.
func (w *Workspace) runStep(ctx context.Context, sessionID string, f flows.Flow, n flows.Node, in string, on func(api.Event)) (string, error) {
	switch n.Type {
	case flows.Accumulate:
		return in, nil
	case flows.ReadFile:
		data, err := w.tools.Workspace().ReadFileLimit(n.Path, flowReadLimit)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", n.Path, err)
		}
		return string(data), nil
	case flows.SaveFile:
		return in, w.saveStepFile(ctx, n.Path, in)
	case flows.Script:
		out := w.tools.SkillScripts().Run(ctx, tools.RunSkillScriptInput{Skill: n.Skill, Script: n.ScriptName, Args: append(slices.Clone(n.Args), in)})
		if out.Error != "" {
			return "", errors.New(out.Error)
		}
		if out.ExitCode != 0 || out.TimedOut {
			return "", fmt.Errorf("the script %s exited with %d: %s", n.ScriptName, out.ExitCode, textutil.Ellipsize(strings.TrimSpace(out.Stderr), 400))
		}
		return out.Stdout, nil
	}
	return w.agentStep(ctx, sessionID, f, n, in, on)
}

// saveStepFile writes a save_file step's input to path in the workspace,
// once it's approved as any write is.
func (w *Workspace) saveStepFile(ctx context.Context, path, text string) error {
	ws := w.tools.Workspace()
	rel, err := ws.Rel(path)
	if err != nil {
		return err
	}
	if err := w.tools.Hooks().Approve(ctx, api.ApprovalRequest{Tool: "save_file", Kind: api.ActionWrite, Detail: rel, Targets: []string{rel}, KeyLabel: rel}); err != nil {
		return err
	}
	return ws.WriteFileAtomic(ctx, path, []byte(text))
}

// stepPrompts are how each agent step's type puts its instructions.
var stepPrompts = map[string]string{
	flows.Agent:     "%s",
	flows.Refine:    "Improve the input below as follows, and reply with the improved version alone: %s",
	flows.Summarize: "Summarize the input below. %s",
	flows.Browser:   "Use your browser tool to reach this goal, and report what you found: %s",
}

// agentStep runs an agent, refine, summarize or browser step as a turn of
// its own in the session (the after-agent gates that apply to its agent
// judge it there, as any turn), then judges the rules it names that those
// didn't: a failing block rule stops the workflow, revise asks once for a
// better answer, warn says so.
func (w *Workspace) agentStep(ctx context.Context, sessionID string, f flows.Flow, n flows.Node, in string, on func(api.Event)) (string, error) {
	var opts []runtime.ExecOption
	if n.Agent != "" {
		if _, ok := w.agents.Get(n.Agent); !ok {
			return "", fmt.Errorf("agent %q isn't defined", n.Agent)
		}
		opts = append(opts, runtime.WithAgent(n.Agent))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You're the step %q of the workflow %q.\n\n", cmp.Or(n.Title, n.ID), f.Name)
	fmt.Fprintf(&b, stepPrompts[n.Type], strings.TrimSpace(n.Instructions))
	if strings.TrimSpace(in) != "" {
		fmt.Fprintf(&b, "\n\nThe input:\n\n%s", in)
	}
	var list []*skills.Skill
	for _, s := range n.Skills {
		if sk, ok := w.skills.Get(s); ok {
			list = append(list, sk)
		}
	}
	b.WriteString(skillNote(list))
	label := fmt.Sprintf("/%s › %s", f.Name, cmp.Or(n.Title, n.ID))
	st := flowStorage(ctx, w)
	res, err := w.run(ctx, sessionID, turn{Turn: api.Turn{Text: label, Prompt: b.String()}}, on, st, opts...)
	if err != nil {
		return "", err
	}
	out := res.Output
	judged := map[string]bool{}
	for _, g := range gates.Gates(w.Rules(), gates.AfterAgent, cmp.Or(n.Agent, w.engine.ActiveAgent())) {
		judged[g.Name] = true
	}
	for _, name := range n.Rules {
		if judged[name] {
			continue // the step's turn judged it
		}
		r, ok := w.stepRule(name)
		if !ok {
			on(api.Event{Notice: &api.Notice{Text: i18n.T("flow.rule_unknown", "rule", name, "step", label), Error: true}})
			continue
		}
		v, err := w.judgeStep(ctx, r, out)
		switch {
		case err != nil:
			on(api.Event{Notice: &api.Notice{Text: i18n.T("rule.unjudged", "name", r.Name, "error", err.Error()), Error: true}})
		case v.Pass:
		case r.Action == gates.Block:
			return "", errors.New(i18n.T("flow.rule_blocked", "rule", r.Name, "reason", v.Reason))
		case r.Action == gates.Revise:
			again, err := w.run(ctx, sessionID, turn{Turn: api.Turn{Text: label, Prompt: i18n.T("flow.rule_revise", "rule", r.Name, "reason", v.Reason)}}, on, st, opts...)
			if err != nil {
				return "", err
			}
			out = again.Output
			if v2, err := w.judgeStep(ctx, r, out); err == nil && !v2.Pass {
				on(api.Event{Notice: &api.Notice{Text: i18n.T("flow.rule_still", "rule", r.Name, "reason", v2.Reason), Error: true}})
			}
		default:
			on(api.Event{Notice: &api.Notice{Text: i18n.T("flow.rule_warned", "rule", r.Name, "reason", v.Reason)}})
		}
	}
	return out, nil
}

// stepRule is the rule a step names (any rule: a gate's action applies,
// an instruction rule warns).
func (w *Workspace) stepRule(name string) (gates.Rule, bool) {
	i := slices.IndexFunc(w.Rules(), func(r gates.Rule) bool { return r.Name == name && r.Problem == "" })
	if i < 0 {
		return gates.Rule{}, false
	}
	r := w.Rules()[i]
	if !r.IsGate() {
		r.Action = gates.Warn
	}
	r.When, r.Action = gates.AfterAgent, cmp.Or(r.Action, gates.Warn)
	return r, true
}

// judgeStep judges a step's output against a rule, as an after-agent
// gate judges an answer.
func (w *Workspace) judgeStep(ctx context.Context, r gates.Rule, output string) (gates.Verdict, error) {
	return gates.Judge(ctx, w.ruleAsker(), r, SecretRedactor(w.cfg).String(textutil.Ellipsize(output, 12<<10)))
}

// extractBrief is the brief for an extract step.
const extractBrief = `You pull values out of text for a workflow. Reply with JSON alone: an object with exactly the fields asked for, each of the type asked for (a list is a list of strings); null for a value the text doesn't give. Don't guess.`

// extractStep pulls an extract step's fields out of its input with the
// suggestions model (WF-13): its data is the object, its text the JSON.
func (w *Workspace) extractStep(ctx context.Context, n flows.Node, input string) (flows.Result, error) {
	var b strings.Builder
	b.WriteString("The fields:\n")
	for _, f := range n.Fields {
		fmt.Fprintf(&b, "- %s (%s)", f.Name, cmp.Or(f.Type, "string"))
		if f.Description != "" {
			fmt.Fprintf(&b, ": %s", f.Description)
		}
		b.WriteString("\n")
	}
	if n.Instructions != "" {
		fmt.Fprintf(&b, "\nAlso: %s\n", n.Instructions)
	}
	fmt.Fprintf(&b, "\nThe text:\n%s", SecretRedactor(w.cfg).String(textutil.Ellipsize(input, 32<<10)))
	reply, err := w.ruleAsker()(ctx, extractBrief, b.String())
	if err != nil {
		return flows.Result{}, err
	}
	obj, ok := flows.DataOf(reply).(map[string]any)
	if !ok {
		return flows.Result{}, fmt.Errorf("the model's answer isn't the fields: %s", textutil.Ellipsize(reply, 200))
	}
	out := map[string]any{}
	for _, f := range n.Fields {
		v, err := fieldValue(f, obj[f.Name])
		if err != nil {
			return flows.Result{}, fmt.Errorf("field %s: %w", f.Name, err)
		}
		out[f.Name] = v
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	return flows.Result{Text: string(data), Data: out}, nil
}

// fieldValue is v as field f's type, or why it isn't.
func fieldValue(f flows.Field, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch f.Type {
	case "number":
		switch x := v.(type) {
		case float64:
			return x, nil
		case string:
			if n, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
				return n, nil
			}
		}
		return nil, fmt.Errorf("%v isn't a number", v)
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("%v isn't true or false", v)
	case "list":
		items, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("%v isn't a list", v)
		}
		out := make([]any, 0, len(items))
		for _, it := range items {
			out = append(out, fmt.Sprint(it))
		}
		return out, nil
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	return fmt.Sprint(v), nil
}

// conditionBrief is the brief for a link's condition in plain English.
const conditionBrief = `You decide whether a condition holds for a step's output in a workflow. Judge only the condition given. Reply with JSON alone: {"verdict": "yes" or "no", "reason": "one sentence"}.`

// judgeCondition has the suggestions model decide whether a condition in
// plain English holds for a step's output (WF-13).
func (w *Workspace) judgeCondition(ctx context.Context, condition, output string) (bool, error) {
	reply, err := w.ruleAsker()(ctx, conditionBrief, fmt.Sprintf("The condition: %s\n\nThe step's output:\n%s", condition, SecretRedactor(w.cfg).String(textutil.Ellipsize(output, 16<<10))))
	if err != nil {
		return false, fmt.Errorf("the condition %q couldn't be judged: %w", condition, err)
	}
	v, ok := flows.DataOf(reply).(map[string]any)
	if !ok {
		return false, fmt.Errorf("the condition %q couldn't be judged: %s", condition, textutil.Ellipsize(reply, 200))
	}
	switch strings.ToLower(fmt.Sprint(v["verdict"])) {
	case "yes", "true":
		return true, nil
	case "no", "false":
		return false, nil
	}
	return false, fmt.Errorf("the condition %q couldn't be judged: %s", condition, textutil.Ellipsize(reply, 200))
}

// flowStorageKey carries the storage a workflow's chat is in (a
// background run's, a worker's), when it isn't the workspace's.
type flowStorageKey struct{}

// withFlowStorage is ctx with st as the storage a workflow's chat is in.
func withFlowStorage(ctx context.Context, st *session.Storage) context.Context {
	return context.WithValue(ctx, flowStorageKey{}, st)
}

// flowStorage is the storage a workflow's chat is in: ctx's, else the
// workspace's.
func flowStorage(ctx context.Context, w *Workspace) *session.Storage {
	if st, ok := ctx.Value(flowStorageKey{}).(*session.Storage); ok && st != nil {
		return st
	}
	return w.storage
}
