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
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
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
