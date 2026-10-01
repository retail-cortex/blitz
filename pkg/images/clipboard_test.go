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

package images

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTool puts an executable shell script called name on PATH.
func fakeTool(t *testing.T, dir, name, script string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0o755))
}

// On Linux the clipboard is read with wl-paste, else xclip; a tool that
// fails or prints nothing is skipped, and none at all is no image.
func TestReadClipboardLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the Linux clipboard tools")
	}
	cases := map[string]struct {
		wlPaste, xclip string // scripts; "" means not installed
		want           string
	}{
		"wl-paste":             {wlPaste: "printf wl", xclip: "printf xc", want: "wl"},
		"wl-paste fails":       {wlPaste: "exit 1", xclip: "printf xc", want: "xc"},
		"wl-paste empty":       {wlPaste: "true", xclip: "printf xc", want: "xc"},
		"xclip only":           {xclip: "printf xc", want: "xc"},
		"neither has an image": {wlPaste: "exit 1", xclip: "exit 1"},
		"no tools":             {},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			bin := t.TempDir()
			if tc.wlPaste != "" {
				fakeTool(t, bin, "wl-paste", tc.wlPaste)
			}
			if tc.xclip != "" {
				fakeTool(t, bin, "xclip", tc.xclip)
			}
			t.Setenv("PATH", bin)
			got, err := readClipboard(context.Background())
			if tc.want == "" {
				assert.ErrorIs(t, err, ErrNoClipboardImage)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

// viaTempFile returns what the command wrote to the file it was given, and
// no image when the command fails or writes nothing.
func TestViaTempFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	cases := map[string]struct {
		script string
		want   string
	}{
		"written": {script: `printf png > "$1"`, want: "png"},
		"fails":   {script: `printf png > "$1"; exit 1`},
		"empty":   {script: `: > "$1"`},
		"nothing": {script: `true`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var path string
			got, err := viaTempFile(context.Background(), func(p string) *exec.Cmd {
				path = p
				return exec.Command("sh", "-c", tc.script, "sh", p)
			})
			if tc.want == "" {
				assert.ErrorIs(t, err, ErrNoClipboardImage)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, string(got))
			}
			assert.NoDirExists(t, filepath.Dir(path), "the temporary directory is removed")
		})
	}
}

// Without a temporary directory there's nothing to read the image into.
func TestViaTempFileNoTempDir(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	_, err := viaTempFile(context.Background(), func(string) *exec.Cmd { return exec.Command("true") })
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoClipboardImage)
}
