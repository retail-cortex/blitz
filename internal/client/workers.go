package client

import (
	"context"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/socket"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/internal/gen/blitz/v1"
	"github.com/retail-cortex/blitz/internal/gen/blitz/v1/blitzv1connect"
)

// Workers are one workspace's workers in the service.
type Workers struct {
	dir string
	c   blitzv1connect.WorkerServiceClient
}

// AttachWorkers reaches the workers of the workspace dir (absolute) in the
// service listening on sock.
func AttachWorkers(sock, dir string) *Workers {
	return AttachWorkersHTTP(socket.Client(sock), socket.BaseURL, dir)
}

// AttachWorkersHTTP is AttachWorkers over any HTTP client.
func AttachWorkersHTTP(hc connect.HTTPClient, baseURL, dir string) *Workers {
	return &Workers{dir: dir, c: blitzv1connect.NewWorkerServiceClient(hc, baseURL)}
}

func (w *Workers) ListWorkers() ([]api.WorkerInfo, error) {
	res, err := w.c.ListWorkers(context.Background(), connect.NewRequest(&pb.ListWorkersRequest{Workspace: w.dir}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]api.WorkerInfo, len(res.Msg.Workers))
	for i, wk := range res.Msg.Workers {
		out[i] = workerInfo(wk)
	}
	return out, nil
}

func (w *Workers) EnableWorker(name, hash string) (api.WorkerInfo, error) {
	res, err := w.c.EnableWorker(context.Background(), connect.NewRequest(&pb.EnableWorkerRequest{Workspace: w.dir, Name: name, Hash: hash}))
	if err != nil {
		return api.WorkerInfo{}, fromAPI(err)
	}
	return workerInfo(res.Msg.Worker), nil
}

func (w *Workers) DisableWorker(name string) (api.WorkerInfo, error) {
	res, err := w.c.DisableWorker(context.Background(), connect.NewRequest(&pb.DisableWorkerRequest{Workspace: w.dir, Name: name}))
	if err != nil {
		return api.WorkerInfo{}, fromAPI(err)
	}
	return workerInfo(res.Msg.Worker), nil
}

// RunWorker runs the worker now in the service, passes its events to
// on while it runs, and returns its record when it finishes.
func (w *Workers) RunWorker(ctx context.Context, name string, on func(api.Event)) (api.Run, error) {
	res, err := w.c.RunWorker(ctx, connect.NewRequest(&pb.RunWorkerRequest{Workspace: w.dir, Name: name}))
	if err != nil {
		return api.Run{}, fromAPI(err)
	}
	id := res.Msg.Run.Id
	if stream, err := w.c.WatchWorkerRun(ctx, connect.NewRequest(&pb.WatchWorkerRunRequest{RunId: id})); err == nil {
		for stream.Receive() {
			if e, ok := event(stream.Msg().Event); ok && on != nil {
				on(e)
			}
		}
		stream.Close()
	}
	// The record appears once the run has finished (and at once if it
	// finished before watching began).
	for {
		got, err := w.c.GetWorkerRun(ctx, connect.NewRequest(&pb.GetWorkerRunRequest{RunId: id}))
		if err != nil {
			return api.Run{}, fromAPI(err)
		}
		if got.Msg.Run.Status != pb.RunStatus_RUN_STATUS_RUNNING {
			return workerRun(got.Msg.Run), nil
		}
		select {
		case <-ctx.Done():
			return workerRun(got.Msg.Run), ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (w *Workers) WorkerRuns(name string, limit int) ([]api.Run, error) {
	res, err := w.c.ListWorkerRuns(context.Background(), connect.NewRequest(&pb.ListWorkerRunsRequest{Workspace: w.dir, Name: name, Limit: int32(limit)}))
	if err != nil {
		return nil, fromAPI(err)
	}
	out := make([]api.Run, len(res.Msg.Runs))
	for i, r := range res.Msg.Runs {
		out[i] = workerRun(r)
	}
	return out, nil
}

var workerStates = map[pb.WorkerState]api.State{
	pb.WorkerState_WORKER_STATE_NEW:      api.StateNew,
	pb.WorkerState_WORKER_STATE_ENABLED:  api.StateEnabled,
	pb.WorkerState_WORKER_STATE_DISABLED: api.StateDisabled,
	pb.WorkerState_WORKER_STATE_CHANGED:  api.StateChanged,
	pb.WorkerState_WORKER_STATE_INVALID:  api.StateInvalid,
}

func workerInfo(w *pb.Worker) api.WorkerInfo {
	return api.WorkerInfo{
		Workspace: w.Workspace, Name: w.Name, Description: w.Description, Path: w.Path, Hash: w.Hash,
		State: workerStates[w.State], Schedule: w.Schedule, Cron: w.Cron, Timezone: w.Timezone, Next: timeOf(w.NextRun),
		Agent: w.Agent, Model: w.Model, Permissions: w.Permissions, Problems: w.Problems,
		Limits: api.Limits{MaxTurns: int(w.Limits.GetMaxTurns()), MaxCostUSD: w.Limits.GetMaxCostUsd(), Timeout: w.Limits.GetTimeout().AsDuration()},
	}
}

var runStatuses = map[pb.RunStatus]api.RunStatus{
	pb.RunStatus_RUN_STATUS_RUNNING:   api.RunRunning,
	pb.RunStatus_RUN_STATUS_SUCCEEDED: api.RunSucceeded,
	pb.RunStatus_RUN_STATUS_FAILED:    api.RunFailed,
	pb.RunStatus_RUN_STATUS_LIMITED:   api.RunLimited,
	pb.RunStatus_RUN_STATUS_SKIPPED:   api.RunSkipped,
}

func workerRun(r *pb.WorkerRun) api.Run {
	out := api.Run{
		ID: r.Id, Workspace: r.Workspace, Worker: r.Worker, Hash: r.Hash, Status: runStatuses[r.Status], Manual: r.Manual,
		Started: timeOf(r.Started), Duration: r.GetDuration().AsDuration(), CostUSD: r.Usage.GetCostUsd(), Calls: int(r.Usage.GetCalls()),
		SessionID: r.SessionId, Error: r.GetError().GetMessage(),
	}
	for _, f := range r.Refusals {
		out.Refusals = append(out.Refusals, api.Refusal{Tool: f.Tool, Kind: actionKind(f.Kind), Detail: f.Detail, Time: timeOf(f.Time)})
	}
	return out
}
