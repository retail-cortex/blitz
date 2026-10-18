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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// Run receipts (spec_workflows_043 WF-19): every run of a workflow keeps a
// receipt, its envelope and routing slip: what started it and when, each
// step's run (when it started and ended, its tries, the models it used,
// its tokens and cost, what it produced and the text it streamed), the
// run's events and how it ended. A receipt is written as the run goes, so
// a client follows it from anywhere (WatchWorkflowRun), and kept after.

// Limits on a receipt.
const (
	receiptStream = 64 << 10 // a step's streamed text
	receiptEvents = 1000     // the events kept (the latest)
)

// keptReceipts is how many receipts a storage dir keeps (the newest).
var keptReceipts = 500

// receiptBeat is how often a running run's receipt is written while
// nothing else changes (so another process sees it's alive); receiptStale
// is how long a receipt that says running can go unwritten before it's
// taken for a run the service lost (it stopped while the run ran).
var (
	receiptBeat  = time.Minute
	receiptStale = 3 * time.Minute
)

// A run's status, beside the steps' (api.StepRunning, …).
const (
	RunRunning = "running"
	RunWaiting = "waiting" // paused on a human task or a timer
	RunDone    = "done"
	RunFailed  = "failed"
	RunStopped = "stopped"
)

// FlowReceipt is a workflow run's receipt.
type FlowReceipt struct {
	Run       string `json:"run"`
	Workspace string `json:"workspace"`
	Workflow  string `json:"workflow"`
	// Origin is chat, background or worker; Session the chat its agent
	// steps are turns in.
	Origin  string    `json:"origin"`
	Session string    `json:"session"`
	Input   string    `json:"input"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitzero"`
	Status  string    `json:"status"`
	Error   string    `json:"error,omitempty"`
	// Output is what the run produced (the start of it).
	Output string `json:"output,omitempty"`
	// Usage is the steps' together; Models every model they used.
	Usage  api.Usage      `json:"usage"`
	Models []string       `json:"models,omitempty"`
	Steps  []StepReceipt  `json:"steps"`
	Events []ReceiptEvent `json:"events,omitempty"`
}

