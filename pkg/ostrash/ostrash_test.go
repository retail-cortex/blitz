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

package ostrash

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
}

// The freedesktop.org trash: the file under files/, its record under
// info/ with where it was and when, a second of the same name numbered,
// and Restore putting each back and its record away (FIL-37a).
func TestFreedesktopTrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no freedesktop trash on Windows")
	}
	base := t.TempDir()
	trash := filepath.Join(base, "Trash")
	ws := filepath.Join(base, "my work")
	at := time.Date(2026, 11, 12, 9, 30, 5, 0, time.Local)

	write(t, filepath.Join(ws, "notes.md"), "one")
	first, err := freedesktopTrash(filepath.Join(ws, "notes.md"), trash, at)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(trash, "files", "notes.md"), first)
	rec, err := os.ReadFile(filepath.Join(trash, "info", "notes.md.trashinfo"))
	require.NoError(t, err)
	assert.Equal(t, "[Trash Info]\nPath="+escapePath(filepath.Join(ws, "notes.md"))+"\nDeletionDate=2026-11-12T09:30:05\n", string(rec))
	assert.Contains(t, string(rec), "my%20work/notes.md", "escaped")
	assert.NoFileExists(t, filepath.Join(ws, "notes.md"))

	write(t, filepath.Join(ws, "notes.md"), "two")
	second, err := freedesktopTrash(filepath.Join(ws, "notes.md"), trash, at)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(trash, "files", "notes.2.md"), second, "numbered, the extension kept")

	write(t, filepath.Join(ws, "src", "a.go"), "package a")
	folder, err := freedesktopTrash(filepath.Join(ws, "src"), trash, at)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(folder, "a.go"), "a folder goes whole")

	require.NoError(t, Restore(second, filepath.Join(ws, "notes.md")))
	data, err := os.ReadFile(filepath.Join(ws, "notes.md"))
	require.NoError(t, err)
	assert.Equal(t, "two", string(data))
	assert.NoFileExists(t, filepath.Join(trash, "info", "notes.2.md.trashinfo"), "its record goes")
	assert.Error(t, Restore(first, filepath.Join(ws, "notes.md")), "never over a file that's there")
	assert.FileExists(t, first)
}

// Trash finds the home trash from XDG_DATA_HOME on Linux and the BSDs.
func TestTrashUsesTheHomeTrash(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("the freedesktop trash is Linux's and the BSDs'")
	}
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("BLITZ_TEST_SYSTEM_TRASH", "1")
	assert.True(t, Available())
	f := filepath.Join(data, "w", "x.txt")
	write(t, f, "x")
	where, err := Trash(context.Background(), f)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(data, "Trash", "files", "x.txt"), where)
}

// On macOS the system moves it and says where (here a stand-in for
// osascript): its answer is the place, its error the reason.
func TestMacTrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for osascript")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "osascript")
	old := osascript
	t.Cleanup(func() { osascript = old })
	osascript = fake

	write(t, fake, "#!/bin/sh\necho \"$HOME/.Trash/$(basename \"$5\")\"\n")
	require.NoError(t, os.Chmod(fake, 0o755))
	where, err := macTrash(context.Background(), "/w/notes.md")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(os.Getenv("HOME"), ".Trash", "notes.md"), where)

	write(t, fake, "#!/bin/sh\necho 'execution error: Error: Error: “notes.md” couldn’t be moved to the trash. (-2700)' >&2\nexit 1\n")
	_, err = macTrash(context.Background(), "/w/notes.md")
	assert.EqualError(t, err, "moving notes.md to the Trash: “notes.md” couldn’t be moved to the trash.")

	write(t, fake, "#!/bin/sh\nexit 0\n")
	_, err = macTrash(context.Background(), "/w/notes.md")
	assert.ErrorContains(t, err, "didn't say where it went")
}

// The real macOS Trash, round trip: only when asked
// (BLITZ_TEST_SYSTEM_TRASH=1), since it uses the person's Trash; what it
// puts there it takes back.
func TestMacTrashForReal(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("BLITZ_TEST_SYSTEM_TRASH") != "1" {
		t.Skip("set BLITZ_TEST_SYSTEM_TRASH=1 on a Mac")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "blitz-ostrash-test.txt")
	write(t, f, "hello")
	where, err := Trash(context.Background(), f)
	require.NoError(t, err)
	t.Logf("trashed to %s", where)
	assert.NoFileExists(t, f)
	assert.FileExists(t, where)
	require.NoError(t, Restore(where, f))
	data, err := os.ReadFile(f)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestTrashMissing(t *testing.T) {
	_, err := Trash(context.Background(), filepath.Join(t.TempDir(), "nothing"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// A test gets no system trash unless it asks (BLITZ_TEST_SYSTEM_TRASH=1),
// so none fills the person's; Fake stands one in.
func TestTrashInTests(t *testing.T) {
	t.Setenv("BLITZ_TEST_SYSTEM_TRASH", "")
	assert.False(t, Available())
	f := filepath.Join(t.TempDir(), "a.txt")
	write(t, f, "a")
	_, err := Trash(context.Background(), f)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.FileExists(t, f, "left alone")

	dir := t.TempDir()
	undo := Fake(dir)
	assert.True(t, Available())
	where, err := Trash(context.Background(), f)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "1", "a.txt"), where)
	require.NoError(t, Restore(where, f))
	assert.FileExists(t, f)
	undo()
	assert.False(t, Available())
}
