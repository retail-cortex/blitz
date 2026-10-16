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
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/pagehost"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noSystem makes the login item's system commands no-ops, recording them.
func noSystem(t *testing.T) *[]string {
	t.Helper()
	var ran []string
	old := loginitem.RunSystem
	loginitem.RunSystem = func(name string, args ...string) error {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { loginitem.RunSystem = old })
	return &ran
}

// listening is a socket something answers on, closed with the test.
func listening(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "bd") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	l, err := socket.Listen(sock)
	require.NoError(t, err)
	t.Cleanup(func() { l.Close() })
	return sock
}

// The status says what answers, what's installed, and which programs this
// app would use.
func TestServiceStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	bin := t.TempDir()
	tray := program(t, bin, "blitz-tray")
	t.Setenv("PATH", bin)
	orig := trayDirs
	trayDirs = func() []string { return nil }
	t.Cleanup(func() { trayDirs = orig })

	sock := listening(t)
	s := (&App{socket: sock}).ServiceStatus()
	assert.True(t, s.Running)
	assert.False(t, s.Installed)
	assert.Equal(t, sock, s.Socket)
	assert.Equal(t, tray, s.Tray)
	assert.False(t, s.TrayInstalled)
	assert.Equal(t, version, (&App{}).Version())
}

// findTray looks in its directories before PATH; trayDirs are beside the
// app and in its runfiles.
func TestFindTray(t *testing.T) {
	dir := t.TempDir()
	want := program(t, dir, "blitz-tray")
	orig := trayDirs
	t.Cleanup(func() { trayDirs = orig })
	trayDirs = func() []string { return []string{t.TempDir(), dir} }
	t.Setenv("PATH", t.TempDir())
	got, err := findTray()
	require.NoError(t, err)
	assert.Equal(t, want, got)

	root := t.TempDir()
	t.Setenv("RUNFILES_DIR", root)
	assert.Contains(t, orig(), filepath.Join(root, "_main", "apps", "tray"))
}

// InstallService (and Restart, with nothing to stop) installs the blitzd
// it finds as the login item, and says when there's none.
func TestInstallService(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("writes a systemd unit")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("RUNFILES_DIR", t.TempDir())
	ran := noSystem(t)
	a := &App{}
	if _, err := findService(); err != nil {
		assert.Error(t, a.InstallService(), "no blitzd anywhere")
	}

	root := t.TempDir()
	want := program(t, filepath.Join(root, "_main", "apps", "service"), "blitzd")
	t.Setenv("RUNFILES_DIR", root)
	a.socket = filepath.Join(t.TempDir(), "none.sock")
	require.NoError(t, a.RestartService(0), "nothing to stop; installs")
	assert.True(t, loginitem.Installed())
	assert.Contains(t, *ran, "systemctl --user restart blitz.service")
	path, err := loginitem.Path()
	require.NoError(t, err)
	unit, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(unit), want)
}

// StopService stops the login item's service; one still answering after
// that, with no process to signal, is reported, and Restart stops there.
func TestStopServiceLoginItemAndStillRunning(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("writes a systemd unit")
	}
	t.Setenv("HOME", t.TempDir())
	ran := noSystem(t)
	require.NoError(t, loginitem.Install("/opt/blitz/blitzd"))
	*ran = nil
	a := &App{socket: filepath.Join(t.TempDir(), "none.sock")}
	require.NoError(t, a.StopService(0))
	assert.Equal(t, []string{"systemctl --user stop blitz.service"}, *ran)
	require.NoError(t, loginitem.Uninstall())

	a = &App{socket: listening(t)}
	assert.ErrorContains(t, a.StopService(0), "still running")
	assert.ErrorContains(t, a.RestartService(0), "still running")
	assert.False(t, a.waitStopped(300*time.Millisecond))

	a = &App{socket: filepath.Join(t.TempDir(), "none.sock")}
	assert.NoError(t, a.StopService(0), "nothing runs")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("RUNFILES_DIR", t.TempDir())
	if _, err := findService(); err != nil {
		assert.Error(t, a.RestartService(0), "stopped, but no blitzd to start")
	}
}

// ProgramExists is true for files only; preferences go through the store.
func TestProgramExistsAndPrefs(t *testing.T) {
	a := &App{prefs: pagehost.NewPrefsStore(filepath.Join(t.TempDir(), "desktop.json"))}
	dir := t.TempDir()
	assert.True(t, a.ProgramExists(program(t, dir, "x")))
	assert.False(t, a.ProgramExists(dir))
	assert.False(t, a.ProgramExists(filepath.Join(dir, "missing")))

	p, err := a.GetPrefs()
	require.NoError(t, err)
	saved, err := a.SavePrefs(p)
	require.NoError(t, err)
	got, err := a.GetPrefs()
	require.NoError(t, err)
	assert.Equal(t, saved, got)
}

// With no unsaved changes the window closes without asking.
func TestBeforeCloseWithoutChanges(t *testing.T) {
	a := &App{}
	a.SetUnsaved(Unsaved{Message: "unsaved", Quit: "Quit", Cancel: "Cancel"})
	assert.Equal(t, "unsaved", a.unsaved.Message)
	a.SetUnsaved(Unsaved{})
	assert.False(t, a.beforeClose(context.TODO()), "nothing to lose: close")
}

// Links other than web and mail ones are refused before anything opens.
func TestOpenURLRefuses(t *testing.T) {
	assert.ErrorContains(t, (&App{}).OpenURL("file:///etc/passwd"), "only web and mail links")
}

// program writes an executable file at dir/name and returns its path,
// symlinks resolved.
func program(t *testing.T, dir, name string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755))
	p, err := filepath.EvalSymlinks(p)
	require.NoError(t, err)
	return p
}

// The CLI's calls are the shared host's: with no blitz beside the app,
// there's nothing to link.
func TestCLIBindings(t *testing.T) {
	t.Setenv("RUNFILES_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	a := &App{}
	if a.CLIStatus().CLI != "" {
		t.Skip("a blitz is beside the test")
	}
	_, err := a.InstallCLI()
	assert.ErrorContains(t, err, "isn't installed beside the app")
	_, err = a.ReinstallCLI()
	assert.ErrorContains(t, err, "isn't installed beside the app")
}
