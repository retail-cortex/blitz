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
	"connectrpc.com/connect"
	"context"
	"net/http"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Asking the service how it is, as the tray does every 2 s, leaves nothing
// behind: no connection, no goroutine, in the tray or the service.
func TestProbeLeavesNothingBehind(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "blitz.sock")
	ln, err := socket.Listen(sock)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(pb.NewWorkspaceServiceHandler(pb.UnimplementedWorkspaceServiceHandler{}))
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })

	probe := func() {
		Probe(context.Background(), sock, func() bool { return false })
		ServiceLogDir(context.Background(), sock)
	}
	probe() // the first opens the connection the rest reuse
	time.Sleep(50 * time.Millisecond)
	before := runtime.NumGoroutine()
	for range 100 {
		probe()
	}
	time.Sleep(50 * time.Millisecond)
	assert.LessOrEqual(t, runtime.NumGoroutine(), before+5, "goroutines grew with each probe")
}

// orgConfig answers the tray's questions about the organization.
type orgConfig struct {
	pb.UnimplementedConfigServiceHandler
	branding string
	state    *pb.EnterpriseState
}

func (o orgConfig) GetBranding(context.Context, *connect.Request[pb.GetBrandingRequest]) (*connect.Response[pb.GetBrandingResponse], error) {
	res := &pb.GetBrandingResponse{}
	if o.branding != "" {
		res.Branding = &pb.Branding{Name: o.branding}
	}
	return connect.NewResponse(res), nil
}

func (o orgConfig) GetEnterprise(context.Context, *connect.Request[pb.GetEnterpriseRequest]) (*connect.Response[pb.GetEnterpriseResponse], error) {
	return connect.NewResponse(&pb.GetEnterpriseResponse{State: o.state}), nil
}

// The tray names the organization from the kept branding, else from what
// its service says, else its address; none when not connected or from a
// service too old to say.
func TestProbeOrg(t *testing.T) {
	cases := map[string]struct {
		cfg  pb.ConfigServiceHandler
		want string
	}{
		"its branding":     {orgConfig{branding: "Acme Assist", state: &pb.EnterpriseState{Url: "https://b.acme.com"}}, "Acme Assist"},
		"its service":      {orgConfig{state: &pb.EnterpriseState{Url: "https://b.acme.com", Name: "Acme"}}, "Acme"},
		"its address":      {orgConfig{state: &pb.EnterpriseState{Url: "https://b.acme.com"}}, "https://b.acme.com"},
		"not connected":    {orgConfig{state: &pb.EnterpriseState{}}, ""},
		"an older service": {pb.UnimplementedConfigServiceHandler{}, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			sock := filepath.Join(shortTemp(t), "blitz.sock")
			ln, err := socket.Listen(sock)
			require.NoError(t, err)
			mux := http.NewServeMux()
			mux.Handle(pb.NewWorkspaceServiceHandler(pb.UnimplementedWorkspaceServiceHandler{}))
			mux.Handle(pb.NewConfigServiceHandler(c.cfg))
			srv := &http.Server{Handler: mux}
			go func() { _ = srv.Serve(ln) }()
			t.Cleanup(func() { srv.Close() })
			assert.Equal(t, c.want, Probe(context.Background(), sock, func() bool { return true }).Org)
		})
	}
}
