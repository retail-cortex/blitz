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
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The program each kind of login entry runs is read back as written.
func TestEntryProgram(t *testing.T) {
	odd := `/opt/blitz "x" & $co/blitz-tray`
	for _, tc := range []struct {
		name string
		data string
		want string
	}{
		{"an autostart entry", trayDesktopEntry(odd), odd},
		{"a systemd unit", systemdUnitFile(odd), odd},
		{"the tray's plist", trayPlist(odd), odd},
		{"the service's plist", launchdPlist(odd, "/tmp/log"), odd},
		{"an unquoted Exec", "[Desktop Entry]\nExec=/usr/bin/blitz-tray --flag\n", "/usr/bin/blitz-tray"},
		{"an unterminated quote", "Exec=\"/a b", "/a b"},
		{"a trailing backslash", "Exec=\"/a\\", "/a"},
		{"no program", "[Desktop Entry]\nName=x\n", ""},
		{"a plist without strings", "<key>ProgramArguments</key><array></array>", ""},
	} {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, entryProgram(tc.data)) })
	}
}

// An entry naming a program that's gone is rewritten for the new one; an
// entry whose program is there, or no entry, is left alone.
func TestRepair(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("login items are files only on macOS and Linux")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	ran := record(t)
	dir := t.TempDir()
	here := filepath.Join(dir, "blitz-tray")
	require.NoError(t, os.WriteFile(here, nil, 0o755))
	gone := filepath.Join(dir, "old", "blitz-tray")

	fixed, err := RepairTray(here)
	require.NoError(t, err)
	assert.False(t, fixed, "no entry")
	assert.Empty(t, TrayProgram())

	require.NoError(t, InstallTray(here))
	fixed, err = RepairTray(filepath.Join(dir, "other"))
	require.NoError(t, err)
	assert.False(t, fixed, "its program is there")
	assert.Equal(t, here, TrayProgram())

	require.NoError(t, InstallTray(gone))
	fixed, err = RepairTray(here)
	require.NoError(t, err)
	assert.True(t, fixed)
	assert.Equal(t, here, TrayProgram())

	blitzd := filepath.Join(dir, "blitzd")
	require.NoError(t, os.WriteFile(blitzd, nil, 0o755))
	fixed, err = RepairService(blitzd)
	require.NoError(t, err)
	assert.False(t, fixed, "no login item")
	require.NoError(t, Install(filepath.Join(dir, "old", "blitzd")))
	*ran = nil
	fixed, err = RepairService(blitzd)
	require.NoError(t, err)
	assert.True(t, fixed)
	assert.Equal(t, blitzd, ServiceProgram())
	assert.NotEmpty(t, *ran, "the repaired item is started")
}
