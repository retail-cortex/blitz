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
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/evals"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
)

// A skill's scenarios (spec_skills_013 SK-111) and trying it (SK-112):
// a prompt runs as an eval case does (unattended, in a session of its
// own), with the skills only offered, so what's checked is whether the
// agent picks the skill up as well as what it answers.

// ErrNoPrompt is a try without a prompt.
var ErrNoPrompt = errors.New("a prompt is needed to try the skill")

// SkillScenarios are a skill's scenarios: any skill the workspace sees.
func (w *Workspace) SkillScenarios(name string) ([]skills.Scenario, error) {
	s, ok := w.skills.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", skills.ErrUnknownSkill, name)
	}
	return skills.Scenarios(s), nil
}

// SaveSkillScenario writes one of a workspace skill's scenarios (previous
// "" for a new one), returning it as read back, or the problems that keep
// it from being saved.
func (w *Workspace) SaveSkillScenario(skill string, sc skills.Scenario, previous string) (skills.Scenario, []string, error) {
	_, dir, err := w.ownSkill(skill)
	if err != nil {
		return skills.Scenario{}, nil, err
	}
	p, probs, err := skills.SaveScenario(dir, skill, sc, previous)
	if err != nil || len(probs) > 0 {
		return skills.Scenario{}, probs, err
	}
	w.reloadSkills()
	for _, x := range skills.LoadScenarios(filepath.Dir(p)) {
		if x.Path == p {
			return x, nil, nil
		}
	}
	return skills.Scenario{}, nil, fmt.Errorf("%w: %q", skills.ErrUnknownScenario, sc.Name)
}

// DeleteSkillScenario removes one of a workspace skill's scenarios.
func (w *Workspace) DeleteSkillScenario(skill, name string) error {
	_, dir, err := w.ownSkill(skill)
	if err != nil {
		return err
	}
	if err := skills.DeleteScenario(dir, skill, name); err != nil {
		return err
	}
	w.reloadSkills()
	return nil
}

// skillAgent is who a skill is tried on: agent, else the first the skill
// is offered to, else the active agent.
func (w *Workspace) skillAgent(s *skills.Skill, agent string) (string, error) {
	agent = strings.TrimSpace(agent)
	if agent == "" && len(s.Agents) > 0 {
		agent = s.Agents[0]
	}
	if agent == "" {
		return w.engine.ActiveAgent(), nil
	}
	if _, ok := w.agents.Get(agent); !ok && agent != w.engine.ActiveAgent() {
		return "", fmt.Errorf("agent %q isn't defined", agent)
	}
	return agent, nil
}

