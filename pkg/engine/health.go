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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/agents"
	"github.com/retail-cortex/blitz/pkg/engine/audit"
	"github.com/retail-cortex/blitz/pkg/engine/gates"
	"github.com/retail-cortex/blitz/pkg/engine/health"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/retail-cortex/blitz/pkg/engine/workers"
	"google.golang.org/genai"
)

// The workspace dashboard (spec_health_044): its facts, from what the
// workspace records, and the judge's verdicts on each item.

// HealthReport is the dashboard: the facts, each item's latest verdict and
// its earlier scores, and the scores.
type HealthReport struct {
	health.Facts
	Verdicts map[string]health.Verdict
	// History are each item's overall scores, oldest first (the latest
	// included), by key.
	History map[string][]float64
	Overall float64
	ByKind  map[string]float64
	// PreviousOverall is the workspace's score before the last check.
	PreviousOverall float64
	State           health.State
	// Model judges; Schedule is when checks run ("" or "off": by hand).
	Model, Schedule string
	MaxCostUSD      float64
	// Next is when the schedule runs next (zero: it doesn't).
	Next time.Time
	// The reflection's schedule and cost limit ([harness]), and why the
	// last scheduled one didn't run or stopped.
	ReflectSchedule, ReflectSkipped string
	ReflectMaxCostUSD               float64
	ReflectThreshold                int
}

// HealthCheck says which items a check judges.
type HealthCheck struct {
	// Kinds and Items (kind/name keys) narrow it; empty: every item.
	Kinds []string
	Items []string
	// Force judges items whose verdict is current too.
	Force bool
	// Starting, when set, hears of each item as its judging starts.
	Starting func(item string, index, total int)
}

// HealthProgress is one step of a check.
type HealthProgress struct {
	Item  string
	Index int
	Total int
	// Verdict is the item's new verdict; Error why it couldn't be judged.
	Verdict *health.Verdict
	Error   string
	CostUSD float64
}

// ErrHealthCheckRunning means a check of the workspace is going already.
var ErrHealthCheckRunning = errors.New("a health check of this workspace is running")

// healthStore is the workspace's verdicts, in the service's storage.
func (w *Workspace) healthStore() *health.Store {
	return health.NewStore(filepath.Join(w.StorageDir(), "health", w.storeID()))
}

// storeID names the workspace in the service's storage: its settings
// folder's name.
func (w *Workspace) storeID() string {
	id := filepath.Base(config.WorkspaceSettingsDir("", w.Dir()))
	if id == "." || id == "" {
		sum := sha256.Sum256([]byte(w.Dir()))
		id = hex.EncodeToString(sum[:6])
	}
	return id
}

// healthModel is the judge's model reference.
func (w *Workspace) healthModel() string {
	return cmp.Or(w.cfg.Health.Model, w.cfg.Permissions.Auto.Model)
}

// HealthFacts are the dashboard's facts, read now.
func (w *Workspace) HealthFacts(ctx context.Context) health.Facts {
	return health.Build(w.healthInputs(ctx))
}

// Health is the dashboard: the facts and the verdicts.
func (w *Workspace) Health(ctx context.Context) HealthReport {
	f := w.HealthFacts(ctx)
	store := w.healthStore()
	all := store.All()
	r := HealthReport{Facts: f, Verdicts: map[string]health.Verdict{}, History: map[string][]float64{}, State: store.State(),
		Model: w.healthModel(), Schedule: w.cfg.Health.Schedule, MaxCostUSD: w.cfg.Health.MaxCostUSD,
		ReflectSchedule: w.cfg.Harness.ReflectSchedule, ReflectMaxCostUSD: w.cfg.Harness.MaxCostUSD, ReflectThreshold: w.cfg.Harness.ReflectThreshold, ReflectSkipped: w.harnessState().Skipped}
	for _, v := range all {
		if cur, ok := r.Verdicts[v.Key()]; !ok || !v.At.Before(cur.At) {
			r.Verdicts[v.Key()] = v
		}
		r.History[v.Key()] = append(r.History[v.Key()], v.Overall)
	}
	r.Overall, r.ByKind = health.Scores(f.Items, r.Verdicts)
	// Before the last check: each item's verdict from before it started.
	if !r.State.LastCheck.IsZero() {
		before := map[string]health.Verdict{}
		for _, v := range all {
			if v.At.Before(r.State.LastCheck.Add(-time.Hour)) {
				before[v.Key()] = v
			}
		}
		r.PreviousOverall, _ = health.Scores(f.Items, before)
	}
	r.Next = w.healthNext(r.State)
	return r
}

