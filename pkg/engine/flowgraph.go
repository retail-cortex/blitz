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
	"time"

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

// A workflow runs on ADK's workflow engine (spec_workflows_043 WF-11), in
// an orchestration session of its own kept on disk (<sessions>/flows):
// one dynamic node schedules the steps as BPMN does with tokens (WF-12),
// running each through workflow.RunNode, which keeps its result there so
// a resumed run replays finished steps rather than running them again.
// An agent step's node runs a turn of ours in the chat's session, so it
// keeps everything a turn has.

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

// The longest a retry waits.
var flowRetryCap = time.Minute

// flowRun is one run's state: its workflow, its input, what serialises
// the chat's turns and the events.
type flowRun struct {
	f       flows.Flow
	order   []flows.Node
	input   string
	session string
	turns   sync.Mutex
	events  sync.Mutex
	on      func(api.Event)
	// failures are the steps' own failures, by ID (ADK keeps only their
	// messages).
	failures sync.Map
}

func (r *flowRun) emit(e api.Event) {
	r.events.Lock()
	defer r.events.Unlock()
	r.on(e)
}

func (r *flowRun) title(id string) string {
	for _, n := range r.f.Nodes {
		if n.ID == id && n.Title != "" {
			return n.Title
		}
	}
	return id
}

func (r *flowRun) index(id string) int {
	return slices.IndexFunc(r.order, func(n flows.Node) bool { return n.ID == id }) + 1
}

func (r *flowRun) step(n flows.Node, status string) *api.FlowStep {
	return &api.FlowStep{Workflow: r.f.Name, ID: n.ID, Title: n.Title, Type: n.Type, Index: r.index(n.ID), Count: len(r.order), Status: status}
}

// Link states while a run goes.
const (
	linkOpen = iota
	linkTaken
	linkDead
)

// stepDone is a step's end, for the scheduler.
type stepDone struct {
	id     string
	result flows.Result
	err    error
}

// schedule runs r's steps (WF-12): each runs once every link into it is
// taken or dead and one is taken (or it has none: the run's input feeds
// it); its result decides its links out (a gateway's or a link's
// conditions, an error link when it fails); a step every link into which
// is dead is skipped, and its links out die too. Steps that can run go
// side by side. A failure without an error link stops the run.
func (w *Workspace) schedule(ctx agent.Context, r *flowRun) (map[string]flows.Result, error) {
	links := make([]int, len(r.f.Edges))
	results := map[string]flows.Result{}
	state := map[string]string{} // ID → running, done, skipped
	in := func(id string) []int {
		var out []int
		for i, e := range r.f.Edges {
			if e.To == id {
				out = append(out, i)
			}
		}
		return out
	}
	var kill func(i int)
	kill = func(i int) {
		links[i] = linkDead
		to := r.f.Edges[i].To
		if state[to] != "" {
			return
		}
		for _, j := range in(to) {
			if links[j] != linkDead {
				return
			}
		}
		state[to] = api.StepSkipped
		n := r.order[r.index(to)-1]
		r.emit(api.Event{Step: r.step(n, api.StepSkipped)})
		for j, e := range r.f.Edges {
			if e.From == to {
				kill(j)
			}
		}
	}
	ready := func(id string) bool {
		if state[id] != "" {
			return false
		}
		taken := false
		for _, j := range in(id) {
			switch links[j] {
			case linkOpen:
				return false
			case linkTaken:
				taken = true
			}
		}
		return taken || len(in(id)) == 0
	}
	done := make(chan stepDone)
	running := 0
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	start := func(n flows.Node) {
		state[n.ID] = "running"
		running++
		input := w.stepInputOf(r, n, in(n.ID), links, results)
		vars := flows.Vars{Input: r.input, Steps: copyResults(results)}
		node := workflow.NewFunctionNode(n.ID, func(actx agent.Context, text string) (flows.Result, error) {
			// Stopped with the run when another step fails.
			sctx, stop := context.WithCancel(actx)
			defer stop()
			defer context.AfterFunc(runCtx, stop)()
			return w.runFlowStep(sctx, r, n, text, vars)
		}, workflow.NodeConfig{})
		go func() {
			res, err := workflow.RunNode[flows.Result](ctx, node, input, workflow.WithRunID("step-"+n.ID))
			done <- stepDone{id: n.ID, result: res, err: err}
		}()
	}
	var failure error
	for {
		if failure == nil {
			for _, n := range r.order {
				if ready(n.ID) {
					start(n)
				}
			}
		}
		if running == 0 {
			break
		}
		d := <-done
		running--
		if failure != nil {
			continue // stopping: let the others end
		}
		n := r.order[r.index(d.id)-1]
		out := flows.Outputs(r.f, n.ID)
		if d.err != nil {
			if f, ok := r.failures.Load(n.ID); ok {
				d.err = f.(*stepFailure)
			}
			errLink := slices.IndexFunc(r.f.Edges, func(e flows.Edge) bool { return e.From == n.ID && e.Error })
			if errLink < 0 {
				failure = d.err
				cancel()
				continue
			}
			var sf *stepFailure
			msg := d.err.Error()
			if errors.As(d.err, &sf) {
				msg = sf.err.Error()
			}
			state[n.ID] = api.StepDone
			results[n.ID] = flows.Result{Text: msg, Data: map[string]any{"error": msg}}
			for i, e := range r.f.Edges {
				if e.From == n.ID {
					if i == errLink {
						links[i] = linkTaken
					} else {
						kill(i)
					}
				}
			}
			continue
		}
		state[n.ID] = api.StepDone
		results[n.ID] = d.result
		taken, err := w.route(runCtx, r, n, out, d.result, results)
		if err != nil {
			failure = &stepFailure{title: r.title(n.ID), err: err}
			cancel()
			continue
		}
		for i, e := range r.f.Edges {
			if e.From != n.ID {
				continue
			}
			if taken[i] {
				links[i] = linkTaken
			} else {
				kill(i)
			}
		}
	}
	return results, failure
}

