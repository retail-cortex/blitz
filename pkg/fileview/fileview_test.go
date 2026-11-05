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

package fileview

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
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
			err := OpenFolder(tc.path)
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "chart.svg"), []byte("<svg/>"), 0o644))
	// Links named as pictures: to a script, to a folder, to a PDF.
	require.NoError(t, os.Symlink(filepath.Join(dir, "run.sh"), filepath.Join(dir, "chart.png")))
	require.NoError(t, os.Symlink(filepath.Join(dir, "dir.pdf"), filepath.Join(dir, "folder.png")))
	require.NoError(t, os.Symlink("spec.PDF", filepath.Join(dir, "paper.png")))
	real, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	for _, tc := range []struct {
		name  string
		opens string // what opens; "" refused
	}{
		{"spec.PDF", "spec.PDF"},
		{"run.sh", ""},
		{"evil.desktop", ""},
		{"dir.pdf", ""},
		{"none.pdf", ""},
		{"chart.svg", ""},
		{"chart.png", ""},
		{"folder.png", ""},
		{"paper.png", "spec.PDF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opened = nil
			p := filepath.Join(dir, tc.name)
			err := OpenDocument(p)
			if tc.opens != "" {
				assert.NoError(t, err)
				assert.Equal(t, []string{filepath.Join(real, tc.opens)}, opened, "the link's target opens")
			} else {
				assert.Error(t, err)
				assert.Empty(t, opened)
			}
		})
	}
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
			err := RevealPath(tc.path)
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

// OpenFolder and OpenDocument report a program that won't start.
func TestOpenCommandFails(t *testing.T) {
	orig := openCommand
	t.Cleanup(func() { openCommand = orig })
	assert.Equal(t, filepath.Base(orig("/x").Path), map[bool]string{true: "open", false: "xdg-open"}[goruntime.GOOS == "darwin"])
	openCommand = func(string) *exec.Cmd { return exec.Command(filepath.Join(t.TempDir(), "missing")) }
	assert.ErrorContains(t, OpenFolder(t.TempDir()), "opening")
	doc := filepath.Join(t.TempDir(), "a.png")
	require.NoError(t, os.WriteFile(doc, nil, 0o600))
	assert.ErrorContains(t, OpenDocument(doc), "opening")
}

// The file manager's name: macOS's and Windows's are known; Linux's comes
// from the desktop file that opens folders.
func TestFileManager(t *testing.T) {
	orig := folderHandler
	folderHandler = func() string { return "org.kde.dolphin.desktop\n" }
	t.Cleanup(func() { folderHandler = orig })
	want := map[string]string{"darwin": "Finder", "windows": "File Explorer", "linux": "Dolphin"}[goruntime.GOOS]
	assert.Equal(t, want, FileManager())
}

// A refusal is in the person's language.
func TestRefusalsTranslated(t *testing.T) {
	t.Cleanup(func() { i18n.SetCurrent(nil) })
	i18n.SetCurrent(i18n.Default().Localizer(language.Spanish))
	assert.ErrorContains(t, OpenDocument("/tmp/run.sh"), "solo se abren imágenes")
	assert.ErrorContains(t, RevealPath("relative"), "no es una ruta absoluta")
}
