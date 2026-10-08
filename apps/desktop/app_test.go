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
	a := &App{prefs: &prefsStore{path: filepath.Join(t.TempDir(), "desktop.json")}}
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

// Preferences that can't be read or saved are reported, with the defaults.
func TestPrefsErrors(t *testing.T) {
	dir := t.TempDir()
	s := &prefsStore{path: dir} // a directory where the file goes
	p, err := s.load()
	assert.Error(t, err)
	assert.Equal(t, defaults(), p)

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, err = (&prefsStore{path: filepath.Join(file, "desktop.json")}).save(Prefs{})
	assert.Error(t, err, "a directory it can't make")
	_, err = (&prefsStore{path: dir}).save(Prefs{})
	assert.Error(t, err, "a directory where the file goes")
	if os.Geteuid() != 0 {
		ro := filepath.Join(t.TempDir(), "ro")
		require.NoError(t, os.Mkdir(ro, 0o500))
		_, err = (&prefsStore{path: filepath.Join(ro, "desktop.json")}).save(Prefs{})
		assert.Error(t, err, "a directory it can't write")
	}
}

// With no unsaved changes the window closes without asking.
func TestBeforeCloseWithoutChanges(t *testing.T) {
	a := &App{}
	a.SetUnsaved(Unsaved{Message: "unsaved", Quit: "Quit", Cancel: "Cancel"})
	assert.Equal(t, "unsaved", a.unsaved.Message)
	a.SetUnsaved(Unsaved{})
	assert.False(t, a.beforeClose(context.TODO()), "nothing to lose: close")
}

// The page's CLI calls find this app's blitz in its runfiles, ask the
// login shell for PATH, and link it.
func TestCLIFromRunfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	root := t.TempDir()
	t.Setenv("RUNFILES_DIR", root)
	a := &App{}
	if _, err := findCLI(); err == nil {
		t.Skip("a blitz is beside the test")
	}
	_, err := a.InstallCLI()
	assert.ErrorContains(t, err, "isn't installed beside the app")
	assert.Empty(t, a.CLIStatus().CLI)

	cli := program(t, filepath.Join(root, "_main", "apps", "cli"), "blitz")
	shell := filepath.Join(t.TempDir(), "zsh")
	require.NoError(t, os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'BLITZ_PATH=/nowhere\\n'\n"), 0o755))
	t.Setenv("SHELL", shell)
	got, err := a.InstallCLI()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".local", "bin", "blitz"), got.Link)
	assert.Equal(t, filepath.Join(home, ".zshrc"), got.Profile)
	s := a.CLIStatus()
	assert.Equal(t, cli, s.CLI)
	assert.Empty(t, s.OnPath, "the login shell's PATH doesn't have it yet")

	t.Setenv("SHELL", "")
	want := "/bin/sh"
	if goruntime.GOOS == "darwin" {
		want = "/bin/zsh"
	}
	assert.Equal(t, want, loginShell())
}

// installCLI and addToPath report what they can't write; linkDir passes
// over a system directory it can't write.
func TestInstallCLIErrors(t *testing.T) {
	cli := program(t, t.TempDir(), "blitz")
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, err := installCLI(cli, file, "/bin/sh", "linux", nil)
	assert.Error(t, err, "a home that's a file")

	_, err = addToPath(file, "/bin/sh", "linux", "/x")
	assert.Error(t, err, "a profile under a file")
	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, ".profile"), 0o700))
	_, err = addToPath(home, "/bin/sh", "linux", "/x")
	assert.Error(t, err, "a profile that's a directory")

	home = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "fish"), nil, 0o644))
	got, err := installCLI(cli, home, "/usr/bin/fish", "linux", []string{"", "/usr/bin"})
	assert.ErrorContains(t, err, "couldn't add")
	assert.Equal(t, filepath.Join(home, ".local", "bin", "blitz"), got.Link, "linked all the same")

	if os.Geteuid() != 0 && !writable("/usr/local/bin") {
		dir, onPath := linkDir(home, []string{"/usr/local/bin"})
		assert.Equal(t, filepath.Join(home, ".local", "bin"), dir)
		assert.False(t, onPath)
	}
}

// Links other than web and mail ones are refused before anything opens;
// writable tells a directory it can write in.
func TestOpenURLRefusesAndWritable(t *testing.T) {
	assert.ErrorContains(t, (&App{}).OpenURL("file:///etc/passwd"), "only web and mail links")
	assert.True(t, writable(t.TempDir()))
	assert.False(t, writable(filepath.Join(t.TempDir(), "missing")))
}

// Without a home directory there's nowhere to link the CLI.
func TestInstallCLIWithoutHome(t *testing.T) {
	root := t.TempDir()
	program(t, filepath.Join(root, "_main", "apps", "cli"), "blitz")
	t.Setenv("RUNFILES_DIR", root)
	t.Setenv("HOME", "")
	_, err := (&App{}).InstallCLI()
	assert.Error(t, err)
}
