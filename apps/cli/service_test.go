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
	"errors"
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

// fakeSystem records the system commands the login item runs, failing
// with err.
func fakeSystem(t *testing.T, err error) *[]string {
	t.Helper()
	var ran []string
	old := loginitem.RunSystem
	loginitem.RunSystem = func(name string, args ...string) error {
		ran = append(ran, name+" "+strings.Join(args, " "))
		return err
	}
	t.Cleanup(func() { loginitem.RunSystem = old })
	return &ran
}

// blitzdOnPath puts a blitzd script running body on PATH (alone).
func blitzdOnPath(t *testing.T, body string) {
	t.Helper()
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "blitzd"), []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	t.Setenv("PATH", bin)
}

// blitz service install, status and uninstall, as commands: keys only in
// the shell are named, and a failure to start it is an error.
func TestServiceCommands(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("login items are only supported on macOS and Linux")
	}
	isolate(t)
	t.Setenv("BLITZ_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	t.Setenv("GEMINI_API_KEY", "AIza-shell-only")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-shell-only")

	t.Setenv("PATH", t.TempDir())
	_, err := runCLI(t, "service", "install")
	assert.Equal(t, exitUsage, exitCodeFor(err), "no blitzd: %v", err)

	blitzdOnPath(t, "exit 0")
	fakeSystem(t, nil)
	out, err := runCLI(t, "service", "install")
	require.NoError(t, err, out)
	assert.Contains(t, out, "GEMINI_API_KEY, ANTHROPIC_API_KEY are only in your shell's environment")
	assert.Contains(t, out, "put them in")
	out, err = runCLI(t, "service", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "service:    not running")
	out, err = runCLI(t, "service", "uninstall")
	require.NoError(t, err)
	assert.Contains(t, out, "no longer starts at login")

	fakeSystem(t, errors.New("systemctl failed"))
	_, err = runCLI(t, "service", "install")
	assert.ErrorContains(t, err, "systemctl failed")
}

// A settings file that doesn't load names no keys.
func TestKeysOnlyInEnvironmentBrokenSettings(t *testing.T) {
	home := isolate(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte("[llm\n"), 0o600))
	t.Setenv("OPENAI_API_KEY", "sk-x")
	assert.Empty(t, keysOnlyInEnvironment())
	assert.Equal(t, "sk-x", os.Getenv("OPENAI_API_KEY"), "the environment wasn't restored")
}

// blitz serve runs blitzd with its arguments and exits as it does.
func TestServeRunsBlitzd(t *testing.T) {
	isolate(t)
	blitzdOnPath(t, `[ "$1" = "--ok" ] && exit 0; exit 3`)
	_, err := runCLI(t, "serve", "--ok")
	assert.NoError(t, err)
	_, err = runCLI(t, "serve")
	assert.Equal(t, 3, exitCodeFor(err), "%v", err)
	assert.ErrorContains(t, err, "blitzd")

	t.Setenv("PATH", t.TempDir())
	_, err = runCLI(t, "serve")
	assert.Equal(t, exitUsage, exitCodeFor(err), "%v", err)
}
