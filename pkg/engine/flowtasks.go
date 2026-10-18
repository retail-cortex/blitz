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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"google.golang.org/adk/v2/agent"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

// People and time (spec_workflows_043 WF-16): a human task pauses its run
// until someone fills in its form, a timer until its time. The run's
// state is in its orchestration session (ADK replays finished steps when
// it resumes); a record of the paused run says where to resume it: its
// workflow (and the file's hash: one changed since isn't resumed), its
// chat, its input and what it waits on.

// FlowTask is what a paused run waits on.
type FlowTask struct {
	// ID is the task's (the run's ID and the step's).
	ID    string `json:"id"`
	Step  string `json:"step"`
	Title string `json:"title"`
	// Kind is human_task or timer.
	Kind string `json:"kind"`
	// Message (a human task's instructions) and Form are what's shown.
	Message string      `json:"message,omitempty"`
	Form    *flows.Form `json:"form,omitempty"`
	// FireAt is when a timer resumes the run.
	FireAt  time.Time `json:"fire_at,omitzero"`
	Created time.Time `json:"created"`
}

// FlowRecord is a paused run.
type FlowRecord struct {
	Run       string `json:"run"`
	Workspace string `json:"workspace"`
	Workflow  string `json:"workflow"`
	Hash      string `json:"hash"`
	// Session is the chat its agent steps are turns in; Origin is chat,
	// background or worker.
	Session string     `json:"session"`
	Origin  string     `json:"origin"`
	Input   string     `json:"input"`
	Started time.Time  `json:"started"`
	Tasks   []FlowTask `json:"tasks"`
	// Done are the steps that finished before it paused, replayed as it
	// resumes.
	Done map[string]Replayed `json:"done,omitempty"`
}

// FlowRecordsDir is where paused runs are recorded, under the session
// storage (shared by every workspace, so the service and the tray list
// every task).
func FlowRecordsDir(storageDir string) string { return filepath.Join(storageDir, "flows", "runs") }