// StepReceipt is one run of a step (a step a loop goes back to runs again,
// each run its own).
type StepReceipt struct {
	ID      string    `json:"id"`
	Title   string    `json:"title,omitempty"`
	Type    string    `json:"type"`
	Status  string    `json:"status"`
	Started time.Time `json:"started,omitzero"`
	Ended   time.Time `json:"ended,omitzero"`
	// Attempts is how many times it was tried (its retry).
	Attempts int `json:"attempts,omitempty"`
	// Agent is an agent step's; Models are the models its calls used
	// (its turns', a rule's judge, an extract step's).
	Agent  string    `json:"agent,omitempty"`
	Models []string  `json:"models,omitempty"`
	Usage  api.Usage `json:"usage"`
	Output string    `json:"output,omitempty"`
	// Stream is the text its turns streamed as they went (the start of it).
	Stream string `json:"stream,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ReceiptEvent is something that happened in a run.
type ReceiptEvent struct {
	At time.Time `json:"at"`
	// Step is the step it's about, if any.
	Step string `json:"step,omitempty"`
	// Kind is run (started, resumed, ended), step, tool or notice.
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Error bool   `json:"error,omitempty"`
}

// Running says whether the receipt is of a run that hasn't ended.
func (r FlowReceipt) Running() bool { return r.Status == RunRunning }

// Summary is the receipt without its events and the steps' text.
func (r FlowReceipt) Summary() FlowReceipt {
	r.Events = nil
	r.Steps = slices.Clone(r.Steps)
	for i := range r.Steps {
		r.Steps[i].Stream, r.Steps[i].Output = "", ""
	}
	return r
}

// FlowReceiptsDir is where runs' receipts are kept, under the session
// storage.
func FlowReceiptsDir(storageDir string) string {
	return filepath.Join(storageDir, "flows", "receipts")
}

// liveReceipt is a running run's receipt, as it changes.
type liveReceipt struct {
	mu      sync.Mutex
	r       FlowReceipt
	path    string
	version int
	saved   time.Time
	cancel  context.CancelFunc
	// meters are the running steps' (by ID): what their calls spent so far.
	meters map[string]*stepMeter
}

// liveRuns are the runs this process is running, by run ID: a receipt on
// disk that says running but isn't here is a run that was lost.
var liveRuns sync.Map // string → *liveReceipt

// beginReceipt starts (or, resuming a paused run, carries on) run r's
// receipt, its run stoppable through cancel.
func (w *Workspace) beginReceipt(r *flowRun, origin string, cancel context.CancelFunc) *liveReceipt {
	dir := FlowReceiptsDir(w.cfg.Session.StorageDir)
	path := filepath.Join(dir, r.runID+".json")
	lr := &liveReceipt{path: path, cancel: cancel, meters: map[string]*stepMeter{}}
	now := time.Now()
	if old, err := readReceipt(path); err == nil {
		lr.r = old
		lr.r.Status, lr.r.Ended, lr.r.Error = RunRunning, time.Time{}, ""
		lr.r.Events = append(lr.r.Events, ReceiptEvent{At: now, Kind: "run", Text: "resumed"})
	} else {
		pruneReceipts(dir)
		lr.r = FlowReceipt{Run: r.runID, Workspace: w.Dir(), Workflow: r.f.Name, Origin: cmp.Or(origin, "chat"), Session: r.session,
			Input: textutil.Ellipsize(r.input, flowStepPreview), Started: now, Status: RunRunning,
			Events: []ReceiptEvent{{At: now, Kind: "run", Text: "started"}}}
	}
	liveRuns.Store(r.runID, lr)
	lr.mu.Lock()
	lr.saveLocked()
	lr.mu.Unlock()
	return lr
}

// readReceipt reads a receipt file.
func readReceipt(path string) (FlowReceipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FlowReceipt{}, err
	}
	var r FlowReceipt
	return r, json.Unmarshal(data, &r)
}

// pruneReceipts removes the oldest receipts in dir beyond keptReceipts.
func pruneReceipts(dir string) {
	entries, _ := os.ReadDir(dir)
	type file struct {
		name string
		mod  time.Time
	}
	var files []file
	for _, e := range entries {
		if info, err := e.Info(); err == nil && strings.HasSuffix(e.Name(), ".json") {
			files = append(files, file{e.Name(), info.ModTime()})
		}
	}
	if len(files) < keptReceipts {
		return
	}
	slices.SortFunc(files, func(a, b file) int { return b.mod.Compare(a.mod) })
	for _, f := range files[keptReceipts-1:] {
		os.Remove(filepath.Join(dir, f.name))
	}
}

// saveLocked writes the receipt (through a temporary file, so a reader
// never sees half of one). Call with mu held.
func (lr *liveReceipt) saveLocked() {
	lr.version++
	if err := os.MkdirAll(filepath.Dir(lr.path), 0o700); err != nil {
		return
	}
	snap := lr.snapshotLocked()
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return
	}
	tmp := lr.path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, lr.path)
	}
	lr.saved = time.Now()
}

// snapshotLocked is the receipt now, the running steps' spending so far
// in it. Call with mu held.
func (lr *liveReceipt) snapshotLocked() FlowReceipt {
	r := lr.r
	r.Steps = slices.Clone(r.Steps)
	r.Events = slices.Clone(r.Events)
	for i := range r.Steps {
		if m := lr.meters[r.Steps[i].ID]; m != nil && r.Steps[i].Ended.IsZero() {
			r.Steps[i].Usage, r.Steps[i].Models, r.Steps[i].Attempts = m.read()
		}
	}
	r.Usage, r.Models = api.Usage{Priced: true}, nil
	for _, s := range r.Steps {
		r.Usage.Add(s.Usage)
		for _, m := range s.Models {
			if !slices.Contains(r.Models, m) {
				r.Models = append(r.Models, m)
			}
		}
	}
	return r
}

// snapshot is the receipt now, and its version (which changes with it).
func (lr *liveReceipt) snapshot() (FlowReceipt, int) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	return lr.snapshotLocked(), lr.version
}

// event adds an event, keeping the latest receiptEvents.
func (lr *liveReceipt) eventLocked(e ReceiptEvent) {
	lr.r.Events = append(lr.r.Events, e)
	if over := len(lr.r.Events) - receiptEvents; over > 0 {
		lr.r.Events = slices.Delete(lr.r.Events, 0, over)
	}
}

// openStep is the index of step id's run that hasn't ended, or -1.
func (lr *liveReceipt) openStep(id string) int {
	for i := len(lr.r.Steps) - 1; i >= 0; i-- {
		if s := lr.r.Steps[i]; s.ID == id && s.Ended.IsZero() {
			return i
		}
	}
	return -1
}

// meter registers step id's meter as it starts.
func (lr *liveReceipt) meter(id string, m *stepMeter) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	lr.meters[id] = m
}

// step records a step event: a step starting adds its run, waiting or
// ending updates it (a step skipped, which never started, is added
// ended).
func (lr *liveReceipt) step(s *api.FlowStep, agent string) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	now := time.Now()
	i := lr.openStep(s.ID)
	if i < 0 {
		lr.r.Steps = append(lr.r.Steps, StepReceipt{ID: s.ID, Title: s.Title, Type: s.Type, Started: now, Agent: agent})
		i = len(lr.r.Steps) - 1
	}
	st := &lr.r.Steps[i]
	st.Status = s.Status
	text := s.Status
	switch s.Status {
	case api.StepDone, api.StepFailed, api.StepSkipped:
		st.Ended = now
		if m := lr.meters[s.ID]; m != nil {
			st.Usage, st.Models, st.Attempts = m.read()
			delete(lr.meters, s.ID)
		}
		st.Output, st.Error = s.Output, s.Error
		if s.Error != "" {
			text += ": " + s.Error
		}
	}
	lr.eventLocked(ReceiptEvent{At: now, Step: s.ID, Kind: "step", Text: text, Error: s.Status == api.StepFailed})
	lr.saveLocked()
}

// note records a notice, a called workflow's step or a tool call as an
// event.
func (lr *liveReceipt) note(step, kind, text string, isErr bool) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	lr.eventLocked(ReceiptEvent{At: time.Now(), Step: step, Kind: kind, Text: text, Error: isErr})
	lr.saveLocked()
}

// stream adds text a step's turn streamed (kept in memory, written with
// the next change or beat).
func (lr *liveReceipt) stream(id, text string) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	i := lr.openStep(id)
	if i < 0 || len(lr.r.Steps[i].Stream) >= receiptStream {
		return
	}
	s := lr.r.Steps[i].Stream + text
	if len(s) > receiptStream {
		s = textutil.Ellipsize(s, receiptStream)
	}
	lr.r.Steps[i].Stream = s
	lr.version++
	if time.Since(lr.saved) > receiptBeat {
		lr.saveLocked()
	}
}

// end records how the run ended (status RunWaiting: it paused) and lets
// it go from the live runs.
func (lr *liveReceipt) end(status, output string, err error) {
	lr.mu.Lock()
	now := time.Now()
	lr.r.Status, lr.r.Output = status, textutil.Ellipsize(output, flowStepPreview)
	if status != RunWaiting {
		lr.r.Ended = now
	}
	text := status
	if err != nil {
		lr.r.Error = err.Error()
		text += ": " + err.Error()
	}
	// A step a stopped run left running ended with it.
	for i := range lr.r.Steps {
		if s := &lr.r.Steps[i]; s.Ended.IsZero() && s.Status == api.StepRunning {
			if m := lr.meters[s.ID]; m != nil {
				s.Usage, s.Models, s.Attempts = m.read()
			}
			s.Status, s.Ended = api.StepFailed, now
			s.Error = cmp.Or(s.Error, status)
		}
	}
	lr.meters = map[string]*stepMeter{}
	lr.eventLocked(ReceiptEvent{At: now, Kind: "run", Text: text, Error: err != nil})
	lr.saveLocked()
	run := lr.r.Run
	lr.mu.Unlock()
	liveRuns.CompareAndDelete(run, lr)
}

// beat writes a running receipt now and then, until done closes.
func (lr *liveReceipt) beat(done <-chan struct{}) {
	t := time.NewTicker(receiptBeat)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			lr.mu.Lock()
			lr.saveLocked()
			lr.mu.Unlock()
		}
	}
}

// stepMeter adds up what a step's model calls spend: its turns' usage, a
// rule's judge's, an extract step's. A called workflow's steps add to the
// call step's too.
type stepMeter struct {
	mu       sync.Mutex
	usage    api.Usage
	models   []string
	attempts int
	parent   *stepMeter
}

type meterKey struct{}

// withMeter has ctx's model calls add to m (and to the meter it's in).
func withMeter(ctx context.Context, m *stepMeter) context.Context {
	m.parent, _ = ctx.Value(meterKey{}).(*stepMeter)
	return context.WithValue(ctx, meterKey{}, m)
}

// meterUsage adds a model call's usage to ctx's step meter, if it has one.
func meterUsage(ctx context.Context, model string, u api.Usage) {
	for m, _ := ctx.Value(meterKey{}).(*stepMeter); m != nil; m = m.parent {
		m.add(model, u)
	}
}

func (m *stepMeter) add(model string, u api.Usage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.usage.Calls == 0 && len(m.models) == 0 {
		m.usage.Priced = true
	}
	m.usage.Add(u)
	if model != "" && !slices.Contains(m.models, model) {
		m.models = append(m.models, model)
	}
}

func (m *stepMeter) try() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts++
}

func (m *stepMeter) read() (api.Usage, []string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.usage
	if u.Calls == 0 {
		u.Priced = true // nothing spent is no unknown price
	}
	return u, slices.Clone(m.models), m.attempts
}

// usageSince is what was spent between two of a session's usages.
func usageSince(before, after api.Usage) api.Usage {
	return api.Usage{
		Calls: after.Calls - before.Calls, Input: after.Input - before.Input, Cached: after.Cached - before.Cached,
		CacheWrite: after.CacheWrite - before.CacheWrite, Output: after.Output - before.Output, LastPrompt: after.LastPrompt,
		CostUSD: after.CostUSD - before.CostUSD, Priced: after.Priced,
		SearchQueries: after.SearchQueries - before.SearchQueries, SearchCostUSD: after.SearchCostUSD - before.SearchCostUSD,
	}
}

// ErrNoRun is a run with no receipt.
var ErrNoRun = errors.New("no such workflow run")

// FlowReceipts are the receipts kept in storageDir, newest first: one
// workspace's ("" every one's), one workflow's ("" every one's). A
// running run's is as it is now; one that says running that this process
// isn't running and that hasn't been written for a while was lost (the
// service stopped while it ran), and says so.
func FlowReceipts(storageDir, workspace, workflow string) []FlowReceipt {
	dir := FlowReceiptsDir(storageDir)
	entries, _ := os.ReadDir(dir)
	var out []FlowReceipt
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		r, ok := receiptAt(filepath.Join(dir, e.Name()))
		if !ok || (workspace != "" && r.Workspace != workspace) || (workflow != "" && r.Workflow != workflow) {
			continue
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b FlowReceipt) int { return b.Started.Compare(a.Started) })
	return out
}

// RecentFlowReceipts are the receipts in storageDir of runs going (or
// waiting) and of those written since, newest first: one workspace's
// ("" every one's). It reads only those files.
func RecentFlowReceipts(storageDir, workspace string, since time.Time) []FlowReceipt {
	dir := FlowReceiptsDir(storageDir)
	entries, _ := os.ReadDir(dir)
	paused := map[string]bool{}
	if recs, err := os.ReadDir(FlowRecordsDir(storageDir)); err == nil {
		for _, e := range recs {
			paused[strings.TrimSuffix(e.Name(), ".json")] = true
		}
	}
	var out []FlowReceipt
	for _, e := range entries {
		run, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		_, live := liveRuns.Load(run)
		if info, err := e.Info(); err != nil || !live && !paused[run] && info.ModTime().Before(since) {
			continue
		}
		if r, ok := receiptAt(filepath.Join(dir, e.Name())); ok && (workspace == "" || r.Workspace == workspace) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b FlowReceipt) int { return b.Started.Compare(a.Started) })
	return out
}

// receiptAt reads the receipt at path, as it is now (see FlowReceipts).
func receiptAt(path string) (FlowReceipt, bool) {
	run := strings.TrimSuffix(filepath.Base(path), ".json")
	if v, ok := liveRuns.Load(run); ok {
		r, _ := v.(*liveReceipt).snapshot()
		return r, true
	}
	r, err := readReceipt(path)
	if err != nil {
		return FlowReceipt{}, false
	}
	if r.Status == RunRunning {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > receiptStale {
			r.Status, r.Error = RunStopped, "the service stopped while it ran"
			if r.Ended.IsZero() {
				r.Ended = info.ModTime()
			}
		}
	}
	return r, true
}

// FlowReceiptOf is run's receipt as it is now, and a version that changes
// with it while it runs (0 for one that isn't running here).
func FlowReceiptOf(storageDir, run string) (FlowReceipt, int, error) {
	if strings.ContainsAny(run, `/\`) || run == "" || run == "." || run == ".." {
		return FlowReceipt{}, 0, ErrNoRun
	}
	if v, ok := liveRuns.Load(run); ok {
		r, ver := v.(*liveReceipt).snapshot()
		return r, ver, nil
	}
	r, ok := receiptAt(filepath.Join(FlowReceiptsDir(storageDir), run+".json"))
	if !ok {
		return FlowReceipt{}, 0, ErrNoRun
	}
	return r, 0, nil
}

// StopFlowRun stops a run this process is running.
func StopFlowRun(run string) bool {
	v, ok := liveRuns.Load(run)
	if !ok {
		return false
	}
	v.(*liveReceipt).cancel()
	return true
}
