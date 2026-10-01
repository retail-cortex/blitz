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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useCredentialsDir points the copies at a test directory.
func useCredentialsDir(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "credentials")
	old := credentialsDir
	credentialsDir = func() (string, error) { return root, nil }
	t.Cleanup(func() { credentialsDir = old })
	return root
}

// A command gets its own copy of the sign-in's credentials, named by the
// variable ScrubEnv would otherwise withhold, and the copy goes when the
// command ends; without the file, the command runs without it.
func TestCredentialCopies(t *testing.T) {
	src := filepath.Join(t.TempDir(), "adc.json")
	require.NoError(t, os.WriteFile(src, []byte(`{"type":"authorized_user"}`), 0o600))
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/the/service/own.json")
	tests := []struct {
		name string
		path string
		want string
	}{
		{"signed in", src, `{"type":"authorized_user"}` + "\n.json\n"},
		{"not signed in", "", "\n\n"},
		{"file gone", filepath.Join(t.TempDir(), "missing.json"), "\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := useCredentialsDir(t)
			e := &ExecEnv{
				Dir:         t.TempDir(),
				ScrubEnv:    []string{"GOOGLE_APPLICATION_CREDENTIALS"},
				Credentials: []Credential{{Env: "GOOGLE_APPLICATION_CREDENTIALS", Path: func() string { return tt.path }}},
			}
			cmd, err := e.command(context.Background(), []string{"sh", "-c", `f="$GOOGLE_APPLICATION_CREDENTIALS"; [ -n "$f" ] && cat "$f"; echo; [ -n "$f" ] && echo ".${f##*.}" || echo`})
			require.NoError(t, err)
			var out bytes.Buffer
			cmd.Stdout = &out
			require.NoError(t, cmd.Run())
			assert.Equal(t, tt.want, out.String())
			left, _ := os.ReadDir(root)
			assert.Empty(t, left, "the copy outlived the command")
		})
	}
}

// Copies left by a crash go after a day; newer ones, still in use, stay.
func TestSweepCredentials(t *testing.T) {
	root := useCredentialsDir(t)
	for name, age := range map[string]time.Duration{"run-old": 25 * time.Hour, "run-new": time.Hour} {
		dir := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(dir, 0o700))
		then := time.Now().Add(-age)
		require.NoError(t, os.Chtimes(dir, then, then))
	}
	sweepCredentials()
	assert.NoDirExists(t, filepath.Join(root, "run-old"))
	assert.DirExists(t, filepath.Join(root, "run-new"))
}

// A link where the copies' directory should be (a command could make one
// where it can write) stops the command, and nothing is copied through it;
// so does a directory others can enter.
func TestCredentialsDirMustBePrivate(t *testing.T) {
	src := filepath.Join(t.TempDir(), "adc.json")
	require.NoError(t, os.WriteFile(src, []byte("{}"), 0o600))
	target := t.TempDir()
	open := filepath.Join(t.TempDir(), "open")
	require.NoError(t, os.Mkdir(open, 0o755))
	require.NoError(t, os.Chmod(open, 0o755)) // past the umask
	link := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.Symlink(target, link))
	for name, dir := range map[string]string{"a link": link, "open to others": open} {
		t.Run(name, func(t *testing.T) {
			setCredentialsDir(t, dir)
			e := &ExecEnv{Credentials: []Credential{{Env: "GOOGLE_APPLICATION_CREDENTIALS", Path: func() string { return src }}}}
			cmd, err := e.command(context.Background(), []string{"true"})
			assert.Nil(t, cmd)
			assert.ErrorContains(t, err, "not copying credentials into it")
			left, _ := os.ReadDir(target)
			assert.Empty(t, left, "copied through the link")
		})
	}
}

// The sweep removes only its own old run-* directories, and nothing behind
// a link put where its directory should be.
func TestSweepCredentialsStaysInItsDirectory(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	victim := t.TempDir()
	keep := filepath.Join(victim, "Documents")
	require.NoError(t, os.Mkdir(keep, 0o755))
	require.NoError(t, os.Chtimes(keep, old, old))
	link := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.Symlink(victim, link))
	setCredentialsDir(t, link)
	sweepCredentials()
	assert.DirExists(t, keep, "the sweep followed a link")

	root := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.Mkdir(root, 0o700))
	for _, name := range []string{"run-old", "notes", "run-file"} {
		p := filepath.Join(root, name)
		if name == "run-file" {
			require.NoError(t, os.WriteFile(p, nil, 0o600))
		} else {
			require.NoError(t, os.Mkdir(p, 0o700))
		}
		require.NoError(t, os.Chtimes(p, old, old))
	}
	require.NoError(t, os.Symlink(victim, filepath.Join(root, "run-link")))
	setCredentialsDir(t, root)
	sweepCredentials()
	assert.NoDirExists(t, filepath.Join(root, "run-old"))
	assert.DirExists(t, filepath.Join(root, "notes"), "not one of its own")
	assert.FileExists(t, filepath.Join(root, "run-file"), "not a run's directory")
	assert.DirExists(t, keep)
}
