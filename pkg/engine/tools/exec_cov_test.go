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

package tools

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failCredentialsDir makes the copies' directory unavailable.
func failCredentialsDir(t *testing.T) {
	t.Helper()
	old := credentialsDir
	credentialsDir = func() (string, error) { return "", errors.New("no cache directory") }
	t.Cleanup(func() { credentialsDir = old })
}

// signedIn is a credential whose file exists.
func signedIn(t *testing.T, env string) Credential {
	t.Helper()
	src := filepath.Join(t.TempDir(), "adc.json")
	require.NoError(t, os.WriteFile(src, []byte(`{}`), 0o600))
	return Credential{Env: env, Path: func() string { return src }}
}

// The copies go in the user's cache directory by default; without one,
// there's nowhere for them.
func TestCredentialsDirDefault(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/cache") // Linux's; macOS has ~/Library/Caches
	cache, err := os.UserCacheDir()
	require.NoError(t, err)
	dir, err := credentialsDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cache, "blitz", "credentials"), dir)

	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	_, err = credentialsDir()
	assert.Error(t, err, "no cache directory without a home")
}

// A command whose credentials can't be copied doesn't start, and says why;
// nothing is left behind.
func TestCredentialCopyFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	readOnly := filepath.Join(t.TempDir(), "ro")
	require.NoError(t, os.Mkdir(readOnly, 0o500))
	tests := []struct {
		name     string
		dir      func(t *testing.T)
		env      string
		needPerm bool
	}{
		{"no cache directory", failCredentialsDir, "GOOGLE_APPLICATION_CREDENTIALS", false},
		{"the directory can't be made", func(t *testing.T) { setCredentialsDir(t, filepath.Join(file, "sub")) }, "GOOGLE_APPLICATION_CREDENTIALS", false},
		{"no room for a run's directory", func(t *testing.T) { setCredentialsDir(t, readOnly) }, "GOOGLE_APPLICATION_CREDENTIALS", true},
		{"the copy can't be written", func(t *testing.T) { setCredentialsDir(t, t.TempDir()) }, "NESTED/NAME", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.needPerm && os.Geteuid() == 0 {
				t.Skip("root ignores directory permissions")
			}
			tt.dir(t)
			e := &ExecEnv{Credentials: []Credential{signedIn(t, tt.env)}}
			_, err := e.command(context.Background(), []string{"true"})
			assert.ErrorContains(t, err, "copying the sign-in's credentials")
		})
	}
}

// setCredentialsDir points the copies at dir.
func setCredentialsDir(t *testing.T, dir string) {
	t.Helper()
	old := credentialsDir
	credentialsDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { credentialsDir = old })
}

// Without ScrubEnv, a command with a credential gets the whole environment
// plus the copy's variable.
func TestCredentialWithoutScrub(t *testing.T) {
	setCredentialsDir(t, filepath.Join(t.TempDir(), "creds"))
	t.Setenv("BLITZ_KEPT", "kept")
	e := &ExecEnv{Credentials: []Credential{signedIn(t, "MY_CRED")}}
	cmd, err := e.command(context.Background(), []string{"sh", "-c", `echo "$BLITZ_KEPT"; [ -f "$MY_CRED" ] && echo copied`})
	require.NoError(t, err)
	var out bytes.Buffer
	cmd.Stdout = &out
	require.NoError(t, cmd.Run())
	assert.Equal(t, "kept\ncopied\n", out.String())
}

// Sweeping without a cache directory does nothing.
func TestSweepCredentialsWithoutDir(t *testing.T) {
	failCredentialsDir(t)
	assert.NotPanics(t, sweepCredentials)
}

// A command that can't start reports it from Run and releases its guard.
func TestGuardedCmdStartFailure(t *testing.T) {
	e := &ExecEnv{Dir: filepath.Join(t.TempDir(), "missing")}
	cmd, err := e.command(context.Background(), []string{"true"})
	require.NoError(t, err)
	err = cmd.Run()
	assert.Error(t, err, "the directory it should run in is missing")
	cmd.Release() // safe again
}
