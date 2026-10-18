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
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/evals"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/engine/skills"
	"github.com/retail-cortex/blitz/pkg/engine/tools"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// Evals (spec_evals_042): each case's prompt runs on each agent the eval
// names, unattended, in a session of its own that's deleted after; its
// output is scored against the expected one.

// Each case's limits: model calls and time.
var (
	evalMaxTurns = 25
	evalTimeout  = 5 * time.Minute
)

// ProjectEvalsDir is where the workspace's evals are: .agents/evals.
func (w *Workspace) ProjectEvalsDir() string { return filepath.Join(w.Dir(), ".agents", "evals") }

// Evals are the workspace's evals, read afresh, by name.
func (w *Workspace) Evals() []evals.Eval { return evals.Load(w.ProjectEvalsDir()) }

// SaveEval writes an eval in .agents/evals, returning it as read back, or
// the problems that keep it from being saved.
func (w *Workspace) SaveEval(e evals.Eval, previous string) (evals.Eval, []string, error) {
	path, probs, err := evals.Save(w.ProjectEvalsDir(), e, previous)
	if err != nil || len(probs) > 0 {
		return evals.Eval{}, probs, err
	}
	return w.eval(strings.TrimSuffix(filepath.Base(path), ".md"))
}

// DeleteEval removes an eval from .agents/evals.
func (w *Workspace) DeleteEval(name string) error { return evals.Delete(w.ProjectEvalsDir(), name) }

func (w *Workspace) eval(name string) (evals.Eval, []string, error) {
	for _, e := range w.Evals() {
		if e.Name == name {
			return e, nil, nil
		}
	}
	return evals.Eval{}, nil, fmt.Errorf("%w: %q", api.ErrUnknownEval, name)
}

// EvalRuns are an eval's recorded runs, newest first.
func (w *Workspace) EvalRuns(name string, limit int) ([]api.EvalRun, error) {
	return w.evalRuns.List(w.Dir(), name, limit)
}

// RunEval runs eval name: each case (or those o.Cases names) on each of
// its agents (the active one when it names none), one after another, and
// records the run. An eval with a problem, or that names no case asked
// for, isn't run.
func (w *Workspace) RunEval(ctx context.Context, name string, o api.EvalOptions) (api.EvalRun, error) {
	w.RefreshAgents(ctx)
	e, _, err := w.eval(name)
	if err != nil {
		return api.EvalRun{}, err
	}
	if e.Problem != "" {
		return api.EvalRun{}, fmt.Errorf("%w: %s", api.ErrEvalNotRunnable, e.Problem)
	}
	cases := e.Cases
	if len(o.Cases) > 0 {
		cases = slices.DeleteFunc(slices.Clone(cases), func(c evals.Case) bool { return !slices.Contains(o.Cases, c.ID) })
	}
	if len(cases) == 0 {
		return api.EvalRun{}, fmt.Errorf("%w: it has no case to run", api.ErrEvalNotRunnable)
	}
	agents := e.Agents
	if len(agents) == 0 {
		agents = []string{w.engine.ActiveAgent()}
	}
	run := api.EvalRun{ID: newRunID(), Workspace: w.Dir(), Eval: name, Started: time.Now(), Status: api.EvalRunning}
	if o.OnStart != nil {
		o.OnStart(run)
	}
	score, err := w.evalScorer(ctx, e)
	if err != nil {
		run.Status, run.Error, run.Duration = api.EvalFailed, err.Error(), time.Since(run.Started)
		return run, w.recordEval(run)
	}
	note := w.evalSkillNote(e.Skills)
	for _, c := range cases {
		for _, agent := range agents {
			if ctx.Err() != nil {
				break
			}
			r := w.runEvalCase(ctx, e, c, agent, note, score)
			run.Results = append(run.Results, r)
			run.CostUSD += r.CostUSD
			if o.OnResult != nil {
				o.OnResult(r)
			}
		}
	}
	run.Duration = time.Since(run.Started)
	counts := run.Counts()
	switch {
	case ctx.Err() != nil:
		run.Status = api.EvalStopped
	case counts[api.EvalFail] > 0 || slices.ContainsFunc(run.Results, func(r api.EvalResult) bool { return r.Status == api.EvalError && r.Scope == evals.Fail }):
		run.Status = api.EvalFailed
	default:
		run.Status = api.EvalPassed
	}
	return run, w.recordEval(run)
}

func (w *Workspace) recordEval(run api.EvalRun) error {
	if err := w.evalRuns.Append(run); err != nil {
		w.warn("recording the eval run: " + err.Error())
	}
	return nil
}

// scorer scores a case's output: 0 to 1, and why.
type scorer func(ctx context.Context, c evals.Case, output string) (float64, string, error)

