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
	"sync"

	"github.com/google/uuid"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/flows"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/textutil"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/workflowagent"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

// A workflow's graph runs on ADK's workflow engine (spec_workflows_043
// WF-11), in an orchestration session of its own kept on disk
// (<sessions>/flows), which schedules the steps, joins branches and
// will pause and resume runs. Each step is a node; an agent step's node
// runs a turn of ours in the chat's session, so it keeps everything a
// turn has.

// Node names the compiler adds: a join before a step fed by several,
// and the end joining several last steps.
const (
	joinPrefix = "join__"
	endNode    = "end__"
)

// flowSessions are the orchestration sessions, opened on first use.
func (w *Workspace) flowSessions() (adksession.Service, error) {
	w.flowMu.Lock()
	defer w.flowMu.Unlock()
	if w.flowStore == nil {
		s, err := session.NewPersistentService(filepath.Join(w.cfg.Session.StorageDir, "flows"))
		if err != nil {
			return nil, err
		}
		w.flowStore = s
	}
	return w.flowStore, nil
}

// stepFailure is a step that failed, and why.
type stepFailure struct {
	title string
	err   error
}

func (e *stepFailure) Error() string { return e.err.Error() }
func (e *stepFailure) Unwrap() error { return e.err }

// flowRun is one run's state, shared by its nodes: their outputs, the
// steps' titles, and what serialises the chat's turns and its events.
type flowRun struct {
	f       flows.Flow
	order   []flows.Node
	outputs sync.Map // step ID → its output
	turns   sync.Mutex
	events  sync.Mutex
	on      func(api.Event)
}

func (r *flowRun) emit(e api.Event) {
	r.events.Lock()
	defer r.events.Unlock()
	r.on(e)
}

func (r *flowRun) title(id string) string {
	i := slices.IndexFunc(r.f.Nodes, func(n flows.Node) bool { return n.ID == id })
	if i < 0 || r.f.Nodes[i].Title == "" {
		return id
	}
	return r.f.Nodes[i].Title
}

// compileFlow builds f's graph: a node per step, a join before each step
// fed by several, and one end when several steps feed none.
func (w *Workspace) compileFlow(run *flowRun, sessionID string) ([]workflow.Edge, error) {
	nodes := map[string]workflow.Node{}
	for i, n := range run.order {
		nodes[n.ID] = workflow.NewEmittingFunctionNode(n.ID, w.stepNode(run, sessionID, n, i), workflow.NodeConfig{})
	}
	b := workflow.NewEdgeBuilder()
	var last []workflow.Node
	for _, n := range run.order {
		from := flows.Inputs(run.f, n.ID)
		switch len(from) {
		case 0:
			b.Add(workflow.Start, nodes[n.ID])
		case 1:
			b.Add(nodes[from[0]], nodes[n.ID])
		default:
			join := workflow.NewJoinNode(joinPrefix + n.ID)
			for _, id := range from {
				b.Add(nodes[id], join)
			}
			b.Add(join, nodes[n.ID])
		}
		if !slices.ContainsFunc(run.f.Edges, func(e flows.Edge) bool { return e.From == n.ID }) {
			last = append(last, nodes[n.ID])
		}
	}
	if len(last) > 1 {
		end := workflow.NewJoinNode(endNode)
		for _, n := range last {
			b.Add(n, end)
		}
	}
	return b.Build(), nil
}

