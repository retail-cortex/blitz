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
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/wininstall"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSystem is a Windows in temporary folders: what's asked is recorded.
type fakeSystem struct {
	self, local, temp, menu, desk string
	answer                        bool
	stopErr                       error
	ran, told                     []string
	entry                         *wininstall.Entry
	deleted, runRemoved           bool
	pathRemoved, deleteLater      string
}

func (f *fakeSystem) Self() (string, error) { return f.self, nil }
func (f *fakeSystem) LocalAppData() string  { return f.local }
func (f *fakeSystem) TempDir() string       { return f.temp }
func (f *fakeSystem) StopApp(root string) error {
	f.ran = append(f.ran, "stop "+filepath.Base(root))
	return f.stopErr
}
func (f *fakeSystem) StartMenu() string { return f.menu }
func (f *fakeSystem) Desktop() string   { return f.desk }
func (f *fakeSystem) Shortcut(link, target string) error {
	return os.WriteFile(link, []byte(target), 0o644)
}
func (f *fakeSystem) WriteEntry(e wininstall.Entry) error { f.entry = &e; return nil }
func (f *fakeSystem) DeleteEntry() error                  { f.deleted = true; return nil }
func (f *fakeSystem) RemoveRunValue() error               { f.runRemoved = true; return nil }
func (f *fakeSystem) RemoveFromPath(dir string) error     { f.pathRemoved = dir; return nil }
func (f *fakeSystem) Start(exe string, args ...string) error {
	f.ran = append(f.ran, strings.TrimSpace("start "+filepath.Base(exe)+" "+strings.Join(args, " ")))
	return nil
}
func (f *fakeSystem) Ask(string) bool          { return f.answer }
func (f *fakeSystem) Tell(text string, _ bool) { f.told = append(f.told, text) }
func (f *fakeSystem) DeleteLater(path string)  { f.deleteLater = path }

// newFake is a Windows with the setup program (carrying an app) at f.self.
func newFake(t *testing.T) *fakeSystem {
	t.Helper()
	base := t.TempDir()
	f := &fakeSystem{local: filepath.Join(base, "Local"), temp: filepath.Join(base, "Temp"), menu: filepath.Join(base, "Menu"), desk: filepath.Join(base, "Desktop"), answer: true}
	for _, d := range []string{f.local, f.temp, f.menu, f.desk} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	var z bytes.Buffer
	w := zip.NewWriter(&z)
	for name, body := range map[string]string{"Blitz/Blitz.exe": "app", "Blitz/" + wininstall.Uninstaller: "uninstaller", "Blitz/resources/app/bin/blitzd.exe": "d"} {
		fw, err := w.Create(name)
		require.NoError(t, err)
		_, _ = fw.Write([]byte(body))
	}
	require.NoError(t, w.Close())
	var exe bytes.Buffer
	exe.WriteString("MZ setup")
	require.NoError(t, wininstall.AppendPayload(&exe, z.Bytes()))
	f.self = filepath.Join(base, "Blitz_setup.exe")
	require.NoError(t, os.WriteFile(f.self, exe.Bytes(), 0o755))
	return f
}

func TestParse(t *testing.T) {
	assert.Equal(t, options{silent: true, uninstall: true, root: `C:\x`}, parse([]string{"/S", "--uninstall", "--root", `C:\x`}))
	assert.Equal(t, options{}, parse([]string{"--root"}))
}

// Installing puts the app in Programs\Blitz with its shortcuts and entry,
// and starts it; again, it replaces it.
func TestInstall(t *testing.T) {
	f := newFake(t)
	require.Equal(t, 0, run(nil, f))
	root := wininstall.DefaultRoot(f.local)
	data, err := os.ReadFile(filepath.Join(root, "Blitz.exe"))
	require.NoError(t, err)
	assert.Equal(t, "app", string(data))
	assert.FileExists(t, filepath.Join(root, wininstall.Uninstaller), "the uninstaller comes with the app")
	link, err := os.ReadFile(filepath.Join(f.menu, "Blitz.lnk"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "Blitz.exe"), string(link))
	assert.FileExists(t, filepath.Join(f.desk, "Blitz.lnk"))
	require.NotNil(t, f.entry)
	assert.Equal(t, root, f.entry.Root)
	assert.Equal(t, []string{"stop Blitz", "start Blitz.exe"}, f.ran)

	f.ran = nil
	require.Equal(t, 0, run([]string{"/S"}, f), "again, silently")
	assert.Equal(t, []string{"stop Blitz"}, f.ran, "not started when silent")
	assert.NoDirExists(t, root+".new")
}

// Saying no installs nothing; a failure is told.
func TestInstallDeclinedOrFailing(t *testing.T) {
	f := newFake(t)
	f.answer = false
	assert.Equal(t, 1, run(nil, f))
	assert.NoDirExists(t, wininstall.DefaultRoot(f.local))

	f.answer, f.stopErr = true, errors.New("access denied")
	assert.Equal(t, 1, run(nil, f))
	require.Len(t, f.told, 1)
	assert.Contains(t, f.told[0], "access denied")
}

// The uninstaller, run from the app's folder, copies itself to %TEMP% and
// runs from there; that copy removes the app, its shortcuts, its entry,
// its start at login and its PATH folder, and deletes itself later.
func TestUninstall(t *testing.T) {
	f := newFake(t)
	require.Equal(t, 0, run([]string{"/S"}, f))
	root := wininstall.DefaultRoot(f.local)

	f.self, f.ran = filepath.Join(root, wininstall.Uninstaller), nil
	require.Equal(t, 0, run(nil, f))
	require.Len(t, f.ran, 1)
	assert.Regexp(t, `^start blitz-uninstall-\d+\.exe --uninstall --root `, f.ran[0])
	assert.DirExists(t, root, "the copy removes it")
	copies, _ := filepath.Glob(filepath.Join(f.temp, "blitz-uninstall-*.exe"))
	require.Len(t, copies, 1)

	f.self, f.ran = copies[0], nil
	retryEvery = 0
	require.Equal(t, 0, run([]string{"--uninstall", "--root", root}, f))
	assert.NoDirExists(t, root)
	assert.NoFileExists(t, filepath.Join(f.menu, "Blitz.lnk"))
	assert.NoFileExists(t, filepath.Join(f.desk, "Blitz.lnk"))
	assert.True(t, f.deleted)
	assert.True(t, f.runRemoved)
	assert.Equal(t, filepath.Join(root, "resources", "app", "bin"), f.pathRemoved)
	assert.Equal(t, copies[0], f.deleteLater)
	assert.Equal(t, []string{"Blitz is uninstalled."}, f.told)

	f.answer = false
	assert.Equal(t, 1, run([]string{"--uninstall", "--root", root}, f), "declined")
}

func TestWithin(t *testing.T) {
	assert.True(t, within(filepath.Join("a", "b", "c"), filepath.Join("a", "b")))
	assert.False(t, within(filepath.Join("a", "bc"), filepath.Join("a", "b")))
	assert.False(t, within(filepath.Join("a", "b"), filepath.Join("a", "b")))
}
