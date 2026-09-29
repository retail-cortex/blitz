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
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		require.NoError(t, err)
		exe, _ := os.Executable()
		assert.Equal(t, tc.want, res.Msg.Version, "info = %v, want version %q, executable %q", res.Msg, tc.want, exe)
		assert.Equal(t, exe, res.Msg.Executable, "info = %v, want version %q, executable %q", res.Msg, tc.want, exe)
		assert.Equal(t, os.Getpid(), int(res.Msg.Pid), "info = %v, want version %q, executable %q", res.Msg, tc.want, exe)
		assert.LessOrEqual(t, time.Since(res.Msg.Started.AsTime()), time.Minute, "info = %v, want version %q, executable %q", res.Msg, tc.want, exe)
		assert.False(t, res.Msg.Replaced, "the test binary counts as replaced")
	}
}

// A program file replaced (a new package or build installed) or removed
// since the service started means it runs the old program.
func TestReplaced(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "blitzd")
	require.NoError(t, os.WriteFile(exe, []byte("old"), 0o755))
	was, err := os.Stat(exe)
	require.NoError(t, err)

	assert.False(t, replaced(nil, exe), "unknown")
	assert.False(t, replaced(was, exe), "the same file")
	// As dpkg and installers do: a new file renamed over the old one.
	next := filepath.Join(dir, "blitzd.new")
	require.NoError(t, os.WriteFile(next, []byte("new"), 0o755))
	require.NoError(t, os.Rename(next, exe))
	assert.True(t, replaced(was, exe), "a new file in its place")
	require.NoError(t, os.Remove(exe))
	assert.True(t, replaced(was, exe), "removed")
}
