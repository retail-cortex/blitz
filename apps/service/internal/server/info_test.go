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
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
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

// The service reads its own log: the days there are, and a day's records
// by level and text; with logging off there's none.
func TestReadLog(t *testing.T) {
	dir := t.TempDir()
	log := `{"time":"2026-09-28T10:00:01Z","level":"INFO","msg":"workspace opened","workspace":"/p"}
{"time":"2026-09-28T10:00:02Z","level":"ERROR","msg":"turn failed","error":"quota exceeded"}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blitz-2026-09-28.jsonl"), []byte(log), 0o600))
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		dir  string
		days []string
	}{
		{"on", dir, []string{"2026-09-28"}},
		{"off", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(nil, WithLogDir(tc.dir))
			srv := httptest.NewServer(s.Handler())
			defer srv.Close()
			defer s.Close()
			c := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
			days, err := c.ListLogDays(ctx, connect.NewRequest(&pb.ListLogDaysRequest{}))
			require.NoError(t, err)
			assert.Equal(t, tc.days, days.Msg.Days)
			assert.Equal(t, tc.dir, days.Msg.Dir)
			res, err := c.ReadLog(ctx, connect.NewRequest(&pb.ReadLogRequest{MinLevel: "warn"}))
			require.NoError(t, err)
			if tc.dir == "" {
				assert.Empty(t, res.Msg.Entries)
				return
			}
			require.Len(t, res.Msg.Entries, 1)
			e := res.Msg.Entries[0]
			assert.Equal(t, "turn failed", e.Message)
			assert.Equal(t, "ERROR", e.Level)
			assert.Equal(t, "quota exceeded", e.Attrs[0].Value)
			assert.Equal(t, int32(1), res.Msg.Matched)
			_, err = c.ReadLog(ctx, connect.NewRequest(&pb.ReadLogRequest{Day: "../etc"}))
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

// A log folder that can't be read fails ListLogDays as INTERNAL.
func TestListLogDaysFails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-folder")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	s := New(nil, WithLogDir(file))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	defer s.Close()
	c := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
	_, err := c.ListLogDays(context.Background(), connect.NewRequest(&pb.ListLogDaysRequest{}))
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

// A workspace whose model can't be built says why in GetModel.
func TestGetModelReportsAnUnavailableModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MODENV_PREFIX", "")
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "LLM_PROVIDER"} {
		t.Setenv(k, "")
	}
	s := New(func(ctx context.Context, dir string) (*engine.Workspace, error) {
		cfg := config.DefaultConfig()
		cfg.Tools.WorkspaceDir = dir
		cfg.Session.StorageDir = t.TempDir()
		return engine.Open(ctx, cfg, engine.Options{})
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	defer s.Close()
	c := pb.NewWorkspaceServiceClient(http.DefaultClient, srv.URL)
	m, err := c.GetModel(context.Background(), connect.NewRequest(&pb.GetModelRequest{Workspace: t.TempDir()}))
	require.NoError(t, err)
	assert.NotEmpty(t, m.Msg.Unavailable)
}
