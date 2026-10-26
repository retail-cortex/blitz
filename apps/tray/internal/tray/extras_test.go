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

package tray

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The MCP servers submenu: each server checked while it's on, a click
// flipping it; a note when there are none.
func TestMCPEntries(t *testing.T) {
	got := MCPEntries(Extras{MCP: []*pb.MCPServerConfig{{Name: "github"}, {Name: "db", Disabled: true}}})
	assert.Equal(t, []Entry{
		{Title: "github", Checkbox: true, Checked: true, Action: Action{Kind: SetMCP, Name: "github", On: false}},
		{Title: "db", Checkbox: true, Checked: false, Action: Action{Kind: SetMCP, Name: "db", On: true}},
	}, got)
	none := MCPEntries(Extras{})
	require.Len(t, none, 1)
	assert.True(t, none[0].Disabled)
}

func TestWorkersLink(t *testing.T) {
	assert.Equal(t, "blitz://workers?dir=%2Fw%2Fmy+shop", WorkersLink("/w/my shop", ""))
	assert.Equal(t, "blitz://workers?dir=%2Fw&worker=nightly", WorkersLink("/w", "nightly"), "a job's details")
}

// fakeExtras answers the submenus' calls and records the changes asked.
type fakeExtras struct {
	pb.UnimplementedConfigServiceHandler
	pb.UnimplementedWorkerServiceHandler
	pb.UnimplementedSessionServiceHandler
	pb.UnimplementedWorkspaceServiceHandler
	mu    sync.Mutex
	asked []string
	// noWorkers fails ListWorkers, as a workspace's trouble can.
	noWorkers bool
}

func (f *fakeExtras) StartBackground(_ context.Context, r *connect.Request[pb.StartBackgroundRequest]) (*connect.Response[pb.StartBackgroundResponse], error) {
	f.note("start " + r.Msg.Workspace + " " + r.Msg.Turn.GetText())
	return connect.NewResponse(&pb.StartBackgroundResponse{}), nil
}

func (f *fakeExtras) StopWorkflowRun(_ context.Context, r *connect.Request[pb.StopWorkflowRunRequest]) (*connect.Response[pb.StopWorkflowRunResponse], error) {
	f.note("stop flow " + r.Msg.Id)
	return connect.NewResponse(&pb.StopWorkflowRunResponse{}), nil
}