// TrySkill runs prompt once on agent (SK-112): with the skill only
// offered, to see whether the agent uses it, or, forced, given to it. It
// saves nothing.
func (w *Workspace) TrySkill(ctx context.Context, skill, agent, prompt string, force bool) (api.SkillTry, error) {
	s, ok := w.skills.Get(skill)
	if !ok {
		return api.SkillTry{}, fmt.Errorf("%w: %q", skills.ErrUnknownSkill, skill)
	}
	if strings.TrimSpace(prompt) == "" {
		return api.SkillTry{}, ErrNoPrompt
	}
	agent, err := w.skillAgent(s, agent)
	if err != nil {
		return api.SkillTry{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, evalCaseTimeout)
	defer cancel()
	started := time.Now()
	out, used, spent, refusals, err := w.skillPrompt(ctx, s, agent, prompt, force, "try "+skill)
	if err != nil {
		return api.SkillTry{}, err
	}
	return api.SkillTry{
		Skill: skill, Agent: agent, Prompt: prompt, Output: out, SkillsUsed: used,
		Triggered: slices.Contains(used, skill), Forced: force, Refusals: refusals, CostUSD: spent, Duration: time.Since(started),
	}, nil
}

// skillPrompt runs prompt on agent unattended, the skills offered (and s
// given, forced), and returns the answer and the skills the agent used:
// those it activated, a meta skill's members with it.
func (w *Workspace) skillPrompt(ctx context.Context, s *skills.Skill, agent, prompt string, force bool, label string) (string, []string, float64, []string, error) {
	var used []string
	use := func(name string) {
		if name != "" && !slices.Contains(used, name) {
			used = append(used, name)
		}
	}
	var useWith func(name string, depth int)
	useWith = func(name string, depth int) {
		use(name)
		if m, ok := w.skills.Get(name); ok && depth < 4 {
			for _, member := range m.Members {
				useWith(member, depth+1)
			}
		}
	}
	sent := prompt
	if force {
		sent += w.evalSkillNote([]string{s.Name})
		useWith(s.Name, 0)
	}
	on := func(ev api.Event) {
		if c := ev.ToolCall; c != nil && !c.Partial && c.Name == "activate_skill" {
			if name, _ := c.Args["skill_name"].(string); name != "" {
				useWith(name, 0)
			}
		}
	}
	out, spent, refusals, err := w.unattended(ctx, label, agent, prompt, sent, nil, on)
	return out, used, spent, refusals, err
}

// runSkillScenarios runs skill's scenarios (or those o.Cases names) as
// eval name (SkillEvalPrefix + skill) and records the run.
func (w *Workspace) runSkillScenarios(ctx context.Context, name, skill string, o api.EvalOptions) (api.EvalRun, error) {
	s, ok := w.skills.Get(skill)
	if !ok {
		return api.EvalRun{}, fmt.Errorf("%w: skill %q", api.ErrUnknownEval, skill)
	}
	list := skills.Scenarios(s)
	if len(o.Cases) > 0 {
		list = slices.DeleteFunc(list, func(sc skills.Scenario) bool { return !slices.Contains(o.Cases, sc.Name) })
	}
	if len(list) == 0 {
		return api.EvalRun{}, fmt.Errorf("%w: the skill has no scenario to run", api.ErrEvalNotRunnable)
	}
	agent, err := w.skillAgent(s, "")
	if err != nil {
		return api.EvalRun{}, fmt.Errorf("%w: %s", api.ErrEvalNotRunnable, err)
	}
	run := api.EvalRun{ID: newRunID(), Workspace: w.Dir(), Eval: name, Started: time.Now(), Status: api.EvalRunning}
	if o.OnStart != nil {
		o.OnStart(run)
	}
	score, err := w.evalScorer(ctx, evals.Eval{Near: evals.Judge})
	if err != nil {
		run.Status, run.Error, run.Duration = api.EvalFailed, err.Error(), time.Since(run.Started)
		return run, w.recordEval(run)
	}
	for _, sc := range list {
		if ctx.Err() != nil {
			break
		}
		r := w.runScenario(ctx, s, sc, agent, score)
		run.Results = append(run.Results, r)
		run.CostUSD += r.CostUSD
		if o.OnResult != nil {
			o.OnResult(r)
		}
	}
	run.Duration = time.Since(run.Started)
	switch {
	case ctx.Err() != nil:
		run.Status = api.EvalStopped
	case slices.ContainsFunc(run.Results, func(r api.EvalResult) bool { return r.Status != api.EvalPass }):
		run.Status = api.EvalFailed
	default:
		run.Status = api.EvalPassed
	}
	return run, w.recordEval(run)
}

// runScenario runs one scenario and checks it: the skills it expects
// were used (or, executes false, the skill wasn't), and the answer is
// within its threshold of the outcome.
func (w *Workspace) runScenario(ctx context.Context, s *skills.Skill, sc skills.Scenario, agent string, score scorer) (r api.EvalResult) {
	r = api.EvalResult{Case: sc.Name, Name: sc.Description, Agent: agent, Type: evals.Near, Scope: evals.Fail, Prompt: sc.Prompt, Expected: sc.Outcome, Threshold: sc.Threshold}
	started := time.Now()
	defer func() { r.Duration = time.Since(started) }()
	run := ctx
	ctx, cancel := context.WithTimeout(ctx, evalCaseTimeout)
	defer cancel()
	fail := func(err error) api.EvalResult {
		if run.Err() == nil && ctx.Err() != nil {
			err = fmt.Errorf("the scenario took longer than %s: %w", evalCaseTimeout, err)
		}
		r.Status, r.Reason = api.EvalError, err.Error()
		return r
	}
	if sc.Problem != "" {
		return fail(errors.New(sc.Problem))
	}
	out, used, spent, refusals, err := w.skillPrompt(ctx, s, agent, sc.Prompt, false, fmt.Sprintf("scenario %s %s", s.Name, sc.Name))
	r.Output, r.SkillsUsed, r.CostUSD, r.Refusals = out, used, spent, refusals
	if err != nil {
		return fail(err)
	}
	why := scenarioSkills(s.Name, sc, used)
	sctx, done := context.WithTimeout(ctx, evalScoreTimeout)
	near, judged, cost, err := score(sctx, evals.Case{Prompt: sc.Prompt, Output: sc.Outcome, Type: evals.Near, Threshold: sc.Threshold}, w.secretRedactor().String(out))
	timedOut := sctx.Err() == context.DeadlineExceeded && ctx.Err() == nil
	done()
	r.CostUSD += cost
	switch {
	case timedOut:
		return fail(fmt.Errorf("scoring took longer than %s", evalScoreTimeout))
	case err != nil:
		return fail(fmt.Errorf("scoring: %w", err))
	}
	r.Score, r.Reason = near, judged
	if why != "" {
		r.Reason = strings.TrimSpace(why + " " + judged)
	}
	r.Status = api.EvalPass
	if why != "" || near < sc.Threshold {
		r.Status = evals.Fail
	}
	return r
}

// scenarioSkills is why the skills used don't fit the scenario ("" when
// they do): one it expects wasn't used, or the skill was when it
// shouldn't be.
func scenarioSkills(skill string, sc skills.Scenario, used []string) string {
	if !sc.Executes {
		if slices.Contains(used, skill) {
			return fmt.Sprintf("It used %s, which it shouldn't for this.", skill)
		}
		return ""
	}
	expected := sc.ExpectedSkills
	if len(expected) == 0 {
		expected = []string{skill}
	}
	var missing []string
	for _, e := range expected {
		if !slices.Contains(used, e) {
			missing = append(missing, e)
		}
	}
	if len(missing) > 0 {
		return fmt.Sprintf("It didn't use %s.", strings.Join(missing, ", "))
	}
	return ""
}
