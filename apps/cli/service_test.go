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
	"encoding/xml"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceInstallAndUninstall(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("login items are only supported on macOS and Linux")
	}
	isolate(t)
	// The login item runs blitzd, found on PATH here.
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "blitzd"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", bin)
	var ran []string
	old := loginitem.RunSystem
	loginitem.RunSystem = func(name string, args ...string) error {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { loginitem.RunSystem = old })

	var out bytes.Buffer
	require.NoError(t, serviceInstall(&out))
	path, _ := loginitem.Path()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "login item not written")
	unit := string(data)
	assert.Contains(t, unit, filepath.Join(bin, "blitzd"), "unit doesn't run blitzd:\n%s", unit)
	switch goruntime.GOOS {
	case "darwin":
		dec := xml.NewDecoder(strings.NewReader(unit))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("plist isn't well-formed: %v", err)
			}
		}
		assert.Len(t, ran, 2, "ran %q", ran)
		assert.True(t, strings.HasPrefix(ran[1], "launchctl bootstrap gui/"), "ran %q", ran)
	case "linux":
		assert.Len(t, ran, 3, "ran %q", ran)
		assert.Equal(t, "systemctl --user enable blitz.service", ran[1], "ran %q", ran)
		assert.Equal(t, "systemctl --user restart blitz.service", ran[2], "ran %q", ran)
	}
	out.Reset()
	serviceStatus(&out)
	assert.Contains(t, out.String(), "login item: installed", "status:\n%s", out.String())

	ran = nil
	require.NoError(t, serviceUninstall(&out))
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, fs.ErrNotExist, "login item left behind")
	assert.NotEqual(t, 0, len(ran), "the service wasn't stopped")
}

// A key only exported in the shell won't reach a login item.
func TestKeysOnlyInEnvironment(t *testing.T) {
	isolate(t)
	got := keysOnlyInEnvironment()
	assert.Len(t, got, 0, "no keys: %v", got)
	t.Setenv("OPENAI_API_KEY", "sk-test-only-in-shell")
	got = keysOnlyInEnvironment()
	assert.Len(t, got, 1, "shell-only key: %v", got)
	assert.Equal(t, "OPENAI_API_KEY", got[0], "shell-only key: %v", got)
	assert.Equal(t, "sk-test-only-in-shell", os.Getenv("OPENAI_API_KEY"), "the environment wasn't restored")
}