// WatchWorkflowRuns sends the runs twice, then ends.
func (f *fakeExtras) WatchWorkflowRuns(_ context.Context, _ *connect.Request[pb.WatchWorkflowRunsRequest], stream *connect.ServerStream[pb.WatchWorkflowRunsResponse]) error {
	for _, status := range []string{"running", "done"} {
		if err := stream.Send(&pb.WatchWorkflowRunsResponse{Runs: []*pb.WorkflowRun{{Id: "f1", Workflow: "brief", Status: status}}}); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeExtras) SetConnectorEnabled(_ context.Context, r *connect.Request[pb.SetConnectorEnabledRequest]) (*connect.Response[pb.SetConnectorEnabledResponse], error) {
	f.note("connector " + r.Msg.Workspace + " " + r.Msg.Scope + "/" + r.Msg.Category + "/" + r.Msg.Name + map[bool]string{true: " on", false: " off"}[r.Msg.Enabled])
	return connect.NewResponse(&pb.SetConnectorEnabledResponse{}), nil
}

func (f *fakeExtras) ListRunningProcesses(context.Context, *connect.Request[pb.ListRunningProcessesRequest]) (*connect.Response[pb.ListRunningProcessesResponse], error) {
	return connect.NewResponse(&pb.ListRunningProcessesResponse{Processes: []*pb.WorkspaceProcess{{Workspace: "/w", Process: &pb.Process{Id: 3, Command: "npm run dev", Running: true}}}}), nil
}

func (f *fakeExtras) KillProcess(_ context.Context, r *connect.Request[pb.KillProcessRequest]) (*connect.Response[pb.KillProcessResponse], error) {
	f.note(fmt.Sprintf("kill %s %d any=%v", r.Msg.Workspace, r.Msg.Id, r.Msg.AnySession))
	return connect.NewResponse(&pb.KillProcessResponse{}), nil
}

func (f *fakeExtras) note(s string) { f.mu.Lock(); f.asked = append(f.asked, s); f.mu.Unlock() }

func (f *fakeExtras) ListMCPServerConfigs(context.Context, *connect.Request[pb.ListMCPServerConfigsRequest]) (*connect.Response[pb.ListMCPServerConfigsResponse], error) {
	return connect.NewResponse(&pb.ListMCPServerConfigsResponse{Servers: []*pb.MCPServerConfig{{Name: "github"}}}), nil
}

func (f *fakeExtras) SetMCPServerEnabled(_ context.Context, r *connect.Request[pb.SetMCPServerEnabledRequest]) (*connect.Response[pb.SetMCPServerEnabledResponse], error) {
	f.note("mcp " + r.Msg.Name + map[bool]string{true: " on", false: " off"}[r.Msg.Enabled])
	return connect.NewResponse(&pb.SetMCPServerEnabledResponse{}), nil
}

func (f *fakeExtras) ListWorkers(context.Context, *connect.Request[pb.ListWorkersRequest]) (*connect.Response[pb.ListWorkersResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.noWorkers {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("lstat /gone: no such file or directory"))
	}
	return connect.NewResponse(&pb.ListWorkersResponse{Workers: []*pb.Worker{{Workspace: "/w", Name: "nightly"}}}), nil
}

func (f *fakeExtras) ListRunningWorkerRuns(context.Context, *connect.Request[pb.ListRunningWorkerRunsRequest]) (*connect.Response[pb.ListRunningWorkerRunsResponse], error) {
	return connect.NewResponse(&pb.ListRunningWorkerRunsResponse{Runs: []*pb.WorkerRun{{Id: "r1"}}}), nil
}

func (f *fakeExtras) StopWorkerRun(_ context.Context, r *connect.Request[pb.StopWorkerRunRequest]) (*connect.Response[pb.StopWorkerRunResponse], error) {
	f.note("stop " + r.Msg.RunId)
	return connect.NewResponse(&pb.StopWorkerRunResponse{}), nil
}

func (f *fakeExtras) RunWorker(_ context.Context, r *connect.Request[pb.RunWorkerRequest]) (*connect.Response[pb.RunWorkerResponse], error) {
	f.note("run " + r.Msg.Name)
	return connect.NewResponse(&pb.RunWorkerResponse{}), nil
}

func (f *fakeExtras) EnableWorker(_ context.Context, r *connect.Request[pb.EnableWorkerRequest]) (*connect.Response[pb.EnableWorkerResponse], error) {
	f.note("enable " + r.Msg.Name + "@" + r.Msg.Hash)
	return connect.NewResponse(&pb.EnableWorkerResponse{}), nil
}

func (f *fakeExtras) DisableWorker(_ context.Context, r *connect.Request[pb.DisableWorkerRequest]) (*connect.Response[pb.DisableWorkerResponse], error) {
	f.note("disable " + r.Msg.Name)
	return connect.NewResponse(&pb.DisableWorkerResponse{}), nil
}

// The submenus' lists come from the service, and their clicks go to it
// (Show in Blitz opens the link).
func TestFetchExtrasAndDo(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "blitz.sock")
	ln, err := socket.Listen(sock)
	require.NoError(t, err)
	f := &fakeExtras{}
	mux := http.NewServeMux()
	mux.Handle(pb.NewConfigServiceHandler(f))
	mux.Handle(pb.NewWorkerServiceHandler(f))
	mux.Handle(pb.NewSessionServiceHandler(f))
	mux.Handle(pb.NewWorkspaceServiceHandler(f))
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })
	ctx := context.Background()

	e, err := FetchExtras(ctx, sock)
	require.NoError(t, err)
	assert.Equal(t, "github", e.MCP[0].Name)
	assert.Equal(t, "nightly", e.Workers[0].Name)
	assert.Equal(t, "r1", e.Runs[0].Id)
	assert.Equal(t, "npm run dev", e.Processes[0].Process.Command)

	// A list the service can't give is left empty; the rest still come.
	f.mu.Lock()
	f.noWorkers = true
	f.mu.Unlock()
	e, err = FetchExtras(ctx, sock)
	require.NoError(t, err, "one list's trouble doesn't hide the menu")
	assert.Empty(t, e.Workers)
	assert.Equal(t, "github", e.MCP[0].Name)
	assert.Equal(t, "r1", e.Runs[0].Id)
	assert.Equal(t, "npm run dev", e.Processes[0].Process.Command)
	f.mu.Lock()
	f.noWorkers = false
	f.mu.Unlock()

	var opened string
	open := func(link string) error { opened = link; return nil }
	for _, a := range []Action{
		{Kind: SetMCP, Name: "github", On: false},
		{Kind: StopRun, RunID: "r1"},
		{Kind: RunWorker, Workspace: "/w", Name: "nightly"},
		{Kind: EnableWorker, Workspace: "/w", Name: "nightly", Hash: "h"},
		{Kind: DisableWorker, Workspace: "/w", Name: "nightly"},
		{Kind: ShowWorkers, Workspace: "/w"},
		{Kind: RunWorkflow, Workspace: "/w", Name: "brief"},
		{Kind: StopWorkflow, Workspace: "/w", RunID: "f1"},
		{Kind: SetConnector, Workspace: "/w", Category: "databases", Scope: "global", Name: "sales", On: false},
		{Kind: StopProcess, Workspace: "/w", ID: 3},
		{Kind: NoAction},
	} {
		require.NoError(t, Do(ctx, sock, a, open), "%+v", a)
	}
	assert.Equal(t, []string{"mcp github off", "stop r1", "run nightly", "enable nightly@h", "disable nightly", "start /w /brief", "stop flow f1", "connector /w global/databases/sales off", "kill /w 3 any=true"}, f.asked)
	assert.Equal(t, "blitz://workers?dir=%2Fw", opened)
	require.NoError(t, Do(ctx, sock, Action{Kind: ShowWorkflow, Workspace: "/w", Name: "brief", RunID: "f1"}, open))
	assert.Equal(t, "blitz://workflow?dir=%2Fw&run=f1&workflow=brief", opened)
	require.NoError(t, Do(ctx, sock, Action{Kind: ShowProcess, Workspace: "/w", ID: 3}, open))
	assert.Equal(t, "blitz://processes?dir=%2Fw&id=3", opened)

	// The runs are followed as they change; the stream's end says none, and it's asked again.
	seen := make(chan []*pb.WorkflowRun, 8)
	watchCtx, stop := context.WithCancel(ctx)
	go WatchRuns(watchCtx, sock, 10*time.Millisecond, func(r []*pb.WorkflowRun) { seen <- r })
	first, second, ended := <-seen, <-seen, <-seen
	stop()
	assert.Equal(t, "running", first[0].Status)
	assert.Equal(t, "done", second[0].Status)
	assert.Empty(t, ended, "the stream ended: nothing known to run")
	assert.Error(t, Do(ctx, sock, Action{Kind: ActionKind(99)}, open))

	srv.Close()
	_, err = FetchExtras(ctx, sock)
	assert.Error(t, err, "nobody answers")
}