func copyResults(m map[string]flows.Result) map[string]flows.Result {
	out := make(map[string]flows.Result, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// route decides which of n's links out are taken, by index in the
// workflow's links (WF-12): an exclusive gateway its first link whose
// condition holds, else its default; any other step every link without a
// condition and every one whose condition holds, the default when no
// conditional one is taken. Error links aren't taken: it succeeded.
func (w *Workspace) route(ctx context.Context, r *flowRun, n flows.Node, out []flows.Edge, res flows.Result, results map[string]flows.Result) (map[int]bool, error) {
	taken := map[int]bool{}
	vars := flows.Vars{Input: r.input, Output: &res, Steps: copyResults(results)}
	conditional, deflt := false, -1
	for i, e := range r.f.Edges {
		if e.From != n.ID || e.Error {
			continue
		}
		switch {
		case e.Default:
			deflt = i
		case !e.Conditional():
			if n.Type != flows.Exclusive {
				taken[i] = true
			}
		default:
			if n.Type == flows.Exclusive && conditional {
				continue // the first that holds wins
			}
			ok, err := w.holds(ctx, e, vars)
			if err != nil {
				return nil, fmt.Errorf("the link to %s: %w", r.title(e.To), err)
			}
			if ok {
				taken[i] = true
				conditional = true
			}
		}
	}
	if !conditional && deflt >= 0 {
		taken[deflt] = true
	}
	if n.Type == flows.Exclusive && !conditional && deflt < 0 {
		// No condition held and no default: an unconditioned link is the way on.
		for i, e := range r.f.Edges {
			if e.From == n.ID && !e.Error && !e.Conditional() && !e.Default {
				taken[i] = true
				break
			}
		}
	}
	return taken, nil
}

// holds evaluates a link's condition: CEL, or plain English judged by the
// suggestions model on the step's output.
func (w *Workspace) holds(ctx context.Context, e flows.Edge, vars flows.Vars) (bool, error) {
	if e.When != "" {
		return flows.Holds(e.When, vars)
	}
	return w.judgeCondition(ctx, e.If, vars.Output.Text)
}

// stepInputOf is a step's input: the run's for a step fed by none, the
// output of the one taken link into it, or (several, or accumulate) each
// under its step's title, in the order the links are listed.
func (w *Workspace) stepInputOf(r *flowRun, n flows.Node, into []int, links []int, results map[string]flows.Result) string {
	if len(into) == 0 {
		return r.input
	}
	var from []string
	for _, j := range into {
		if links[j] == linkTaken {
			from = append(from, r.f.Edges[j].From)
		}
	}
	if len(from) == 1 && n.Type != flows.Accumulate {
		return results[from[0]].Text
	}
	var parts []string
	for _, id := range from {
		parts = append(parts, fmt.Sprintf("## %s\n\n%s", r.title(id), results[id].Text))
	}
	return strings.Join(parts, "\n\n")
}

// runFlowStep runs a step as its node does: it says the step started,
// fills in its templates, runs it (trying again as its retry says, held
// to its timeout), and says how it ended.
func (w *Workspace) runFlowStep(ctx context.Context, r *flowRun, n flows.Node, input string, vars flows.Vars) (flows.Result, error) {
	r.emit(api.Event{Step: r.step(n, api.StepRunning)})
	fail := func(err error) (flows.Result, error) {
		s := r.step(n, api.StepFailed)
		s.Error = err.Error()
		r.emit(api.Event{Step: s})
		f := &stepFailure{title: r.title(n.ID), err: err}
		r.failures.Store(n.ID, f)
		return flows.Result{}, f
	}
	filled, err := fillStep(n, vars)
	if err != nil {
		return fail(err)
	}
	attempts, delay := 1, time.Second
	if n.Retry != nil {
		attempts = max(1, n.Retry.Attempts)
		delay, _ = flows.ParseDuration(n.Retry.Delay, time.Second)
	}
	timeout, _ := flows.ParseDuration(n.Timeout, 0)
	var res flows.Result
	for try := 1; ; try++ {
		sctx, cancel := ctx, context.CancelFunc(func() {})
		if timeout > 0 {
			sctx, cancel = context.WithTimeout(ctx, timeout)
		}
		res, err = w.runStepOnce(sctx, r, filled, input)
		cancel()
		if err == nil || try >= attempts || ctx.Err() != nil {
			break
		}
		r.emit(api.Event{Notice: &api.Notice{Text: fmt.Sprintf("↻ /%s › %s: %v (try %d of %d)", r.f.Name, r.title(n.ID), err, try+1, attempts)}})
		select {
		case <-time.After(delay):
		case <-ctx.Done():
		}
		delay = min(delay*2, flowRetryCap)
	}
	if err != nil {
		return fail(err)
	}
	s := r.step(n, api.StepDone)
	s.Output = textutil.Ellipsize(res.Text, flowStepPreview)
	r.emit(api.Event{Step: s})
	return res, nil
}

// fillStep is n with its templates filled in.
func fillStep(n flows.Node, vars flows.Vars) (flows.Node, error) {
	var err error
	if n.Instructions, err = flows.Fill(n.Instructions, vars); err != nil {
		return n, err
	}
	if n.Path, err = flows.Fill(n.Path, vars); err != nil {
		return n, err
	}
	args := make([]string, len(n.Args))
	for i, a := range n.Args {
		if args[i], err = flows.Fill(a, vars); err != nil {
			return n, err
		}
	}
	n.Args = args
	if n.URL, err = flows.Fill(n.URL, vars); err != nil {
		return n, err
	}
	if n.Body, err = flows.Fill(n.Body, vars); err != nil {
		return n, err
	}
	pairs := func(in []flows.Pair) ([]flows.Pair, error) {
		out := make([]flows.Pair, len(in))
		for i, p := range in {
			v, err := flows.Fill(p.Value, vars)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", p.Name, err)
			}
			out[i] = flows.Pair{Name: p.Name, Value: v}
		}
		return out, nil
	}
	if n.Query, err = pairs(n.Query); err != nil {
		return n, err
	}
	if n.Headers, err = pairs(n.Headers); err != nil {
		return n, err
	}
	if n.Arguments != nil {
		v, err := flows.FillValue(n.Arguments, vars)
		if err != nil {
			return n, err
		}
		n.Arguments, _ = v.(map[string]any)
	}
	if n.JSON != nil {
		if n.JSON, err = flows.FillValue(n.JSON, vars); err != nil {
			return n, err
		}
	}
	if n.Auth != nil {
		a := *n.Auth
		for _, f := range []*string{&a.Token, &a.Username, &a.Password, &a.Value, &a.TokenURL, &a.ClientID, &a.ClientSecret, &a.Audience} {
			if *f, err = flows.Fill(*f, vars); err != nil {
				return n, err
			}
		}
		n.Auth = &a
	}
	return n, nil
}

// runStepOnce runs a filled-in step on its input: a gateway passes it on,
// an agent step is a turn in the chat (one at a time).
func (w *Workspace) runStepOnce(ctx context.Context, r *flowRun, n flows.Node, input string) (flows.Result, error) {
	switch {
	case flows.IsGateway(n.Type):
		return flows.Result{Text: input, Data: flows.DataOf(input)}, nil
	case n.Type == flows.Extract:
		return w.extractStep(ctx, n, input)
	case n.Type == flows.MCPCall:
		return w.mcpStep(ctx, n)
	case n.Type == flows.APICall:
		return w.apiStep(ctx, n, input)
	case n.Type == flows.A2ACall:
		return w.a2aStep(ctx, n, input)
	case flows.IsAgentType(n.Type):
		r.turns.Lock()
		defer r.turns.Unlock()
	}
	text, err := w.runStep(ctx, r.session, r.f, n, input, r.emit)
	if err != nil {
		return flows.Result{}, err
	}
	res := flows.Result{Text: text}
	if flows.IsAgentType(n.Type) || n.Type == flows.Script || strings.EqualFold(filepath.Ext(n.Path), ".json") {
		res.Data = flows.DataOf(text)
	}
	return res, nil
}

// runFlow runs workflow f on input in sessionID (WF-03, WF-04, WF-11,
// WF-12). The result's output is what the last steps that ran (those
// that feed none) produced.
func (w *Workspace) runFlow(ctx context.Context, sessionID string, f flows.Flow, input string, on func(api.Event)) (api.TurnResult, error) {
	if f.Problem != "" {
		return api.TurnResult{}, fmt.Errorf("the workflow %s can't run: %s", f.Name, f.Problem)
	}
	order, err := flows.Order(f)
	if err != nil {
		return api.TurnResult{}, err
	}
	res := api.TurnResult{Before: w.engine.Usage(sessionID)}
	run := &flowRun{f: f, order: order, input: input, session: sessionID, on: on}
	var results map[string]flows.Result
	scheduler := workflow.NewDynamicNode("run", func(actx agent.Context, _ string, _ func(*adksession.Event) error) (string, error) {
		var err error
		results, err = w.schedule(actx, run)
		return "", err
	}, workflow.NodeConfig{})
	root, err := workflowagent.New(workflowagent.Config{Name: "flow_" + strings.ReplaceAll(f.Name, "-", "_"), Description: f.Description, Edges: workflow.Chain(workflow.Start, scheduler)})
	if err != nil {
		return res, fmt.Errorf("the workflow %s: %w", f.Name, err)
	}
	store, err := w.flowSessions()
	if err != nil {
		return res, err
	}
	rn, err := runner.New(runner.Config{AppName: "blitz-flows", Agent: root, SessionService: store, AutoCreateSession: true})
	if err != nil {
		return res, err
	}
	var runErr error
	for _, err := range rn.Run(ctx, "blitz", "flow-"+uuid.NewString(), genai.NewContentFromText(input, genai.RoleUser), agent.RunConfig{}) {
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
		if _, ran := results[n.ID]; ran && !slices.ContainsFunc(f.Edges, func(e flows.Edge) bool { return e.From == n.ID && !e.Error }) {
			last = append(last, results[n.ID].Text)
		}
	}
	res.Output = strings.Join(last, "\n\n")
	return res, nil
}
