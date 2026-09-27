package app

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"

	"github.com/retail-cortex/blitz/internal/config"
	"github.com/retail-cortex/blitz/internal/runtime"
	"github.com/retail-cortex/blitz/internal/session"
	"github.com/retail-cortex/blitz/internal/tools"
	"github.com/retail-cortex/blitz/internal/workers"
)

// Workers: scheduled workflows the workspace defines in
// workers/<name>/WORKER.md (ROADMAP item 24). Enabling one after reviewing
// it pins its content hash; scanning alone never schedules anything.

// defaultWorkerStore is where enabled workers are recorded.
func defaultWorkerStore() (*workers.Store, error) {
	return workers.OpenStore(config.ExpandHome("~/.blitz/workers.json"))
}

// workerRoots are the directories holding the workspace's workers.
func (w *Workspace) workerRoots() []string {
	var out []string
	for _, p := range w.cfg.Workers.Paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(w.Dir(), p)
		}
		out = append(out, p)
	}
	return out
}

// discoverWorkers loads the workspace's workers, keyed by name (a later
// root doesn't override an earlier one).
func (w *Workspace) discoverWorkers() map[string]workers.Found {
	out := map[string]workers.Found{}
	for _, root := range w.workerRoots() {
		list, err := workers.Discover(root)
		if err != nil {
			w.warn(err.Error())
		}
		for _, f := range list {
			if _, dup := out[f.Worker.Name]; !dup {
				out[f.Worker.Name] = f
			}
		}
	}
	return out
}

func (w *Workspace) workerInfo(wk *workers.Worker, loadErr error, now time.Time) api.WorkerInfo {
	info := api.WorkerInfo{
		Workspace: w.Dir(), Name: wk.Name, Description: wk.Description, Path: wk.Path, Hash: wk.Hash,
		State:    w.workerStore.State(w.Dir(), wk, loadErr),
		Schedule: wk.Schedule.Text, Cron: wk.Schedule.Cron, Agent: wk.Agent, Model: wk.Model, CatchUp: wk.CatchUp,
	}
	if wk.Schedule.Location != nil {
		info.Timezone = wk.Schedule.Location.String()
	}
	var invalid *workers.InvalidError
	if errors.As(loadErr, &invalid) {
		info.Problems = invalid.Problems
	} else if loadErr != nil {
		info.Problems = []string{loadErr.Error()}
	}
	if loadErr == nil {
		eff := workers.Apply(wk, w.cfg.Workers.Policy)
		for _, p := range eff.Permissions {
			info.Permissions = append(info.Permissions, p.String())
		}
		info.Limits = eff.Limits
		info.Problems = append(info.Problems, eff.Notes...)
		if wk.Agent != "" {
			if _, ok := w.agents.Get(wk.Agent); !ok {
				info.Problems = append(info.Problems, fmt.Sprintf("agent %q isn't defined: runs will fail until it is", wk.Agent))
			}
		}
		if info.State == api.StateEnabled {
			info.Next = wk.Schedule.Next(now)
		}
	}
	return info
}

// ListWorkers returns the workspace's workers, by name.
func (w *Workspace) ListWorkers() ([]api.WorkerInfo, error) {
	if !w.cfg.Workers.Enabled {
		return nil, api.ErrWorkersDisabled
	}
	found := w.discoverWorkers()
	now := time.Now()
	var out []api.WorkerInfo
	for _, name := range slices.Sorted(maps.Keys(found)) {
		out = append(out, w.workerInfo(found[name].Worker, found[name].Err, now))
	}
	return out, nil
}

// worker loads one worker by name.
func (w *Workspace) worker(name string) (*workers.Worker, error, error) {
	if !w.cfg.Workers.Enabled {
		return nil, nil, api.ErrWorkersDisabled
	}
	if !workers.ValidName(name) { // it names a directory: nothing else is looked up
		return nil, nil, fmt.Errorf("%w: %q", api.ErrUnknownWorker, name)
	}
	for _, root := range w.workerRoots() {
		dir := filepath.Join(root, name)
		wk, loadErr := workers.Load(dir)
		if wk != nil {
			return wk, loadErr, nil
		}
	}
	return nil, nil, fmt.Errorf("%w: %q", api.ErrUnknownWorker, name)
}

// EnableWorker lets a worker run on its schedule, pinned to hash: the one
// the caller reviewed, which must still be current (api.ErrHashMismatch
// otherwise). An invalid worker can't be enabled.
func (w *Workspace) EnableWorker(name, hash string) (api.WorkerInfo, error) {
	wk, loadErr, err := w.worker(name)
	if err != nil {
		return api.WorkerInfo{}, err
	}
	if loadErr != nil {
		return w.workerInfo(wk, loadErr, time.Now()), loadErr
	}
	if err := w.workerStore.Enable(w.Dir(), wk, hash); err != nil {
		return w.workerInfo(wk, nil, time.Now()), err
	}
	return w.workerInfo(wk, nil, time.Now()), nil
}

// DisableWorker stops a worker from running.
func (w *Workspace) DisableWorker(name string) (api.WorkerInfo, error) {
	wk, loadErr, err := w.worker(name)
	if err != nil {
		return api.WorkerInfo{}, err
	}
	if err := w.workerStore.Disable(w.Dir(), name); err != nil {
		return api.WorkerInfo{}, err
	}
	return w.workerInfo(wk, loadErr, time.Now()), nil
}

