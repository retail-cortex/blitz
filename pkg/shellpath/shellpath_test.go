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
	"strings"
	"testing"
	"time"

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

// A startup file that leaves a program running with the shell's output
// open, or a shell that never ends, doesn't hold up the start: past the
// timeout, this process's PATH (N26 pkg-small-2#12).
func TestUserPathDoesntHang(t *testing.T) {
	timeout, waitDelay = 200*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { timeout, waitDelay = 5*time.Second, time.Second })
	dir := t.TempDir()
	for name, script := range map[string]string{
		"a program left running": "#!/bin/sh\n/bin/sleep 5 &\n/bin/sleep 5\n",
		"a shell reading input":  "#!/bin/sh\nread line\n/bin/sleep 5\n",
	} {
		t.Run(name, func(t *testing.T) {
			shell := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"))
			require.NoError(t, os.WriteFile(shell, []byte(script), 0o755))
			t.Setenv("PATH", "/app/bin")
			start := time.Now()
			assert.Equal(t, []string{"/app/bin"}, UserPath(shell))
			assert.Less(t, time.Since(start), 2*time.Second)
		})
	}
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

// The terminal's folders first, in its order; this process's after; each
// once.
func TestMerge(t *testing.T) {
	for _, tc := range []struct {
		name            string
		user, own, want []string
	}{
		{"the terminal's first", []string{"/nvm/bin", "/usr/bin"}, []string{"/usr/bin", "/bin"}, []string{"/nvm/bin", "/usr/bin", "/bin"}},
		{"no terminal", nil, []string{"/usr/bin", "/usr/bin", ""}, []string{"/usr/bin"}},
		{"nothing", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Merge(tc.user, tc.own))
		})
	}
}

// A service started without a terminal takes the login shell's PATH.
func TestAdoptUserPath(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "sh")
	require.NoError(t, os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'BLITZ_PATH=/nvm/bin:/usr/bin\\n'\n"), 0o755))
	t.Setenv("SHELL", shell)
	t.Setenv("PATH", "/usr/bin:/bin")
	assert.Equal(t, 1, AdoptUserPath())
	assert.Equal(t, "/nvm/bin:/usr/bin:/bin", os.Getenv("PATH"))
	assert.Equal(t, 0, AdoptUserPath(), "already there")
}