// stepNode is a step's node: it says the step started, gathers its input
// (the run's, one step's output, or a join's), runs it, and says how it
// ended.
func (w *Workspace) stepNode(run *flowRun, sessionID string, n flows.Node, index int) workflow.EmittingFunctionFn[any, any] {
	return func(ctx agent.Context, in any, _ func(*adksession.Event) error) (any, error) {
		step := api.FlowStep{Workflow: run.f.Name, ID: n.ID, Title: n.Title, Type: n.Type, Index: index + 1, Count: len(run.order), Status: api.StepRunning}
		run.emit(api.Event{Step: &step})
		input := stepInput(run, n, in)
		var out string
		var err error
		if n.Type == flows.Agent || n.Type == flows.Refine || n.Type == flows.Summarize || n.Type == flows.Browser {
			// The chat's turns go one at a time, even from parallel branches.
			run.turns.Lock()
			out, err = w.runStep(ctx, sessionID, run.f, n, input, run.emit)
			run.turns.Unlock()
		} else {
			out, err = w.runStep(ctx, sessionID, run.f, n, input, run.emit)
		}
		if err != nil {
			step.Status, step.Error = api.StepFailed, err.Error()
			run.emit(api.Event{Step: &step})
			return nil, &stepFailure{title: run.title(n.ID), err: err}
		}
		run.outputs.Store(n.ID, out)
		step.Status, step.Output = api.StepDone, textutil.Ellipsize(out, flowStepPreview)
		run.emit(api.Event{Step: &step})
		return out, nil
	}
}

// stepInput is a step's input: the run's for a step fed by none, the
// output of the one that feeds it, or (several, or accumulate) each
// under its step's title, in the order the links are listed.
func stepInput(run *flowRun, n flows.Node, in any) string {
	from := flows.Inputs(run.f, n.ID)
	if len(from) == 0 {
		s, _ := in.(string)
		return s
	}
	var parts []string
	for _, id := range from {
		v, _ := run.outputs.Load(id)
		s, _ := v.(string)
		if n.Type == flows.Accumulate || len(from) > 1 {
			parts = append(parts, fmt.Sprintf("## %s\n\n%s", run.title(id), s))
		} else {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

// runFlow runs workflow f on input in sessionID (WF-03, WF-04): its graph
// on ADK's engine, each step as its node, stopping at the first that
// fails. The result's output is what the last steps (those that feed
// none) produced.
func (w *Workspace) runFlow(ctx context.Context, sessionID string, f flows.Flow, input string, on func(api.Event)) (api.TurnResult, error) {
	if f.Problem != "" {
		return api.TurnResult{}, fmt.Errorf("the workflow %s can't run: %s", f.Name, f.Problem)
	}
	order, err := flows.Order(f)
	if err != nil {
		return api.TurnResult{}, err
	}
	res := api.TurnResult{Before: w.engine.Usage(sessionID)}
	run := &flowRun{f: f, order: order, on: on}
	edges, err := w.compileFlow(run, sessionID)
	if err != nil {
		return res, err
	}
	root, err := workflowagent.New(workflowagent.Config{Name: "flow_" + strings.ReplaceAll(f.Name, "-", "_"), Description: f.Description, Edges: edges})
	if err != nil {
		return res, fmt.Errorf("the workflow %s: %w", f.Name, err)
	}
	store, err := w.flowSessions()
	if err != nil {
		return res, err
	}
	r, err := runner.New(runner.Config{AppName: "blitz-flows", Agent: root, SessionService: store, AutoCreateSession: true})
	if err != nil {
		return res, err
	}
	var runErr error
	for _, err := range r.Run(ctx, "blitz", "flow-"+uuid.NewString(), genai.NewContentFromText(input, genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			runErr = err
			break
		}
	}
	res.After = w.engine.Usage(sessionID)
	if runErr != nil {
		var failed *stepFailure
		if errors.As(runErr, &failed) {
			return res, fmt.Errorf("the workflow %s stopped at %s: %w", f.Name, failed.title, failed.err)
		}
		return res, fmt.Errorf("the workflow %s: %w", f.Name, runErr)
	}
	var last []string
	for _, n := range order {
		if !slices.ContainsFunc(f.Edges, func(e flows.Edge) bool { return e.From == n.ID }) {
			v, _ := run.outputs.Load(n.ID)
			s, _ := v.(string)
			last = append(last, s)
		}
	}
	res.Output = strings.Join(last, "\n\n")
	return res, nil
}
