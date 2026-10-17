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

package wininstall

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// zipOf is a zip of files (name → content; a name ending in / a folder).
func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, body := range files {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return b.Bytes()
}

// The setup program carries the zip at its end; the uninstaller, without
// one, says so.
func TestPayload(t *testing.T) {
	z := zipOf(t, map[string]string{"Blitz/Blitz.exe": "app", "Blitz/resources/app/bin/blitzd.exe": "d"})
	var exe bytes.Buffer
	exe.WriteString("MZ the program")
	require.NoError(t, AppendPayload(&exe, z))
	zr, err := Payload(bytes.NewReader(exe.Bytes()), int64(exe.Len()))
	require.NoError(t, err)
	assert.Len(t, zr.File, 2)

	_, err = Payload(bytes.NewReader([]byte("MZ the program")), 14)
	assert.ErrorIs(t, err, ErrNoPayload)
	_, err = Payload(bytes.NewReader(nil), 0)
	assert.ErrorIs(t, err, ErrNoPayload)
	bad := append([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}, magic...)
	_, err = Payload(bytes.NewReader(bad), int64(len(bad)))
	assert.ErrorContains(t, err, "damaged")
}

// Unpacking drops the zip's folder and refuses a file that would land
// outside the app's.
func TestUnpack(t *testing.T) {
	dest := t.TempDir()
	z := zipOf(t, map[string]string{"Blitz/": "", "Blitz/Blitz.exe": "app", "Blitz/resources/app/package.json": "{}"})
	zr, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	require.NoError(t, err)
	require.NoError(t, Unpack(zr, dest, "Blitz"))
	data, err := os.ReadFile(filepath.Join(dest, "Blitz.exe"))
	require.NoError(t, err)
	assert.Equal(t, "app", string(data))
	assert.FileExists(t, filepath.Join(dest, "resources", "app", "package.json"))

	evil := zipOf(t, map[string]string{"Blitz/../../evil.txt": "x"})
	zr, err = zip.NewReader(bytes.NewReader(evil), int64(len(evil)))
	require.NoError(t, err)
	assert.ErrorContains(t, Unpack(zr, filepath.Join(t.TempDir(), "app"), "Blitz"), "outside")
}

// Placing the app puts it at root, an older one moved aside and removed.
func TestPlace(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "Programs", "Blitz")
	write := func(dir, body string) string {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, App), []byte(body), 0o644))
		return dir
	}
	require.NoError(t, Place(write(filepath.Join(base, "new1"), "v1"), root))
	require.NoError(t, Place(write(filepath.Join(base, "new2"), "v2"), root))
	data, err := os.ReadFile(filepath.Join(root, App))
	require.NoError(t, err)
	assert.Equal(t, "v2", string(data))
	left, err := filepath.Glob(root + ".old-*")
	require.NoError(t, err)
	assert.Empty(t, left, "the older app was removed")

	assert.Error(t, Place(filepath.Join(base, "missing"), root))
	data, err = os.ReadFile(filepath.Join(root, App))
	require.NoError(t, err)
	assert.Equal(t, "v2", string(data), "a failed place keeps the app there")
}

func TestEntryAndHelpers(t *testing.T) {
	root := filepath.Join("C:", "Users", "me", "AppData", "Local", "Programs", "Blitz")
	e := Entry{Root: root, Version: "0.7.1", SizeKB: 300_000}
	s := e.Strings()
	assert.Equal(t, "Blitz", s["DisplayName"])
	assert.Equal(t, "0.7.1", s["DisplayVersion"])
	assert.Equal(t, `"`+filepath.Join(root, Uninstaller)+`"`, s["UninstallString"])
	assert.Equal(t, s["UninstallString"]+" /S", s["QuietUninstallString"])
	assert.Equal(t, uint32(300_000), e.Numbers()["EstimatedSize"])
	assert.Equal(t, uint32(1), e.Numbers()["NoModify"])

	assert.Equal(t, filepath.Join("L", "Programs", "Blitz"), DefaultRoot("L"))
	assert.Equal(t, "0.7.1", TrayVersion("blitz-tray 0.7.1\n"))
	assert.Empty(t, TrayVersion("something else"))

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), make([]byte, 2048), 0o644))
	assert.Equal(t, int64(2), Size(dir))
	assert.NoError(t, SetDisplayVersion("0.7.1"), "nothing to do off Windows, or without an entry")
}
