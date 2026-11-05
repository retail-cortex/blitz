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

package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/orgsync"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// fakeSync is a service whose one workspace's sync is state, and Save now
// makes it saved (or fails with err).
type fakeSync struct {
	pb.UnimplementedConfigServiceHandler
	state  string
	err    error
	getErr error
	saves  int
	dirs   []string // the workspaces asked about
}

func (f *fakeSync) GetOrgSync(_ context.Context, r *connect.Request[pb.GetOrgSyncRequest]) (*connect.Response[pb.GetOrgSyncResponse], error) {
	f.dirs = append(f.dirs, r.Msg.GetWorkspace())
	if f.getErr != nil {
		return nil, f.getErr
	}
	out := &pb.GetOrgSyncResponse{State: f.state, Failed: orgsync.Failed(f.state)}
	if f.state == "conflicts" {
		out.Pairs = []*pb.OrgSyncPair{{Shared: "plan.md", Mine: "plan (conflict, Ana).md"}}
	} else {
		out.Error = "remote unpack failed"
	}
	return connect.NewResponse(out), nil
}

func (f *fakeSync) SaveOrgSync(context.Context, *connect.Request[pb.SaveOrgSyncRequest]) (*connect.Response[pb.SaveOrgSyncResponse], error) {
	f.saves++
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(&pb.SaveOrgSyncResponse{State: "saved"}), nil
}

// blitz enterprise sync saves now and says how it went; sync status only
// looks, failing while the service hasn't got this computer's saves.
func TestEnterpriseSync(t *testing.T) {
	isolate(t)
	dir, err := os.MkdirTemp("", "bs")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	t.Setenv("BLITZ_SOCKET", sock)
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	f := &fakeSync{state: "refused"}
	mux := http.NewServeMux()
	mux.Handle(pb.NewConfigServiceHandler(f))
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })

	out, err := runCLI(t, "enterprise", "sync", "status")
	assert.ErrorContains(t, err, "not saved to the service yet")
	assert.Contains(t, out, "Not saved: the service refused it\n  remote unpack failed")
	assert.Zero(t, f.saves, "status only looks")

	out, err = runCLI(t, "enterprise", "sync")
	require.NoError(t, err)
	assert.Contains(t, out, "✓ Saved: the service has everything")
	assert.Equal(t, 1, f.saves)

	f.state = "conflicts"
	out, err = runCLI(t, "enterprise", "sync", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "plan.md (yours: plan (conflict, Ana).md)")

	f.err = connect.NewError(connect.CodeFailedPrecondition, errors.New("a turn is running"))
	_, err = runCLI(t, "enterprise", "sync")
	assert.ErrorContains(t, err, "a turn is running")
	f.state = ""
	_, err = runCLI(t, "enterprise", "sync")
	assert.ErrorContains(t, err, "isn't kept in step")

	// The global --dir names the workspace, from any folder (N26 cli-1#6).
	ws := t.TempDir()
	t.Chdir(t.TempDir())
	f.state = "saved"
	_, err = runCLI(t, "-d", ws, "enterprise", "sync", "status")
	require.NoError(t, err)
	assert.Equal(t, ws, f.dirs[len(f.dirs)-1])

	f.getErr = connect.NewError(connect.CodeUnavailable, errors.New("the service is restarting"))
	_, err = runCLI(t, "enterprise", "sync", "status")
	assert.ErrorContains(t, err, "the service is restarting")
}

// fakeRuns is a service whose runs going now come in two lists, then the
// stream waits for the client to go.
type fakeRuns struct {
	pb.UnimplementedConfigServiceHandler
}

func (fakeRuns) WatchServiceRuns(ctx context.Context, _ *connect.Request[pb.WatchServiceRunsRequest], st *connect.ServerStream[pb.WatchServiceRunsResponse]) error {
	run := &pb.GoingServiceRun{Run: &pb.ServiceRun{Id: "run-7", Status: "running"}, JobName: "digest", Workspace: "acme/plans"}
	for _, m := range []*pb.WatchServiceRunsResponse{
		{Listed: true, Runs: []*pb.GoingServiceRun{run}},
		{}, // a keepalive
		{Listed: true},
	} {
		if err := st.Send(m); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

// blitz enterprise jobs watch follows the runs going on the service as
// they change (EN-42; N26 cli-1#9), skipping keepalives.
func TestEnterpriseJobsWatch(t *testing.T) {
	dir, err := os.MkdirTemp("", "bw") // short: a socket's path has a limit
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(pb.NewConfigServiceHandler(fakeRuns{}))
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	var got [][]client.GoingServiceRun
	err = client.AttachSettings(sock).WatchServiceRuns(ctx, func(runs []client.GoingServiceRun) {
		got = append(got, runs)
		if len(got) == 2 {
			cancel()
		}
	})
	require.NoError(t, err, "a cancelled watch ends quietly")
	require.Len(t, got, 2)
	require.Len(t, got[0], 1)
	assert.Equal(t, "digest", got[0][0].JobName)
	assert.Empty(t, got[1])

	var out bytes.Buffer
	printGoingRuns(&out, got[0])
	printGoingRuns(&out, got[1])
	assert.Contains(t, out.String(), "1 run going on the service")
	assert.Contains(t, out.String(), "acme/plans  digest  run-7  running")
	assert.Contains(t, out.String(), "0 runs going on the service")
}
