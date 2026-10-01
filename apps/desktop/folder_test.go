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
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenFolder(t *testing.T) {
	var opened []string
	orig := openCommand
	openCommand = func(dir string) *exec.Cmd {
		opened = append(opened, dir)
		return exec.Command("true")
	}
	t.Cleanup(func() { openCommand = orig })

	dir := t.TempDir()
	file := filepath.Join(dir, "run.sh")
	require.NoError(t, os.WriteFile(file, []byte("#!/bin/sh\n"), 0o755))
	a := &App{}
	for _, tc := range []struct {
		name, path string
		ok         bool
	}{
		{"a folder", dir, true},
		{"a file", file, false},
		{"nothing there", filepath.Join(dir, "none"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opened = nil
			err := a.OpenFolder(tc.path)
			if tc.ok {
				assert.NoError(t, err)
				assert.Equal(t, []string{tc.path}, opened)
			} else {
				assert.Error(t, err)
				assert.Empty(t, opened, "opened %v", opened)
			}
		})
	}
}

func TestOpenDocument(t *testing.T) {
	var opened []string
	orig := openCommand
	openCommand = func(p string) *exec.Cmd {
		opened = append(opened, p)
		return exec.Command("true")
	}
	t.Cleanup(func() { openCommand = orig })

	dir := t.TempDir()
	for _, name := range []string{"spec.PDF", "run.sh", "evil.desktop"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o755))
	}
	require.NoError(t, os.Mkdir(filepath.Join(dir, "dir.pdf"), 0o755))
	a := &App{}
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"spec.PDF", true},
		{"run.sh", false},
		{"evil.desktop", false},
		{"dir.pdf", false},
		{"none.pdf", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opened = nil
			p := filepath.Join(dir, tc.name)
			err := a.OpenDocument(p)
			if tc.ok {
				assert.NoError(t, err)
				assert.Equal(t, []string{p}, opened)
			} else {
				assert.Error(t, err)
				assert.Empty(t, opened)
			}
		})
	}
}

// The tray switch runs blitz-tray --install or --uninstall, and says why
// when it fails.
func TestSetTray(t *testing.T) {
	bin := t.TempDir()
	out := filepath.Join(bin, "args")
	script := "#!/bin/sh\necho \"$1\" >> " + out + "\n[ \"$1\" = --install ] || [ \"$1\" = --uninstall ]\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "blitz-tray"), []byte(script), 0o755))
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	orig := trayDirs
	trayDirs = func() []string { return nil } // never a real tray
	t.Cleanup(func() { trayDirs = orig })
	a := &App{}
	require.NoError(t, a.SetTray(true))
	require.NoError(t, a.SetTray(false))
	got, _ := os.ReadFile(out)
	assert.Equal(t, "--install\n--uninstall\n", string(got))

	require.NoError(t, os.WriteFile(filepath.Join(bin, "blitz-tray"), []byte("#!/bin/sh\necho no display >&2\nexit 1\n"), 0o755))
	err := a.SetTray(true)
	assert.ErrorContains(t, err, "no display")

	t.Setenv("PATH", t.TempDir())
	assert.ErrorContains(t, a.SetTray(true), "isn't installed")
}

func TestRevealCommand(t *testing.T) {
	for _, tc := range []struct {
		goos, path string
		want       []string
	}{
		{"darwin", "/a/b c.go", []string{"open", "-R", "/a/b c.go"}},
		{"windows", `C:\a\b c.go`, []string{"explorer", "/select,", `C:\a\b c.go`}},
		{"linux", "/a/b c,d.go", []string{"dbus-send", "--session", "--print-reply", "--dest=org.freedesktop.FileManager1",
			"/org/freedesktop/FileManager1", "org.freedesktop.FileManager1.ShowItems", "array:string:file:///a/b%20c%2Cd.go", "string:"}},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			cmd := revealCommand(context.Background(), tc.goos, tc.path)
			assert.Equal(t, tc.want[0], filepath.Base(cmd.Path))
			assert.Equal(t, tc.want[1:], cmd.Args[1:])
		})
	}
}

func TestRevealPath(t *testing.T) {
	var revealed, opened []string
	revealErr := error(nil)
	origReveal, origOpen := reveal, openCommand
	reveal = func(p string) error {
		revealed = append(revealed, p)
		return revealErr
	}
	openCommand = func(dir string) *exec.Cmd {
		opened = append(opened, dir)
		return exec.Command("true")
	}
	t.Cleanup(func() { reveal, openCommand = origReveal, origOpen })

	dir := t.TempDir()
	file := filepath.Join(dir, "run.sh")
	require.NoError(t, os.WriteFile(file, []byte("#!/bin/sh\n"), 0o755))
	a := &App{}
	for _, tc := range []struct {
		name, path string
		fails      bool
		ok         bool
		revealed   []string
		opened     []string
	}{
		{"a file", file, false, true, []string{file}, nil},
		{"a folder", dir + "/", false, true, []string{dir}, nil},
		{"no file manager answers", file, true, true, []string{file}, []string{dir}},
		{"nothing there", filepath.Join(dir, "none"), false, false, nil, nil},
		{"relative", "run.sh", false, false, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			revealed, opened = nil, nil
			revealErr = nil
			if tc.fails {
				revealErr = errors.New("no file manager")
			}
			err := a.RevealPath(tc.path)
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
			assert.Equal(t, tc.revealed, revealed)
			assert.Equal(t, tc.opened, opened)
		})
	}
}

func TestFileManagerName(t *testing.T) {
	for _, tc := range []struct{ goos, desktop, want string }{
		{"darwin", "", "Finder"},
		{"windows", "", "File Explorer"},
		{"linux", "org.kde.dolphin.desktop\n", "Dolphin"},
		{"linux", "org.gnome.Nautilus.desktop", "Files"},
		{"linux", "nemo.desktop", "Nemo"},
		{"linux", "thunar.desktop", "Thunar"},
		{"linux", "something-else.desktop", ""},
		{"linux", "", ""},
	} {
		t.Run(tc.goos+" "+tc.desktop, func(t *testing.T) {
			assert.Equal(t, tc.want, fileManagerName(tc.goos, tc.desktop))
		})
	}
}
