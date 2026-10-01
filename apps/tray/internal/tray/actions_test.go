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
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/socket"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// With a login item, the system's failures and a service that doesn't
// come up are reported.
func TestActionsLoginItemFailures(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("systemd")
	}
	_, a := newFake(t)
	require.NoError(t, loginitem.Install("/opt/blitz/blitzd"))
	loginitem.RunSystem = func(string, ...string) error { return errors.New("systemctl failed") }
	assert.ErrorContains(t, a.Start(), "systemctl failed")
	assert.ErrorContains(t, a.Restart(), "systemctl failed")

	loginitem.RunSystem = func(string, ...string) error { return nil }
	a.Wait = func(bool, time.Duration) bool { return false }
	assert.Error(t, a.Start(), "never started")
	assert.Error(t, a.Restart(), "never came back")
}

// Without a login item, a program that won't run is reported, Stop
// signals the service's process, and Restart stops then starts.
func TestActionsWithoutLoginItemMore(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	require.NoError(t, err)
	f, a := newFake(t)
	require.NoError(t, os.WriteFile(filepath.Join(a.Beside, "blitzd"), []byte("#!/bin/sh\n"), 0o755))
	run := a.Run
	a.Run = func(string, ...string) error { return errors.New("exec failed") }
	assert.ErrorContains(t, a.Start(), "exec failed")
	a.Run = run

	// A real process stands in for the service, to be signalled.
	sleeper := exec.Command(sleep, "30")
	require.NoError(t, sleeper.Start())
	exited := make(chan struct{})
	go func() { _ = sleeper.Wait(); close(exited) }()
	a.Status = func() Status { return Status{Running: true, PID: sleeper.Process.Pid} }
	a.Wait = func(running bool, _ time.Duration) bool {
		if running {
			return true
		}
		select {
		case <-exited:
			return true
		case <-time.After(5 * time.Second):
			return false
		}
	}
	require.NoError(t, a.Restart())
	assert.Contains(t, f.ran, "blitzd", "started again")

	a.Wait = func(bool, time.Duration) bool { return false }
	a.Status = func() Status { return Status{} }
	assert.Error(t, a.Restart(), "the stop fails")
}

// The app is found on PATH too; a logs folder that can't be made is
// reported; LogDir falls back to ~/.blitz/logs when the settings don't load.
func TestOpenAppOnPathAndLogErrors(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("Linux's app lookup")
	}
	f, a := newFake(t)
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "blitz-desktop"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", bin)
	require.NoError(t, a.OpenApp())
	assert.Equal(t, []string{"blitz-desktop"}, f.ran)

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	a.LogDir = func() string { return filepath.Join(file, "logs") }
	assert.Error(t, a.OpenLogs())

	home := os.Getenv("HOME")
	t.Setenv("MODENV_PREFIX", "")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[[["), 0o600))
	assert.Equal(t, filepath.Join(home, ".blitz", "logs"), LogDir())
}

// Detached starts a program and doesn't wait for it; one that isn't
// there is an error.
func TestDetached(t *testing.T) {
	require.NoError(t, Detached("true"))
	assert.Error(t, Detached(filepath.Join(t.TempDir(), "missing")))
}

// A service that doesn't answer ListLogDays says nothing about its log.
func TestServiceLogDirUnanswered(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "blitz.sock")
	ln, err := socket.Listen(sock)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(pb.NewWorkspaceServiceHandler(pb.UnimplementedWorkspaceServiceHandler{}))
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })
	assert.Empty(t, ServiceLogDir(context.Background(), sock))
}
