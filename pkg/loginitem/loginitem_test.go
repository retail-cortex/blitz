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

package loginitem

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// record replaces RunSystem for a test and returns what ran.
func record(t *testing.T) *[]string {
	var ran []string
	old := RunSystem
	RunSystem = func(name string, args ...string) error {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { RunSystem = old })
	return &ran
}

func TestInstallStopUninstall(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("login items are only supported on macOS and Linux")
	}
	t.Setenv("HOME", t.TempDir())
	ran := record(t)
	bin := "/opt/blitz & co/blitzd" // escaped in the plist, quoted in the unit
	require.NoError(t, Install(bin))
	path, _ := Path()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "login item not written")
	require.True(t, Installed(), "login item not written: %v", err)
	switch goruntime.GOOS {
	case "darwin":
		dec := xml.NewDecoder(strings.NewReader(string(data)))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("plist isn't well-formed: %v", err)
			}
		}
		assert.Contains(t, string(data), "/opt/blitz &amp; co/blitzd", "plist doesn't run blitzd:\n%s", data)
		assert.Len(t, *ran, 2, "install ran %q", *ran)
		assert.True(t, strings.HasPrefix((*ran)[0], "launchctl bootout gui/"), "install ran %q", *ran)
		assert.True(t, strings.HasPrefix((*ran)[1], "launchctl bootstrap gui/"), "install ran %q", *ran)
	case "linux":
		assert.Contains(t, string(data), `ExecStart="/opt/blitz & co/blitzd"`, "unit doesn't run blitzd:\n%s", data)
		// A running unit restarts, so a reinstall runs the new program.
		assert.Equal(t, "systemctl --user daemon-reload; systemctl --user enable blitz.service; systemctl --user restart blitz.service", strings.Join(*ran, "; "), "install ran %q", *ran)
	}

	*ran = nil
	err = Stop()
	assert.NoError(t, err, "stop: %v, ran %q", err, *ran)
	assert.Len(t, *ran, 1, "stop: %v, ran %q", err, *ran)
	assert.True(t, Installed(), "stop removed the login item")
	err = Uninstall()
	assert.NoError(t, err, "uninstall: %v, installed %v", err, Installed())
	assert.False(t, Installed(), "uninstall: %v, installed %v", err, Installed())
}

func TestFindService(t *testing.T) {
	beside, onPath := t.TempDir(), t.TempDir()
	for _, d := range []string{beside, onPath} {
		t.Run(d, func(t *testing.T) {
			require.NoError(t, os.WriteFile(filepath.Join(d, "blitzd"), []byte("#!/bin/sh\n"), 0o755))
		})
	}
	t.Setenv("PATH", onPath)
	want := func(d string) string { p, _ := filepath.EvalSymlinks(filepath.Join(d, "blitzd")); return p }
	got, err := FindService(t.TempDir(), beside)
	assert.NoError(t, err, "beside: %q,", got)
	assert.Equal(t, want(beside), got, "beside: %q, %v", got, err)
	got, err = FindService(t.TempDir())
	assert.NoError(t, err, "on PATH: %q,", got)
	assert.Equal(t, want(onPath), got, "on PATH: %q, %v", got, err)
	t.Setenv("PATH", t.TempDir())
	_, err = FindService()
	assert.Error(t, err, "found a blitzd that isn't there")
}

// Start and Restart need the login item, and ask the system to start or
// restart it.
func TestStartRestart(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("login items are only supported on macOS and Linux")
	}
	t.Setenv("HOME", t.TempDir())
	ran := record(t)
	assert.ErrorIs(t, Start(), ErrNotInstalled)
	assert.ErrorIs(t, Restart(), ErrNotInstalled)
	assert.Empty(t, *ran, "ran without a login item")
	require.NoError(t, Install("/opt/blitz/blitzd"))
	for _, tc := range []struct {
		name  string
		f     func() error
		linux string
		mac   string
	}{
		{"start", Start, "systemctl --user start blitz.service", "launchctl kickstart gui/"},
		{"restart", Restart, "systemctl --user restart blitz.service", "launchctl kickstart -k gui/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			*ran = nil
			require.NoError(t, tc.f())
			last := (*ran)[len(*ran)-1]
			if goruntime.GOOS == "linux" {
				assert.Equal(t, tc.linux, last)
			} else {
				assert.True(t, strings.HasPrefix(last, tc.mac), "ran %q", *ran)
			}
		})
	}
}