// unattendedPreamble tells the agent how a worker run differs from a
// conversation.
const unattendedPreamble = "You are running unattended as the scheduled worker %q: nobody is watching or can answer questions. " +
	"You may only do what the worker is permitted; anything else is refused, and you should carry on without it or stop. " +
	"End with a short summary of what you did and anything you couldn't do.\n\n"

// RunWorker runs the named worker once, now, and records the run. The
// worker must be enabled at its current hash. The run has a session of its
// own (not the workspace's active one), gets exactly the worker's
// permissions after the host policy (anything else is refused and
// recorded), and stops at its limits. on receives the turn's events.
// A run of the same worker still going makes this ErrRunInProgress.
// RunOptions configure RunWorker.
type RunOptions struct {
	// Manual: started on request rather than by the schedule.
	Manual bool
	// OnStart receives the run's record as it starts (its ID and session).
	OnStart func(api.Run)
	// OnEvent receives the turn's events.
	OnEvent func(api.Event)
}

func (w *Workspace) RunWorker(ctx context.Context, name string, o RunOptions) (api.Run, error) {
	wk, loadErr, err := w.worker(name)
	if err != nil {
		return api.Run{}, err
	}
	if state := w.workerStore.State(w.Dir(), wk, loadErr); state != api.StateEnabled {
		return api.Run{}, fmt.Errorf("%w (it is %s)", api.ErrWorkerNotEnabled, state)
	}
	w.runsMu.Lock()
	if w.running[name] {
		w.runsMu.Unlock()
		skipped := api.Run{ID: newRunID(), Workspace: w.Dir(), Worker: name, Hash: wk.Hash, Status: api.RunSkipped,
			Manual: o.Manual, Started: time.Now(), Error: api.ErrRunInProgress.Error()}
		if !o.Manual { // a scheduled run that couldn't happen is worth a record
			w.runLog.Append(skipped)
		}
		return skipped, api.ErrRunInProgress
	}
	w.running[name] = true
	w.runsMu.Unlock()
	defer func() {
		w.runsMu.Lock()
		delete(w.running, name)
		w.runsMu.Unlock()
	}()

	eff := workers.Apply(wk, w.cfg.Workers.Policy)
	agent := cmp.Or(wk.Agent, w.engine.ActiveAgent())
	run := api.Run{ID: newRunID(), Workspace: w.Dir(), Worker: name, Hash: wk.Hash, Status: api.RunRunning, Manual: o.Manual, Started: time.Now()}

	st, err := session.NewStorage(w.cfg.Session.StorageDir)
	if err != nil {
		return run, err
	}
	st.SetWorkspace(w.Dir())
	rec, err := st.CreateSession(session.NewSessionID(), fmt.Sprintf("⏰ %s %s", name, run.Started.Format("2006-01-02 15:04")), agent)
	if err != nil {
		return run, err
	}
	run.SessionID = rec.ID
	if o.OnStart != nil {
		o.OnStart(run)
	}

	var refusalsMu sync.Mutex
	decide := func(_ context.Context, req api.ApprovalRequest) (api.Decision, error) {
		if workers.Allows(eff.Permissions, req) {
			return api.DecisionOnce, nil
		}
		refusalsMu.Lock()
		run.Refusals = append(run.Refusals, api.Refusal{Tool: req.Tool, Kind: req.Kind, Detail: req.Detail, Time: time.Now()})
		refusalsMu.Unlock()
		return api.DecisionDeny, nil
	}
	runCtx := tools.Unattended(ctx, decide)
	on := o.OnEvent
	if on == nil {
		on = func(api.Event) {}
	}
	// The worker's own agent and model, for this run only.
	var opts []runtime.ExecOption
	var runErr error
	if wk.Agent != "" {
		opts = append(opts, runtime.WithAgent(wk.Agent))
		if _, ok := w.agents.Get(wk.Agent); !ok {
			runErr = fmt.Errorf("agent %q isn't defined", wk.Agent)
		}
	}
	if wk.Model != "" && runErr == nil {
		llm, err := w.newModel(ctx, w.cfg, wk.Model)
		if err != nil {
			runErr = fmt.Errorf("model %q: %s", wk.Model, ModelErrorSummary(err, w.cfg))
		} else {
			opts = append(opts, runtime.WithModel(llm))
		}
	}
	if runErr == nil {
		_, runErr = w.run(runCtx, rec.ID, turn{Turn: api.Turn{
			Text: wk.Prompt, Prompt: fmt.Sprintf(unattendedPreamble, name) + wk.Prompt,
			MaxTurns: eff.Limits.MaxTurns, MaxCostUSD: eff.Limits.MaxCostUSD, Timeout: eff.Limits.Timeout,
		}}, on, st, opts...)
	}

	u := w.engine.Usage(rec.ID)
	run.Duration, run.CostUSD, run.Calls = time.Since(run.Started), u.CostUSD, u.Calls
	switch {
	case runErr == nil:
		run.Status = api.RunSucceeded
	case api.IsLimit(runErr):
		run.Status = api.RunLimited
		run.Error = runErr.Error()
	default:
		run.Status = api.RunFailed
		run.Error = runErr.Error()
	}
	if err := w.runLog.Append(run); err != nil {
		w.warn("recording the worker run: " + err.Error())
	}
	return run, nil
}

// WorkerRuns returns a worker's recorded runs, newest first.
func (w *Workspace) WorkerRuns(name string, limit int) ([]api.Run, error) {
	if !workers.ValidName(name) {
		return nil, fmt.Errorf("%w: %q", api.ErrUnknownWorker, name)
	}
	return w.runLog.List(w.Dir(), name, limit)
}

func newRunID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b)
}
