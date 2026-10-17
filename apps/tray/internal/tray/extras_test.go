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
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"testing"

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

// The Workers submenu: runs to stop first, then each worker with Run now,
// Enabled (only for a reviewed one) and Show in Blitz, named with its
// workspace when there are several.
func TestWorkerEntries(t *testing.T) {
	one := []*pb.Worker{
		{Workspace: "/w/shop", Name: "nightly", State: pb.WorkerState_WORKER_STATE_ENABLED, Hash: "h1"},
		{Workspace: "/w/shop", Name: "weekly", State: pb.WorkerState_WORKER_STATE_DISABLED, Hash: "h2"},
		{Workspace: "/w/shop", Name: "fresh", State: pb.WorkerState_WORKER_STATE_NEW, Hash: "h3"},
	}
	got := WorkerEntries(Extras{Workers: one, Runs: []*pb.WorkerRun{{Id: "r1", Workspace: "/w/shop", Worker: "nightly"}}})
	require.Len(t, got, 4)
	assert.Equal(t, Action{Kind: StopRun, RunID: "r1"}, got[0].Action)
	assert.Contains(t, got[0].Title, "nightly")

	nightly := got[1]
	assert.Equal(t, "nightly", nightly.Title, "one workspace: just the name")
	require.Len(t, nightly.Children, 3)
	assert.Equal(t, Action{Kind: RunWorker, Workspace: "/w/shop", Name: "nightly"}, nightly.Children[0].Action)
	assert.True(t, nightly.Children[1].Checked)
	assert.Equal(t, Action{Kind: DisableWorker, Workspace: "/w/shop", Name: "nightly"}, nightly.Children[1].Action)
	assert.Equal(t, Action{Kind: ShowWorkers, Workspace: "/w/shop"}, nightly.Children[2].Action)

	weekly := got[2].Children[1]
	assert.False(t, weekly.Checked)
	assert.Equal(t, Action{Kind: EnableWorker, Workspace: "/w/shop", Name: "weekly", Hash: "h2"}, weekly.Action, "re-enabled at the hash reviewed")

	fresh := got[3].Children
	assert.True(t, fresh[0].Disabled, "a new worker doesn't run before it's reviewed")
	assert.True(t, fresh[1].Disabled)
	assert.Equal(t, NoAction, fresh[1].Action.Kind)

	two := append(one, &pb.Worker{Workspace: "/w/blog", Name: "nightly", State: pb.WorkerState_WORKER_STATE_ENABLED})
	assert.Equal(t, "shop › nightly", WorkerEntries(Extras{Workers: two})[0].Title)

	none := WorkerEntries(Extras{})
	require.Len(t, none, 1)
	assert.True(t, none[0].Disabled)
	assert.NotEqual(t, Key(MCPEntries(Extras{})), Key(WorkerEntries(Extras{})))
}

func TestWorkersLink(t *testing.T) {
	assert.Equal(t, "blitz://workers?dir=%2Fw%2Fmy+shop", WorkersLink("/w/my shop"))
}

// fakeExtras answers the submenus' calls and records the changes asked.
type fakeExtras struct {
	pb.UnimplementedConfigServiceHandler
	pb.UnimplementedWorkerServiceHandler
	mu    sync.Mutex
	asked []string
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
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })
	ctx := context.Background()

	e, err := FetchExtras(ctx, sock)
	require.NoError(t, err)
	assert.Equal(t, "github", e.MCP[0].Name)
	assert.Equal(t, "nightly", e.Workers[0].Name)
	assert.Equal(t, "r1", e.Runs[0].Id)

	var opened string
	open := func(link string) error { opened = link; return nil }
	for _, a := range []Action{
		{Kind: SetMCP, Name: "github", On: false},
		{Kind: StopRun, RunID: "r1"},
		{Kind: RunWorker, Workspace: "/w", Name: "nightly"},
		{Kind: EnableWorker, Workspace: "/w", Name: "nightly", Hash: "h"},
		{Kind: DisableWorker, Workspace: "/w", Name: "nightly"},
		{Kind: ShowWorkers, Workspace: "/w"},
		{Kind: NoAction},
	} {
		require.NoError(t, Do(ctx, sock, a, open), "%+v", a)
	}
	assert.Equal(t, []string{"mcp github off", "stop r1", "run nightly", "enable nightly@h", "disable nightly"}, f.asked)
	assert.Equal(t, "blitz://workers?dir=%2Fw", opened)
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