// ListFlowRecords are the paused runs recorded in dir (FlowRecordsDir),
// oldest first; workspace "" is every workspace's.
func ListFlowRecords(dir, workspace string) []FlowRecord {
	entries, _ := os.ReadDir(dir)
	var out []FlowRecord
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r FlowRecord
		if json.Unmarshal(data, &r) != nil || (workspace != "" && r.Workspace != workspace) {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

// DueTimers are the paused runs in dir with a timer due by now: each with
// that timer's task ID.
func DueTimers(dir string, now time.Time) map[string]FlowRecord {
	out := map[string]FlowRecord{}
	for _, r := range ListFlowRecords(dir, "") {
		for _, t := range r.Tasks {
			if t.Kind == flows.Timer && !t.FireAt.After(now) {
				out[t.ID] = r
			}
		}
	}
	return out
}

func (w *Workspace) recordPath(runID string) string {
	return filepath.Join(FlowRecordsDir(w.cfg.Session.StorageDir), runID+".json")
}

// forgetRun deletes a finished run's record.
func (w *Workspace) forgetRun(runID string) { os.Remove(w.recordPath(runID)) }

// flowHash identifies a workflow file as it is.
func flowHash(f flows.Flow) string {
	data, _ := os.ReadFile(f.Path)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// flowOriginKey says what started a workflow's run: chat, background, worker.
type flowOriginKey struct{}

func withFlowOrigin(ctx context.Context, origin string) context.Context {
	return context.WithValue(ctx, flowOriginKey{}, origin)
}

// pauseRun records a paused run (keeping when it started, if it paused
// before) and says in the chat what it waits on.
func (w *Workspace) pauseRun(ctx context.Context, r *flowRun, tasks []FlowTask, done map[string]Replayed) error {
	origin, _ := ctx.Value(flowOriginKey{}).(string)
	rec := FlowRecord{Run: r.runID, Workspace: w.Dir(), Workflow: r.f.Name, Hash: flowHash(r.f), Session: r.session, Origin: cmpOr(origin, "chat"), Input: r.input, Started: time.Now(), Tasks: tasks, Done: done}
	if old, err := w.loadRecord(r.runID); err == nil {
		rec.Started = old.Started
	}
	if err := os.MkdirAll(filepath.Dir(w.recordPath(r.runID)), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(w.recordPath(r.runID), data, 0o600); err != nil {
		return err
	}
	for _, t := range tasks {
		text := i18n.T("flow.waits_task", "workflow", r.f.Name, "title", t.Title)
		if t.Kind == flows.Timer {
			text = i18n.T("flow.waits_timer", "workflow", r.f.Name, "time", t.FireAt.Local().Format("Mon 2 Jan 15:04"))
		}
		r.emit(api.Event{Notice: &api.Notice{Text: text}})
	}
	return nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (w *Workspace) loadRecord(runID string) (FlowRecord, error) {
	var rec FlowRecord
	data, err := os.ReadFile(w.recordPath(runID))
	if err != nil {
		return rec, fmt.Errorf("%w: %s", ErrNoTask, runID)
	}
	return rec, json.Unmarshal(data, &rec)
}

// ErrNoTask means there's no such paused run or task.
var ErrNoTask = errors.New("no such task: it may have been answered already")

// pauseStep runs a human task or a timer (WF-16): the first time it asks
// ADK for input and the run pauses; run again with the answer (the form's
// values, or the timer's time), that's its result.
func (w *Workspace) pauseStep(ctx agent.Context, r *flowRun, n flows.Node, vars flows.Vars, emit func(*adksession.Event) error) (flows.Result, error) {
	filled, err := fillStep(n, vars)
	if err != nil {
		return flows.Result{}, err
	}
	task := FlowTask{ID: r.runID + "/" + n.ID, Step: n.ID, Title: cmpOr(n.Title, n.ID), Kind: n.Type, Message: filled.Instructions, Form: n.Form, Created: time.Now()}
	if n.Type == flows.Timer {
		if task.FireAt, err = fireAt(filled); err != nil {
			s := r.step(n, api.StepFailed)
			s.Error = err.Error()
			r.emit(api.Event{Step: s})
			return flows.Result{}, &stepFailure{title: task.Title, err: err}
		}
	}
	reply, err := workflow.ResumeOrRequestInput(ctx, emit, adksession.RequestInput{InterruptID: task.ID, Message: task.Title, Payload: map[string]any{"kind": task.Kind, "fire_at": task.FireAt}})
	if errors.Is(err, workflow.ErrNodeInterrupted) {
		r.mu.Lock()
		r.waiting = append(r.waiting, task)
		r.mu.Unlock()
		r.emit(api.Event{Step: r.step(n, api.StepWaiting)})
		return flows.Result{}, err
	}
	if err != nil {
		return flows.Result{}, err
	}
	res := answerResult(n, reply)
	s := r.step(n, api.StepDone)
	s.Output = res.Text
	r.emit(api.Event{Step: s})
	return res, nil
}

// fireAt is when a timer resumes its run: its wait from now, or its until.
func fireAt(n flows.Node) (time.Time, error) {
	if n.Until != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(n.Until))
		if err != nil {
			return time.Time{}, fmt.Errorf("until %q: a time like 2026-10-20T09:00:00Z", n.Until)
		}
		return t, nil
	}
	d, err := flows.ParseDuration(n.Wait, 0)
	if err != nil {
		return time.Time{}, err
	}
	return time.Now().Add(d), nil
}

// answerResult is a human task's or a timer's result from its answer: the
// form's values as data, and as "name: value" lines.
func answerResult(n flows.Node, reply any) flows.Result {
	m, _ := reply.(map[string]any)
	if p, ok := m["payload"].(map[string]any); ok {
		m = p
	}
	if n.Type == flows.Timer {
		return flows.Result{Text: fmt.Sprint(m["fired_at"]), Data: m}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var lines []string
	for _, k := range keys {
		v, _ := json.Marshal(m[k])
		lines = append(lines, fmt.Sprintf("%s: %s", k, strings.Trim(string(v), `"`)))
	}
	return flows.Result{Text: strings.Join(lines, "\n"), Data: m}
}

// ResumeFlow answers a paused run's task and runs it on from there (the
// form's values, or {fired_at} for a timer), its agent steps in the chat
// it started in. A run whose workflow changed since it paused isn't
// resumed; its record goes.
func (w *Workspace) ResumeFlow(ctx context.Context, taskID string, answer map[string]any, on func(api.Event)) (api.TurnResult, error) {
	runID, _, _ := strings.Cut(taskID, "/")
	rec, err := w.loadRecord(runID)
	if err != nil {
		return api.TurnResult{}, err
	}
	if !slices.ContainsFunc(rec.Tasks, func(t FlowTask) bool { return t.ID == taskID }) {
		return api.TurnResult{}, fmt.Errorf("%w: %s", ErrNoTask, taskID)
	}
	f, ok := w.workflow(rec.Workflow)
	if !ok || flowHash(f) != rec.Hash {
		w.forgetRun(runID)
		return api.TurnResult{}, fmt.Errorf("the workflow %s changed since its run paused: it can't be resumed; run it again", rec.Workflow)
	}
	// The task is answered: the record keeps the others.
	rec.Tasks = slices.DeleteFunc(rec.Tasks, func(t FlowTask) bool { return t.ID == taskID })
	if data, err := json.MarshalIndent(rec, "", "  "); err == nil {
		_ = os.WriteFile(w.recordPath(runID), data, 0o600)
	}
	ctx = withFlowOrigin(ctx, rec.Origin)
	if st := w.storage; st.Active() == nil || st.Active().ID != rec.Session {
		other, err := session.NewStorage(w.cfg.Session.StorageDir)
		if err != nil {
			return api.TurnResult{}, err
		}
		other.SetWorkspace(w.Dir())
		defer other.Release()
		if _, err := other.Load(rec.Session); err != nil {
			// A chat with nothing in it yet (the run paused first) isn't on
			// disk: it starts now, under its ID.
			created, cerr := other.CreateSession(rec.Session, "/"+rec.Workflow, w.engine.ActiveAgent())
			if cerr != nil {
				return api.TurnResult{}, fmt.Errorf("the run's chat: %w", err)
			}
			created.Origin = session.OriginBackground
		}
		ctx = withFlowStorage(ctx, other)
	}
	resp := &genai.FunctionResponse{ID: taskID, Name: workflow.WorkflowInputFunctionCallName, Response: map[string]any{"payload": answer}}
	return w.driveFlow(ctx, rec.Session, f, rec.Input, on, runID, resp, rec.Done)
}

// FlowTasks are the tasks the workspace's paused runs wait on, with their
// runs, oldest first.
func (w *Workspace) FlowTasks() []FlowRecord {
	return ListFlowRecords(FlowRecordsDir(w.cfg.Session.StorageDir), w.Dir())
}

// StorageDir is where the workspace keeps sessions (and paused runs'
// records, under it).
func (w *Workspace) StorageDir() string { return w.cfg.Session.StorageDir }
