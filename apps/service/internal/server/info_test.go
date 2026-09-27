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
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
)

// The service says which version it is and what it runs from, so clients
// can tell a stale one (DSK-51a).
func TestGetServiceInfo(t *testing.T) {
	for _, tc := range []struct {
		opts []Option
		want string
	}{
		{nil, "dev"},
		{[]Option{WithVersion("1.4.0")}, "1.4.0"},
	} {
		s := New(nil, tc.opts...)
		srv := httptest.NewServer(s.Handler())
		c := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
		res, err := c.GetServiceInfo(context.Background(), connect.NewRequest(&pb.GetServiceInfoRequest{}))
		srv.Close()
		s.Close()
		if err != nil {
			t.Fatal(err)
		}
		exe, _ := os.Executable()
		if res.Msg.Version != tc.want || res.Msg.Executable != exe || int(res.Msg.Pid) != os.Getpid() || time.Since(res.Msg.Started.AsTime()) > time.Minute {
			t.Errorf("info = %v, want version %q, executable %q", res.Msg, tc.want, exe)
		}
	}
}