// healthNext is when the schedule next runs a check: after the last one,
// once there's been one (zero otherwise, or without a schedule).
func (w *Workspace) healthNext(st health.State) time.Time {
	s := strings.TrimSpace(w.cfg.Health.Schedule)
	if s == "" || strings.EqualFold(s, "off") || st.LastCheck.IsZero() {
		return time.Time{}
	}
	sched, err := workers.ParseSchedule(s, "")
	if err != nil {
		return time.Time{}
	}
	return sched.Next(st.LastCheck)
}

// HealthDue reports whether the schedule wants a check now.
func (w *Workspace) HealthDue(now time.Time) bool {
	next := w.healthNext(w.healthStore().State())
	return !next.IsZero() && !next.After(now)
}

// healthItems are the items a check judges, with their files.
func (w *Workspace) healthItems(ctx context.Context, c HealthCheck) []health.Item {
	f := w.HealthFacts(ctx)
	var out []health.Item
	for _, it := range f.Items {
		if len(c.Kinds) > 0 && !slices.Contains(c.Kinds, it.Kind) {
			continue
		}
		if len(c.Items) > 0 && !slices.Contains(c.Items, it.Key()) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// due are the items the check would judge.
func (w *Workspace) due(ctx context.Context, c HealthCheck) []health.Item {
	latest := w.healthStore().Latest()
	model := w.healthModel()
	var out []health.Item
	for _, it := range w.healthItems(ctx, c) {
		if health.Due(it, latest, model, c.Force) {
			out = append(out, it)
		}
	}
	return out
}

// healthFile is what the judge reads of an item: its file, secrets
// redacted. A built-in guardrail's is how it runs (gates.Describe),
// after its file when it has one, since a file holds only what it
// changes.
func (w *Workspace) healthFile(it health.Item) string {
	if it.Kind == health.KindGuardrail {
		if g, ok := w.guardrailNamed(it.Name); ok && g.Builtin != "" {
			how := gates.Describe(g)
			if it.Path == "" {
				return "(a built-in guardrail, with no file: it runs as below)\n" + how
			}
			return w.fileText(it.Path) + "\n\nAs it runs (the built-in's defaults with this file's changes):\n" + how
		}
	}
	if it.Path == "" {
		return fmt.Sprintf("(a built-in %s with its default settings: %s)", it.Kind, it.Detail)
	}
	return w.fileText(it.Path)
}

// guardrailNamed is the workspace's guardrail by that name.
func (w *Workspace) guardrailNamed(name string) (gates.Guardrail, bool) {
	all := w.Guardrails()
	i := slices.IndexFunc(all, func(g gates.Guardrail) bool { return g.Name == name })
	if i < 0 {
		return gates.Guardrail{}, false
	}
	return all[i], true
}

// fileText is an item's file for the judge, secrets redacted (a skill's
// SKILL.md).
func (w *Workspace) fileText(path string) string {
	p := path
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		p = filepath.Join(p, "SKILL.md")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "(the file can't be read: " + err.Error() + ")"
	}
	return w.secretRedactor().String(string(data))
}

// estimatedOutput is the tokens a verdict takes, for estimates.
const estimatedOutput = 700

// EstimateHealthCheck says how many items a check would judge and what it
// would cost, estimated from the prompts' size.
func (w *Workspace) EstimateHealthCheck(ctx context.Context, c HealthCheck) (items int, costUSD float64, priced bool) {
	model := w.healthModel()
	served := model
	if served == "" {
		served = w.Model().Name
	}
	priced = true
	for _, it := range w.due(ctx, c) {
		in := (len(health.Instruction) + len(health.Prompt(it, w.healthFile(it)))) / 4
		u := w.engine.EstimateUsage(served, &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: int32(in), CandidatesTokenCount: estimatedOutput})
		costUSD += u.CostUSD
		priced = priced && u.Priced
		items++
	}
	return items, costUSD, priced
}

