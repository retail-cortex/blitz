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

package pagehost

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Preferences that can't be read or saved are reported, with the defaults.
func TestPrefsErrors(t *testing.T) {
	dir := t.TempDir()
	s := NewPrefsStore(dir) // a directory where the file goes
	p, err := s.Load()
	assert.Error(t, err)
	assert.Equal(t, Defaults(), p)

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, err = NewPrefsStore(filepath.Join(file, "desktop.json")).Save(Prefs{})
	assert.Error(t, err, "a directory it can't make")
	_, err = NewPrefsStore(dir).Save(Prefs{})
	assert.Error(t, err, "a directory where the file goes")
	if os.Geteuid() != 0 {
		ro := filepath.Join(t.TempDir(), "ro")
		require.NoError(t, os.Mkdir(ro, 0o500))
		_, err = NewPrefsStore(filepath.Join(ro, "desktop.json")).Save(Prefs{})
		assert.Error(t, err, "a directory it can't write")
	}
}

// The CLI calls find the app's blitz in its runfiles, ask the
// login shell for PATH, and link it.
func TestCLIFromRunfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", "")
	root := t.TempDir()
	t.Setenv("RUNFILES_DIR", root)
	if _, err := findCLI(); err == nil {
		t.Skip("a blitz is beside the test")
	}
	_, err := InstallCLI()
	assert.ErrorContains(t, err, "isn't installed beside the app")
	assert.Empty(t, CurrentCLIStatus().CLI)

	cli := program(t, filepath.Join(root, "_main", "apps", "cli"), "blitz")
	shell := filepath.Join(t.TempDir(), "zsh")
	require.NoError(t, os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'BLITZ_PATH=/nowhere\\n'\n"), 0o755))
	t.Setenv("SHELL", shell)
	got, err := InstallCLI()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".local", "bin", "blitz"), got.Link)
	assert.Equal(t, filepath.Join(home, ".zshrc"), got.Profile)
	s := CurrentCLIStatus()
	assert.Equal(t, cli, s.CLI)
	assert.Empty(t, s.OnPath, "the login shell's PATH doesn't have it yet")

	// Linking again replaces the link with this app's.
	again, err := ReinstallCLI()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".local", "bin", "blitz"), again.Link)
	target, err := os.Readlink(again.Link)
	require.NoError(t, err)
	assert.Equal(t, cli, target)

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

// writable tells a directory it can write in.
func TestWritable(t *testing.T) {
	assert.True(t, writable(t.TempDir()))
	assert.False(t, writable(filepath.Join(t.TempDir(), "missing")))
}

// Without a home directory there's nowhere to link the CLI.
func TestInstallCLIWithoutHome(t *testing.T) {
	root := t.TempDir()
	program(t, filepath.Join(root, "_main", "apps", "cli"), "blitz")
	t.Setenv("RUNFILES_DIR", root)
	t.Setenv("HOME", "")
	_, err := InstallCLI()
	assert.Error(t, err)
	_, err = ReinstallCLI()
	assert.Error(t, err)
}
