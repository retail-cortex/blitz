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

package shellpath

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PATH as the login shell prints it, else this process's.
func TestUserPath(t *testing.T) {
	dir := t.TempDir()
	shell := filepath.Join(dir, "sh")
	require.NoError(t, os.WriteFile(shell, []byte("#!/bin/sh\necho welcome\nprintf 'BLITZ_PATH=/a:/b\\n'\necho bye\n"), 0o755))
	t.Setenv("PATH", "/app/bin")
	assert.Equal(t, []string{"/a", "/b"}, UserPath(shell))
	assert.Equal(t, []string{"/app/bin"}, UserPath(filepath.Join(dir, "missing")))
}

// A program on this process's PATH, else on the login shell's; a file
// that isn't executable doesn't count.
func TestFind(t *testing.T) {
	app, terminal := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(app, "here"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(terminal, "there"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(terminal, "notes"), nil, 0o644))
	shell := filepath.Join(t.TempDir(), "sh")
	require.NoError(t, os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'BLITZ_PATH=%s\\n' "+terminal+"\n"), 0o755))
	t.Setenv("PATH", app)
	t.Setenv("SHELL", shell)
	assert.Equal(t, filepath.Join(app, "here"), Find("here"))
	assert.Equal(t, filepath.Join(terminal, "there"), Find("there"))
	assert.Empty(t, Find("notes"))
	assert.Empty(t, Find("missing"))
	assert.Empty(t, LookPathIn([]string{"", terminal}, "notes"))

	t.Setenv("SHELL", "")
	assert.NotEmpty(t, LoginShell())
}