// CheckHealth judges the check's items that are due, one at a time,
// saying how each went; a verdict is kept as it comes. Stopping ctx stops
// it after the item being judged.
func (w *Workspace) CheckHealth(ctx context.Context, c HealthCheck, progress func(HealthProgress)) (judged int, costUSD float64, err error) {
	if !w.healthMu.TryLock() {
		return 0, 0, ErrHealthCheckRunning
	}
	defer w.healthMu.Unlock()
	store := w.healthStore()
	items := w.due(ctx, c)
	ref := w.healthModel()
	var failed []string
	defer func() {
		st := store.State()
		st.LastCheck, st.LastCost, st.Skipped, st.Judged, st.Errors = time.Now(), costUSD, "", judged, failed
		_ = store.SetState(st)
	}()
	if len(items) == 0 {
		return 0, 0, nil
	}
	llm, err := w.newModel(ctx, w.settings(), ref)
	if err != nil {
		return 0, 0, fmt.Errorf("%s", ModelErrorSummary(err, w.settings()))
	}
	for i, it := range items {
		if ctx.Err() != nil {
			return judged, costUSD, ctx.Err()
		}
		p := HealthProgress{Item: it.Key(), Index: i + 1, Total: len(items)}
		if c.Starting != nil {
			c.Starting(it.Key(), i+1, len(items))
		}
		reply, served, usage, aerr := runtime.Ask(ctx, llm, health.Instruction, health.Prompt(it, w.healthFile(it)))
		cost := w.engine.EstimateUsage(served, usage).CostUSD
		costUSD += cost
		var v health.Verdict
		if aerr == nil {
			v, aerr = health.Parse(it, reply)
		}
		if aerr != nil {
			p.Error = aerr.Error()
			if len(failed) < 10 {
				failed = append(failed, it.Key()+": "+p.Error)
			}
		} else {
			// The model that answered, by name, when none is set.
			v.Model, v.Ref, v.At, v.CostUSD = cmp.Or(ref, served), ref, time.Now(), cost
			if serr := store.Add(v); serr != nil {
				p.Error = serr.Error()
			} else {
				p.Verdict = &v
				judged++
			}
		}
		p.CostUSD = costUSD
		if progress != nil {
			progress(p)
		}
	}
	return judged, costUSD, nil
}

// ScheduledHealthCheck runs the schedule's check: what changed, if its
// estimate is within [health] max_cost_usd (else it records why not).
func (w *Workspace) ScheduledHealthCheck(ctx context.Context, starting func(item string, index, total int)) error {
	n, cost, _ := w.EstimateHealthCheck(ctx, HealthCheck{})
	store := w.healthStore()
	if limit := w.cfg.Health.MaxCostUSD; limit > 0 && cost > limit {
		st := store.State()
		st.Skipped = fmt.Sprintf("judging %d items would cost about $%.2f, over the $%.2f limit", n, cost, limit)
		// Not again until the next scheduled time.
		st.LastCheck = time.Now()
		return store.SetState(st)
	}
	_, _, err := w.CheckHealth(ctx, HealthCheck{Starting: starting}, nil)
	return err
}

