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

package server

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/api"
)

// Eval runs going on (spec_evals_042 EV-09a): each runs in the service,
// not in the request that started it, so Studio can be left while it goes
// on, and followed or stopped again from anywhere. A run is forgotten
// here once it has ended, by then recorded with the workspace's runs.

// liveEval is an eval run going on.
type liveEval struct {
	workspace string
	cancel    context.CancelFunc
	done      chan struct{}

	mu     sync.Mutex
	begun  api.EvalRun // as it started
	run    api.EvalRun // as it stands: its results so far, then finished
	ended  bool
	notify map[chan struct{}]struct{}
}

// evalRuns are the eval runs going on.
type evalRuns struct {
	mu   sync.Mutex
	list []*liveEval
}

// snapshot is the run as it stands, its results copied.
func (l *liveEval) snapshot() (api.EvalRun, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	run := l.run
	run.Results = slices.Clone(l.run.Results)
	return run, l.ended
}

// update changes the run and tells its watchers.
func (l *liveEval) update(f func(*api.EvalRun)) {
	l.mu.Lock()
	f(&l.run)
	for ch := range l.notify {
		select {
		case ch <- struct{}{}:
		default: // it has a change to read already
		}
	}
	l.mu.Unlock()
}

// subscribe returns a channel that has a value after each change, and a
// function that stops it.
func (l *liveEval) subscribe() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	l.mu.Lock()
	l.notify[ch] = struct{}{}
	l.mu.Unlock()
	return ch, func() {
		l.mu.Lock()
		delete(l.notify, ch)
		l.mu.Unlock()
	}
}

// startEval starts eval name on w, in the service's own context when
// background (else in ctx, so it stops with the request), and returns
// once it has started: an eval that can't run is the error.
func (s *Server) startEval(ctx context.Context, w *workspace, name string, cases []string, background bool) (*liveEval, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, apiError(connect.CodeUnavailable, "SHUTTING_DOWN", errServerClosed)
	}
	parent := ctx
	if background {
		parent = context.Background()
	}
	runCtx, cancel := context.WithCancel(parent)
	l := &liveEval{workspace: w.Dir(), cancel: cancel, done: make(chan struct{}), notify: map[chan struct{}]struct{}{}}
	s.evals.mu.Lock()
	s.evals.list = append(s.evals.list, l)
	s.evals.mu.Unlock()
	s.mu.Unlock()

	started := make(chan error, 1)
	go func() {
		defer close(l.done)
		defer cancel()
		var once sync.Once
		run, err := w.RunEval(runCtx, name, api.EvalOptions{
			Cases: cases,
			OnStart: func(run api.EvalRun) {
				l.update(func(r *api.EvalRun) { l.begun, *r = run, run })
				once.Do(func() { started <- nil })
			},
			OnResult: func(x api.EvalResult) {
				l.update(func(r *api.EvalRun) { r.Results = append(r.Results, x) })
			},
		})
		once.Do(func() { started <- err }) // it never started
		if err == nil {
			l.update(func(r *api.EvalRun) { *r = run })
		}
		s.evals.mu.Lock()
		s.evals.list = slices.DeleteFunc(s.evals.list, func(x *liveEval) bool { return x == l })
		s.evals.mu.Unlock()
		l.update(func(*api.EvalRun) { l.ended = true })
	}()
	if err := <-started; err != nil {
		<-l.done
		return nil, err
	}
	return l, nil
}

// liveEval finds a run going on in workspace by its ID (nil: none).
func (s *Server) liveEval(workspace, id string) *liveEval {
	s.evals.mu.Lock()
	defer s.evals.mu.Unlock()
	for _, l := range s.evals.list {
		if l.workspace == workspace {
			if run, _ := l.snapshot(); run.ID == id {
				return l
			}
		}
	}
	return nil
}

// liveEvals are the runs of eval name going on in workspace, as they
// stand.
func (s *Server) liveEvals(workspace, name string) []api.EvalRun {
	s.evals.mu.Lock()
	list := slices.Clone(s.evals.list)
	s.evals.mu.Unlock()
	var out []api.EvalRun
	for _, l := range list {
		if run, _ := l.snapshot(); l.workspace == workspace && run.Eval == name && run.ID != "" {
			out = append(out, run)
		}
	}
	return out
}

// stopEvals stops every eval run and waits for them to end (the service
// is closing): each is recorded as stopped.
func (s *Server) stopEvals() {
	s.evals.mu.Lock()
	list := slices.Clone(s.evals.list)
	s.evals.mu.Unlock()
	for _, l := range list {
		l.cancel()
	}
	for _, l := range list {
		<-l.done
	}
}

// follow sends l's events until the run ends or ctx is done (the run
// goes on): from its start (the run as it started, every result, then
// finished) for the request that started it; else the run as it stands,
// with its results so far, each result after, then finished, or finished
// alone once it has ended.
func (l *liveEval) follow(ctx context.Context, fromStart bool, send func(api.EvalRun, *api.EvalResult, bool) error) error {
	ch, stop := l.subscribe()
	defer stop()
	run, ended := l.snapshot()
	first, sent := run, len(run.Results)
	switch {
	case fromStart:
		l.mu.Lock()
		first, sent = l.begun, 0
		l.mu.Unlock()
	case ended:
		return send(run, nil, true)
	}
	if err := send(first, nil, false); err != nil {
		return err
	}
	for {
		run, ended = l.snapshot()
		for ; sent < len(run.Results); sent++ {
			if err := send(run, &run.Results[sent], false); err != nil {
				return err
			}
		}
		if ended {
			return send(run, nil, true)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

// withLiveEvals puts the runs going on before those recorded, each once.
func withLiveEvals(live, recorded []api.EvalRun) []api.EvalRun {
	out := slices.Clone(live)
	for _, run := range recorded {
		if !slices.ContainsFunc(live, func(l api.EvalRun) bool { return l.ID == run.ID }) {
			out = append(out, run)
		}
	}
	return out
}

// errUnknownEvalRun is an eval run that isn't going on (or recorded).
func errUnknownEvalRun(id string) error {
	return apiError(connect.CodeNotFound, "EVAL_RUN_NOT_FOUND", fmt.Errorf("no eval run %q", id), "id", id)
}