// evalScorer is how e's near cases are scored: by the judge (the
// suggestions model, as rules are), or by the embeddings of
// search.embedding_model.
func (w *Workspace) evalScorer(ctx context.Context, e evals.Eval) (scorer, error) {
	if e.Near != evals.Embedding {
		ask := w.ruleAsker()
		return func(ctx context.Context, c evals.Case, output string) (float64, string, error) {
			return evals.JudgeNear(ctx, ask, c.Prompt, c.Output, output)
		}, nil
	}
	ref := w.cfg.Search.EmbeddingModel
	if strings.TrimSpace(ref) == "" {
		return nil, errors.New("near: embedding needs an embedding model (search.embedding_model)")
	}
	emb, err := newEmbedder(ctx, w.cfg, ref)
	if err != nil || emb == nil {
		return nil, fmt.Errorf("the embedding model %s: %v", ref, err)
	}
	var mu sync.Mutex
	cache := map[string][]float32{} // an expected output's vector, once
	return func(ctx context.Context, c evals.Case, output string) (float64, string, error) {
		mu.Lock()
		want, ok := cache[c.Output]
		mu.Unlock()
		texts := []string{output}
		if !ok {
			texts = append(texts, c.Output)
		}
		vecs, err := emb.Embed(ctx, texts)
		if err != nil || len(vecs) != len(texts) {
			return 0, "", fmt.Errorf("embedding the outputs: %v", err)
		}
		if !ok {
			want = vecs[1]
			mu.Lock()
			cache[c.Output] = want
			mu.Unlock()
		}
		return evals.Cosine(vecs[0], want), "", nil
	}, nil
}

// evalSkillNote is what goes to the agent with each prompt for the
// eval's skills, as \skill-name sends it; skills the workspace hasn't are
// left out.
func (w *Workspace) evalSkillNote(names []string) string {
	var list []*skills.Skill
	for _, n := range names {
		if s, ok := w.skills.Get(n); ok {
			list = append(list, s)
		}
	}
	return skillNote(list)
}

// runEvalCase runs one case on one agent and scores it. The case runs in
// a session of its own (deleted after), unattended: files aren't changed
// and commands aren't run; the web and MCP tools only when the eval allows
// them.
func (w *Workspace) runEvalCase(ctx context.Context, e evals.Eval, c evals.Case, agent, note string, score scorer) api.EvalResult {
	r := api.EvalResult{Case: c.ID, Name: c.Name, Agent: agent, Type: c.Type, Scope: c.Scope, Prompt: c.Prompt, Expected: c.Output}
	if c.Type == evals.Near {
		r.Threshold = c.Threshold
	}
	started := time.Now()
	defer func() { r.Duration = time.Since(started) }()
	fail := func(err error) api.EvalResult {
		r.Status, r.Reason = api.EvalError, err.Error()
		r.Duration = time.Since(started)
		return r
	}
	if _, ok := w.agents.Get(agent); !ok && agent != w.engine.ActiveAgent() {
		return fail(fmt.Errorf("agent %q isn't defined", agent))
	}
	st, err := session.NewStorage(w.cfg.Session.StorageDir)
	if err != nil {
		return fail(err)
	}
	st.SetWorkspace(w.Dir())
	rec, err := st.CreateSession(session.NewSessionID(), fmt.Sprintf("eval %s %s", e.Name, c.ID), agent)
	if err != nil {
		st.Release()
		return fail(err)
	}
	rec.Origin = session.OriginEval
	defer func() {
		st.Release()
		_ = st.Delete(rec.ID)
	}()
	var mu sync.Mutex
	decide := func(_ context.Context, req api.ApprovalRequest) (api.Decision, error) {
		if (req.Kind == api.ActionNetwork && slices.Contains(e.Allow, evals.AllowNetwork)) || (req.Kind == api.ActionMCP && slices.Contains(e.Allow, evals.AllowMCP)) {
			return api.DecisionOnce, nil
		}
		mu.Lock()
		r.Refusals = append(r.Refusals, textutil.Ellipsize(req.Tool+": "+req.Detail, 200))
		mu.Unlock()
		return api.DecisionDeny, nil
	}
	prompt := c.Prompt + note
	res, err := w.run(tools.Unattended(ctx, decide), rec.ID, turn{Turn: api.Turn{
		Text: c.Prompt, Prompt: prompt, MaxTurns: evalMaxTurns, Timeout: evalTimeout,
	}}, func(api.Event) {}, st, runtime.WithAgent(agent))
	r.CostUSD = w.engine.Usage(rec.ID).CostUSD
	if err != nil {
		return fail(err)
	}
	r.Output = res.Output
	pass := false
	if c.Type == evals.Exact {
		pass = evals.ExactMatch(c.Output, res.Output)
		r.Score = map[bool]float64{true: 1}[pass]
	} else {
		s, why, err := score(ctx, c, SecretRedactor(w.cfg).String(res.Output))
		if err != nil {
			return fail(fmt.Errorf("scoring: %w", err))
		}
		r.Score, r.Reason = s, why
		pass = s >= c.Threshold
	}
	r.Status = api.EvalPass
	if !pass {
		r.Status = cmp.Or(c.Scope, evals.Fail)
	}
	return r
}