// healthInputs gathers what the facts are made from.
func (w *Workspace) healthInputs(ctx context.Context) health.Inputs {
	now := time.Now()
	in := health.Inputs{Now: now, DefaultAgent: w.engine.ActiveAgent()}
	if w.cfg.Blitz.DefaultAgent != "" {
		in.DefaultAgent = w.cfg.Blitz.DefaultAgent
	}

	files, _ := agents.ListFiles(w.ProjectAgentsDir(), w.agents.IsBuiltin)
	for _, f := range files {
		s := health.Source{Path: f.Path, Problem: f.Problem, Hash: fileHash(f.Path), Modified: modTime(f.Path)}
		s.Name = strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path))
		if f.Spec != nil {
			s.Name = cmp.Or(f.Spec.Name, s.Name)
			s.Detail = f.Spec.Description
		}
		in.Agents = append(in.Agents, s)
	}

	for _, sk := range w.skills.List() {
		if !w.ownsSkill(sk) {
			continue
		}
		info := w.skillInfo(sk)
		kind := "simple"
		switch {
		case len(sk.Members) > 0:
			kind = "meta"
		case len(sk.Scripts) > 0:
			kind = "scripts"
		case len(sk.References) > 0:
			kind = "references"
		}
		in.Skills = append(in.Skills, health.Source{Name: sk.Name, Path: sk.Path, Detail: kind, Hash: info.Hash, Agents: sk.Agents, Skills: sk.Members, Modified: modTime(sk.Path)})
	}

	for _, r := range w.Rules() {
		stage := cmp.Or(r.When, "instruction")
		in.Rules = append(in.Rules, health.Source{Name: r.Name, Path: r.Path, Detail: stage, Problem: r.Problem, Hash: fileHash(r.Path), Agents: r.Agents})
	}
	for _, g := range w.Guardrails() {
		detail := cmp.Or(g.Builtin, "custom") + " · " + g.Stage
		hash := fileHash(g.Path)
		if g.Builtin != "" {
			// How a built-in runs is part of it: new defaults are judged again.
			sum := sha256.Sum256([]byte(gates.Describe(g)))
			hash = "builtin:" + g.Name + ":" + hex.EncodeToString(sum[:6]) + ":" + hash
		}
		in.Guardrails = append(in.Guardrails, health.Source{Name: g.Name, Path: g.Path, Detail: detail, Problem: g.Problem, Hash: hash, Agents: g.Agents, Off: g.Off})
	}

	for _, e := range w.Evals() {
		s := health.Source{Name: e.Name, Path: e.Path, Detail: e.Description, Problem: e.Problem, Hash: fileHash(e.Path), Agents: e.Agents, Skills: e.Skills, Cases: len(e.Cases)}
		runs, _ := w.EvalRuns(e.Name, 50)
		for _, r := range runs {
			er := health.EvalRun{Started: r.Started}
			for _, x := range r.Results {
				er.Results = append(er.Results, health.EvalResult{Agent: x.Agent, Passed: x.Status == api.EvalPass})
			}
			s.EvalRuns = append(s.EvalRuns, er)
		}
		in.Evals = append(in.Evals, s)
	}

	since := now.Add(-health.Window)
	receipts := RecentFlowReceipts(w.StorageDir(), w.Dir(), since)
	for _, f := range w.Workflows() {
		s := health.Source{Name: f.Name, Path: f.Path, Detail: f.Description, Problem: f.Problem, Hash: fileHash(f.Path)}
		for _, n := range f.Nodes {
			s.Skills = append(s.Skills, n.Skills...)
			if n.Skill != "" {
				s.Skills = append(s.Skills, n.Skill)
			}
		}
		for _, r := range receipts {
			if r.Workflow != f.Name {
				continue
			}
			run := health.Run{Started: r.Started, CostUSD: r.Usage.CostUSD}
			switch r.Status {
			case RunDone:
				run.Status = health.RunDone
			case RunFailed:
				run.Status = health.RunFailed
			case RunStopped:
				run.Status = health.RunStopped
			default:
				continue // still going
			}
			if !r.Ended.IsZero() {
				run.Duration = r.Ended.Sub(r.Started)
			}
			for i := len(r.Steps) - 1; i >= 0; i-- {
				if r.Steps[i].Status == api.StepFailed {
					run.FailedStep = cmp.Or(r.Steps[i].Title, r.Steps[i].ID)
					break
				}
			}
			s.Runs = append(s.Runs, run)
		}
		in.Workflows = append(in.Workflows, s)
	}

	list, _ := w.ListWorkers()
	for _, wk := range list {
		s := health.Source{Name: wk.Name, Path: wk.Path, Detail: wk.Workflow, Hash: wk.Hash, Off: wk.State != api.StateEnabled}
		if len(wk.Problems) > 0 && wk.State != api.StateEnabled {
			s.Problem = strings.Join(wk.Problems, "; ")
		}
		runs, _ := w.WorkerRuns(wk.Name, 200)
		for _, r := range runs {
			if r.Started.Before(since) {
				continue
			}
			run := health.Run{Started: r.Started, Duration: r.Duration, CostUSD: r.CostUSD}
			switch r.Status {
			case api.RunSucceeded:
				run.Status = health.RunDone
			case api.RunFailed, api.RunLimited:
				run.Status = health.RunFailed
			case api.RunStopped:
				run.Status = health.RunStopped
			default:
				continue
			}
			s.Runs = append(s.Runs, run)
		}
		in.Workers = append(in.Workers, s)
	}

	// Only entries that name this workspace: those written before entries
	// named theirs (2026-10-22) could be any workspace's.
	for _, e := range audit.Read(config.ExpandHome(w.cfg.Audit.Dir), w.Dir(), since) {
		if e.Workspace != "" {
			in.Audit = append(in.Audit, e)
		}
	}
	_ = ctx
	return in
}

// fileHash is the SHA-256 of the file at path ("" when it can't be read;
// a folder's SKILL.md).
func fileHash(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func modTime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}
