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
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMenuFor(t *testing.T) {
	cases := []struct {
		name                 string
		s                    Status
		state                string
		start, stop, restart bool
	}{
		{"running, with its version", Status{Running: true, Installed: true, Version: "1.4.0"}, "Blitz service: running (1.4.0)", false, true, true},
		{"running, too old to say", Status{Running: true}, "Blitz service: running", false, true, true},
		{"stopped login item", Status{Installed: true}, "Blitz service: stopped", true, false, false},
		{"not installed, not running", Status{}, "Blitz service: not running", true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := MenuFor(c.s)
			assert.Equal(t, c.state, m.State.Title)
			assert.Equal(t, c.state, m.Tooltip)
			assert.False(t, m.State.Enabled, "the state line isn't an action")
			assert.Equal(t, c.s.Running, m.Running)
			assert.Equal(t, []bool{c.start, c.stop, c.restart}, []bool{m.Start.Enabled, m.Stop.Enabled, m.Restart.Enabled})
		})
	}
}

// infoService answers GetServiceInfo, as a running service does.
type infoService struct {
	pb.UnimplementedWorkspaceServiceHandler
}

func (infoService) ListLogDays(context.Context, *connect.Request[pb.ListLogDaysRequest]) (*connect.Response[pb.ListLogDaysResponse], error) {
	return connect.NewResponse(&pb.ListLogDaysResponse{Dir: "/var/log/blitz"}), nil
}

func (infoService) GetServiceInfo(context.Context, *connect.Request[pb.GetServiceInfoRequest]) (*connect.Response[pb.GetServiceInfoResponse], error) {
	return connect.NewResponse(&pb.GetServiceInfoResponse{Version: "1.4.0", Pid: 4242}), nil
}

func TestProbe(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "blitz.sock")
	installed := func() bool { return true }
	assert.Equal(t, Status{Installed: true}, Probe(context.Background(), sock, installed), "nothing listening")

	ln, err := socket.Listen(sock)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(pb.NewWorkspaceServiceHandler(infoService{}))
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	assert.Equal(t, Status{Running: true, Installed: true, Version: "1.4.0", PID: 4242}, Probe(context.Background(), sock, installed))
	assert.Equal(t, "/var/log/blitz", ServiceLogDir(context.Background(), sock))
	assert.Empty(t, ServiceLogDir(context.Background(), filepath.Join(shortTemp(t), "none.sock")), "nothing listening")
}

// shortTemp is a temporary directory with a path short enough for a Unix
// socket (macOS's is long).
func shortTemp(t *testing.T) string {
	dir, err := os.MkdirTemp("/tmp", "tray")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// fakeSystem records the programs Actions run and the system commands
// the login item runs, and plays the service's state.
type fakeSystem struct {
	ran     []string
	running bool
}

func newFake(t *testing.T) (*fakeSystem, Actions) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // no blitzd, no blitz-desktop
	f := &fakeSystem{}
	old := loginitem.RunSystem
	loginitem.RunSystem = func(name string, args ...string) error {
		f.ran = append(f.ran, name+" "+strings.Join(args, " "))
		cmd := strings.Join(args, " ")
		switch {
		case strings.Contains(cmd, "start") || strings.Contains(cmd, "kickstart") || strings.Contains(cmd, "restart"):
			f.running = true
		case strings.Contains(cmd, "stop") || strings.Contains(cmd, "bootout"):
			f.running = false
		}
		return nil
	}
	t.Cleanup(func() { loginitem.RunSystem = old })
	beside := t.TempDir()
	return f, Actions{
		Run: func(name string, args ...string) error {
			f.ran = append(f.ran, strings.TrimSpace(filepath.Base(name)+" "+strings.Join(args, " ")))
			if filepath.Base(name) == "blitzd" {
				f.running = true
			}
			return nil
		},
		Beside: beside,
		Status: func() Status { return Status{Running: f.running, PID: 0} },
		Wait:   func(running bool, _ time.Duration) bool { return f.running == running },
	}
}

func TestActionsWithALoginItem(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("checks systemd's commands")
	}
	f, a := newFake(t)
	require.NoError(t, loginitem.Install("/opt/blitz/blitzd"))
	f.ran = nil
	require.NoError(t, a.Stop())
	assert.Equal(t, []string{"systemctl --user stop blitz.service"}, f.ran)
	require.NoError(t, a.Start())
	assert.Equal(t, "systemctl --user start blitz.service", f.ran[len(f.ran)-1])
	require.NoError(t, a.Restart())
	assert.Equal(t, "systemctl --user restart blitz.service", f.ran[len(f.ran)-1])
}

func TestActionsWithoutALoginItem(t *testing.T) {
	f, a := newFake(t)
	assert.Error(t, a.Start(), "no blitzd anywhere")
	require.NoError(t, os.WriteFile(filepath.Join(a.Beside, "blitzd"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, a.Start())
	assert.Equal(t, []string{"blitzd"}, f.ran, "blitzd from beside the tray, on its own")
	assert.True(t, f.running)

	// Stopping signals the process that answered; the fake's never stops.
	a.Wait = func(running bool, _ time.Duration) bool { return !running && !f.running }
	err := a.Stop()
	assert.ErrorContains(t, err, "still running")
}

func TestOpenAppAndLogs(t *testing.T) {
	f, a := newFake(t)
	if goruntime.GOOS == "darwin" {
		require.NoError(t, a.OpenApp())
		assert.Equal(t, "open -a Blitz", f.ran[0])
		return
	}
	err := a.OpenApp()
	assert.ErrorContains(t, err, "blitz-desktop isn't installed")
	require.NoError(t, os.WriteFile(filepath.Join(a.Beside, "blitz-desktop"), []byte("x"), 0o755))
	require.NoError(t, a.OpenApp())
	require.NoError(t, a.OpenLogs())
	assert.Equal(t, []string{"blitz-desktop", "xdg-open " + filepath.Join(os.Getenv("HOME"), ".blitz", "logs")}, f.ran)
	info, err := os.Stat(LogDir())
	require.NoError(t, err, "the logs folder is made if missing")
	assert.True(t, info.IsDir())
	assert.False(t, errors.Is(err, os.ErrNotExist))
}

func TestOpenLogsOpensTheServicesFolder(t *testing.T) {
	opener := "xdg-open "
	if goruntime.GOOS == "darwin" {
		opener = "open "
	}
	cases := []struct {
		name     string
		says     bool // the service says where its log is
		settings bool // the settings file's folder is opened
	}{
		{name: "the service's own", says: true},
		{name: "the settings file's when the service doesn't say", settings: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, a := newFake(t)
			said := filepath.Join(t.TempDir(), "svc-logs")
			a.LogDir = func() string {
				if c.says {
					return said
				}
				return ""
			}
			want := said
			if c.settings {
				want = filepath.Join(os.Getenv("HOME"), ".blitz", "logs")
			}
			require.NoError(t, a.OpenLogs())
			assert.Equal(t, []string{opener + want}, f.ran)
		})
	}
}