// Links open with the system's opener; the app is found in the Electron
// app's layout.
func TestOpenLinkAndElectronApp(t *testing.T) {
	f, a := newFake(t)
	require.NoError(t, a.OpenLink("blitz://workers?dir=%2Fw"))
	want := map[string]string{"darwin": "open blitz://workers?dir=%2Fw", "windows": "rundll32 url.dll,FileProtocolHandler blitz://workers?dir=%2Fw"}[goruntime.GOOS]
	if want == "" {
		want = "xdg-open blitz://workers?dir=%2Fw"
	}
	assert.Equal(t, []string{want}, f.ran)

	assert.Equal(t, "/Applications/Blitz.app", appBundle("/Applications/Blitz.app/Contents/Resources/app/bin"))
	assert.Equal(t, "/Applications/Blitz.app", appBundle("/Applications/Blitz.app/Contents/MacOS"))
	assert.Empty(t, appBundle("/usr/lib/blitz-desktop/resources/app/bin"))

	if goruntime.GOOS == "linux" {
		root := t.TempDir()
		a.Beside = filepath.Join(root, "resources", "app", "bin")
		require.NoError(t, os.MkdirAll(a.Beside, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "blitz-desktop"), []byte("#!/bin/sh\n"), 0o755))
		f.ran = nil
		require.NoError(t, a.OpenApp())
		assert.Equal(t, []string{"blitz-desktop"}, f.ran)
	}
}
