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
	"bytes"
	"encoding/binary"
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

// The program and icon the zip's Blitz.exe is made from (BUILD.bazel).
var (
	exePath  = flag.String("exe", "", "Electron's Windows program")
	iconPath = flag.String("icon", "", "the app's icon")
)

// runfile is a path from $(rootpath), which is relative to the main
// repository's runfiles.
func runfile(t *testing.T, p string) string {
	t.Helper()
	if p == "" {
		t.Skip("run by bazel test, which passes the program and the icon")
	}
	return filepath.Join(os.Getenv("RUNFILES_DIR"), "_main", p)
}

// Electron's program comes out with Blitz's icon, at every size, and
// details naming Blitz, unsigned; its other resources stay.
func TestRewrite(t *testing.T) {
	exe := runfile(t, *exePath)
	out := filepath.Join(t.TempDir(), "Blitz.exe")
	require.NoError(t, run(exe, runfile(t, *iconPath), "0.6.6", out))

	before := load(t, exe)
	after := load(t, out)
	icon, err := after.GetIcon(winres.ID(1))
	require.NoError(t, err)
	var ico bytes.Buffer
	require.NoError(t, icon.SaveICO(&ico))
	count := int(binary.LittleEndian.Uint16(ico.Bytes()[4:6])) // ICONDIR's image count
	assert.Equal(t, len(iconSizes), count, "an image a size")

	data := after.Get(winres.RT_VERSION, winres.ID(1), 0)
	require.NotNil(t, data, "version details")
	v, err := version.FromBytes(data)
	require.NoError(t, err)
	table := v.Table().GetMainTranslation()
	assert.Equal(t, "Blitz", table[version.ProductName])
	assert.Equal(t, "Blitz", table[version.FileDescription])
	assert.Equal(t, "Blitz.exe", table[version.OriginalFilename])
	assert.Equal(t, "0.6.6", table[version.ProductVersion])

	manifest := winres.RT_MANIFEST
	assert.Equal(t, before.Get(manifest, winres.ID(1), 0x409) != nil, after.Get(manifest, winres.ID(1), 0x409) != nil, "the manifest stays as it was")
}

// load reads a program's resources.
func load(t *testing.T, path string) *winres.ResourceSet {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	rs, err := winres.LoadFromEXE(f)
	require.NoError(t, err)
	return rs
}

// What it needs, and an icon it can use, or it says why not.
func TestRewriteRefuses(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, w, h int) string {
		var b bytes.Buffer
		require.NoError(t, png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))))
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, b.Bytes(), 0o600))
		return p
	}
	notExe := filepath.Join(dir, "not.exe")
	require.NoError(t, os.WriteFile(notExe, []byte("MZ but not really"), 0o600))
	for _, tc := range []struct {
		name, in, icon, out, want string
	}{
		{"nothing to do", "", "", "", "are needed"},
		{"no program", filepath.Join(dir, "missing.exe"), write("ok.png", 256, 256), filepath.Join(dir, "o.exe"), "no such file"},
		{"no icon", notExe, filepath.Join(dir, "missing.png"), filepath.Join(dir, "o.exe"), "no such file"},
		{"not a program", notExe, write("ok2.png", 256, 256), filepath.Join(dir, "o.exe"), "reading the program's resources"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorContains(t, run(tc.in, tc.icon, "1.0.0", tc.out), tc.want)
		})
	}
	if *exePath != "" {
		exe := runfile(t, *exePath)
		assert.ErrorContains(t, run(exe, write("small.png", 64, 64), "1.0.0", filepath.Join(dir, "o.exe")), "at least 256 px")
		assert.ErrorContains(t, run(exe, write("wide.png", 512, 256), "1.0.0", filepath.Join(dir, "o.exe")), "must be square")
		bad := filepath.Join(dir, "bad.png")
		require.NoError(t, os.WriteFile(bad, []byte("not a png"), 0o600))
		assert.ErrorContains(t, run(exe, bad, "1.0.0", filepath.Join(dir, "o.exe")), "reading the icon")
	}
}

// The command: its flags, what went wrong on stderr, and its status.
func TestCmd(t *testing.T) {
	var stderr bytes.Buffer
	assert.Equal(t, 2, cmd([]string{"--nonsense"}, &stderr))
	assert.Contains(t, stderr.String(), "flag provided but not defined")
	stderr.Reset()
	assert.Equal(t, 1, cmd(nil, &stderr))
	assert.Contains(t, stderr.String(), "winexe: --in, --icon and --out are needed")
	if *exePath != "" {
		out := filepath.Join(t.TempDir(), "Blitz.exe")
		assert.Equal(t, 0, cmd([]string{"--in", runfile(t, *exePath), "--icon", runfile(t, *iconPath), "--version", "1.2.3", "--out", out}, &stderr))
		_, err := os.Stat(out)
		assert.NoError(t, err)
	}
}