// The tray starts at login from its own entry, which runs the program
// given, quoted as the format needs.
func TestInstallTray(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("login items are only supported on macOS and Linux")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	record(t)
	bin := `/opt/blitz "x" & $co/blitz-tray`
	require.False(t, TrayInstalled())
	require.NoError(t, InstallTray(bin))
	require.True(t, TrayInstalled())
	path, _ := TrayPath()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	switch goruntime.GOOS {
	case "linux":
		assert.Equal(t, filepath.Join(os.Getenv("HOME"), ".config", "autostart", "blitz-tray.desktop"), path)
		assert.Contains(t, string(data), `Exec="/opt/blitz \"x\" & \$co/blitz-tray"`)
		assert.Contains(t, string(data), "[Desktop Entry]\nType=Application\n")
	case "darwin":
		assert.Contains(t, string(data), "<string>/opt/blitz &#34;x&#34; &amp; $co/blitz-tray</string>")
	}
	require.NoError(t, UninstallTray())
	assert.False(t, TrayInstalled())
	require.NoError(t, UninstallTray(), "uninstalling twice")

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(os.Getenv("HOME"), "cfg"))
	if goruntime.GOOS == "linux" {
		p, _ := TrayPath()
		assert.Equal(t, filepath.Join(os.Getenv("HOME"), "cfg", "autostart", "blitz-tray.desktop"), p)
	}
}

// wellFormed fails the test unless data is well-formed XML.
func wellFormed(t *testing.T, data string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(data))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return
		}
		require.NoError(t, err, "plist isn't well-formed:\n%s", data)
	}
}

// The launchd plists are well-formed, with the program and log file escaped;
// they're built on every OS, so they're checked on every OS.
func TestPlists(t *testing.T) {
	bin := `/opt/a & "b"/blitzd`
	svc := launchdPlist(bin, "/logs/<x>.log")
	wellFormed(t, svc)
	assert.Contains(t, svc, "<string>"+Label+"</string>")
	assert.Contains(t, svc, "<string>/opt/a &amp; &#34;b&#34;/blitzd</string>")
	assert.Contains(t, svc, "<string>/logs/&lt;x&gt;.log</string>")
	assert.Contains(t, svc, "<key>SuccessfulExit</key>", "restarted after a failure")

	tray := trayPlist(bin)
	wellFormed(t, tray)
	assert.Contains(t, tray, "<string>"+TrayLabel+"</string>")
	assert.Contains(t, tray, "<string>/opt/a &amp; &#34;b&#34;/blitzd</string>")
}

// The log file is under ~/.blitz, and launchd's domain is the user's GUI
// session.
func TestLogFileAndDomain(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	assert.Equal(t, filepath.Join(home, ".blitz", "logs", "service.log"), LogFile())
	assert.Equal(t, "gui/"+strconv.Itoa(os.Getuid()), domain())
}

// RunSystem runs the command, and reports a failure with its output.
func TestRunSystem(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	require.NoError(t, RunSystem("sh", "-c", "exit 0"))
	err := RunSystem("sh", "-c", "echo boom; exit 3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sh -c echo boom; exit 3")
	assert.Contains(t, err.Error(), ": boom")
}

// Install stops at the first system command that fails, and reports it;
// Stop, Start and Restart report the system's error too.
func TestInstallFailures(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("the systemd steps")
	}
	t.Setenv("HOME", t.TempDir())
	for _, failing := range []string{"daemon-reload", "enable", "restart"} {
		t.Run(failing, func(t *testing.T) {
			var ran []string
			old := RunSystem
			RunSystem = func(name string, args ...string) error {
				ran = append(ran, strings.Join(args, " "))
				if strings.Contains(strings.Join(args, " "), failing) {
					return errors.New("failed: " + failing)
				}
				return nil
			}
			t.Cleanup(func() { RunSystem = old })
			assert.EqualError(t, Install("/b/blitzd"), "failed: "+failing)
			assert.Contains(t, ran[len(ran)-1], failing, "nothing runs after the failure")
		})
	}
}

// Installing and uninstalling report an entry they can't write or remove.
func TestInstallFileErrors(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("the systemd paths")
	}
	record(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Files where the entries' directories should be.
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "systemd"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "systemd", "user"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "autostart"), nil, 0o644))
	assert.Error(t, Install("/b/blitzd"), "the unit's directory can't be made")
	assert.Error(t, InstallTray("/b/blitz-tray"), "the autostart directory can't be made")

	home = t.TempDir()
	t.Setenv("HOME", home)
	path, err := Path()
	require.NoError(t, err)
	// A directory, not empty, where the unit should be.
	require.NoError(t, os.MkdirAll(filepath.Join(path, "x"), 0o755))
	assert.Error(t, Install("/b/blitzd"), "the unit can't be written")
	assert.Error(t, Uninstall(), "the unit can't be removed")
	tray, err := TrayPath()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(tray, "x"), 0o755))
	assert.Error(t, UninstallTray(), "the tray's entry can't be removed")
}

// Beside is the test binary's own directory.
func TestBeside(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	exe, err = filepath.EvalSymlinks(exe)
	require.NoError(t, err)
	assert.Equal(t, filepath.Dir(exe), Beside())
}
