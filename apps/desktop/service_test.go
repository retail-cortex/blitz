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
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/socket"
)

// The app finds its blitzd in Bazel's runfiles when Bazel runs it, so it
// can install and restart the service without the CLI.
func TestFindServiceInRunfiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "_main", "apps", "service")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "blitzd")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNFILES_DIR", root)
	t.Setenv("PATH", t.TempDir())
	got, err := findService()
	want, _ := filepath.EvalSymlinks(bin)
	if err != nil || got != want {
		t.Errorf("findService() = %q, %v; want %q", got, err, want)
	}
	t.Setenv("RUNFILES_DIR", t.TempDir())
	if got, err := findService(); err == nil && got == want {
		t.Errorf("found %q without runfiles", got)
	}
}

// A service that isn't a login item (started by hand) stops by its
// process ID, as GetServiceInfo reports it.
func TestStopServiceByPID(t *testing.T) {
	bin, err := findService()
	if err != nil {
		t.Skip("blitzd isn't in the test's runfiles")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)                 // no login item here
	dir, err := os.MkdirTemp("/tmp", "bd") // socket paths must be short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	cmd := exec.Command(bin, "--socket", sock)
	cmd.Env = append(os.Environ(), "HOME="+home, "GEMINI_API_KEY=", "GOOGLE_API_KEY=", "MODENV_PREFIX=")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { cmd.Process.Kill() })
	for deadline := time.Now().Add(10 * time.Second); !socket.Running(sock); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("blitzd didn't start")
		}
	}

	a := &App{socket: sock}
	if err := a.StopService(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("blitzd still running")
	}
	if socket.Running(sock) {
		t.Error("the socket still answers")
	}
}
