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
	"bytes"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// app makes an installed app at dir/name with its program saying what.
func app(t *testing.T, dir, name, what string) string {
	t.Helper()
	root := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "resources", "app", "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Blitz.exe"), []byte(what), 0o755))
	return root
}

// The update waits for the app, stops a service running from it, swaps
// them, and starts the service and the new app.
func TestUpdate(t *testing.T) {
	f, a := newFake(t)
	f.running = true
	dir := t.TempDir()
	root := app(t, dir, "Blitz", "old")
	staged := app(t, filepath.Join(dir, "updates"), "Blitz", "new")
	require.NoError(t, os.WriteFile(filepath.Join(a.Beside, "blitzd"), []byte("#!/bin/sh\n"), 0o755))
	a.Shutdown = func() error { f.running = false; return nil }
	var waited int
	var log bytes.Buffer
	err := Update{
		Staged: staged, Root: root, Relaunch: filepath.Join(root, "Blitz.exe"), WaitPID: 42, Actions: a,
		ServiceExe: func() string { return filepath.Join(root, "resources", "app", "bin", "blitzd") },
		Exited:     func(pid int, _ time.Duration) bool { waited = pid; return true },
		Log:        &log,
	}.Run()
	require.NoError(t, err, log.String())
	assert.Equal(t, 42, waited)
	data, _ := os.ReadFile(filepath.Join(root, "Blitz.exe"))
	assert.Equal(t, "new", string(data))
	assert.Contains(t, log.String(), "stopping the service")
	assert.True(t, f.running, "started again")
	if goruntime.GOOS == "darwin" {
		assert.Contains(t, f.ran, "open "+filepath.Join(root, "Blitz.exe"))
	} else {
		assert.Contains(t, f.ran, "Blitz.exe")
	}
}

// A service running from elsewhere is left alone; an app that doesn't
// quit isn't touched; a failed swap still starts the (old) app.
func TestUpdateRefusals(t *testing.T) {
	_, a := newFake(t)
	dir := t.TempDir()
	root := app(t, dir, "Blitz", "old")
	var log bytes.Buffer
	u := Update{
		Staged: filepath.Join(dir, "missing"), Root: root, Relaunch: filepath.Join(root, "Blitz.exe"), Actions: a,
		ServiceExe: func() string { return "/usr/local/bin/blitzd" },
		Exited:     func(int, time.Duration) bool { return true },
		Log:        &log,
	}
	assert.ErrorContains(t, u.Run(), "the update isn't there")
	data, _ := os.ReadFile(filepath.Join(root, "Blitz.exe"))
	assert.Equal(t, "old", string(data))
	assert.NotContains(t, log.String(), "stopping the service")

	u.WaitPID = 7
	u.Exited = func(int, time.Duration) bool { return false }
	assert.ErrorContains(t, u.Run(), "didn't quit")

	assert.True(t, Exited(-12345, time.Second) || goruntime.GOOS == "windows", "a process that isn't there has ended")
	assert.False(t, Exited(os.Getpid(), 100*time.Millisecond), "this one hasn't")
}

func TestWithin(t *testing.T) {
	assert.True(t, within("/a/b/c", "/a/b"))
	assert.False(t, within("/a/bc", "/a/b"))
	assert.False(t, within("/x", "/a/b"))
}

// A service that won't stop stops the update; what fails after the swap
// is logged, not returned.
func TestUpdateServiceFailures(t *testing.T) {
	f, a := newFake(t)
	f.running = true
	dir := t.TempDir()
	root := app(t, dir, "Blitz", "old")
	inside := func() string { return filepath.Join(root, "resources", "app", "bin", "blitzd") }
	a.Shutdown = func() error { return errors.New("no") }
	var log bytes.Buffer
	u := Update{Staged: app(t, filepath.Join(dir, "u1"), "Blitz", "new"), Root: root, Actions: a, ServiceExe: inside, Exited: func(int, time.Duration) bool { return true }, Log: &log}
	assert.ErrorContains(t, u.Run(), "stopping the service")
	data, _ := os.ReadFile(filepath.Join(root, "Blitz.exe"))
	assert.Equal(t, "old", string(data), "not updated")

	// Stopped, but no blitzd to start again, and the app won't start: the
	// update stands, and the log says so.
	a.Shutdown = func() error { f.running = false; return nil }
	a.Run = func(string, ...string) error { return errors.New("can't start") }
	u = Update{Staged: app(t, filepath.Join(dir, "u2"), "Blitz", "new"), Root: root, Relaunch: filepath.Join(root, "Blitz.exe"), Actions: a, ServiceExe: inside, Exited: func(int, time.Duration) bool { return true }, Log: &log}
	require.NoError(t, u.Run())
	assert.Contains(t, log.String(), "starting the service again")
	assert.Contains(t, log.String(), "starting the app again")
}
