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
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
}

func (f *fakeSync) GetOrgSync(context.Context, *connect.Request[pb.GetOrgSyncRequest]) (*connect.Response[pb.GetOrgSyncResponse], error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	out := &pb.GetOrgSyncResponse{State: f.state}
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

	f.getErr = connect.NewError(connect.CodeUnavailable, errors.New("the service is restarting"))
	_, err = runCLI(t, "enterprise", "sync", "status")
	assert.ErrorContains(t, err, "the service is restarting")
}
