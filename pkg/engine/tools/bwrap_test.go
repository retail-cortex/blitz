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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBwrapArgs(t *testing.T) {
	ws := t.TempDir()
	ro := filepath.Join(ws, "vendor-ro")
	os.Mkdir(ro, 0o755)
	spec := OSSandboxSpec{WritableDirs: []string{ws, "/definitely/missing"}, ReadOnlyDirs: []string{ro}}

	args := bwrapArgs("bwrap", spec, []string{ws + "/.env"}, []string{ws + "/secrets"})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"bwrap --die-with-parent --ro-bind / / --dev-bind /dev /dev",
		"--bind " + ws + " " + ws,
		"--ro-bind " + ro + " " + ro,
		"--ro-bind /dev/null " + ws + "/.env",
		"--tmpfs " + ws + "/secrets --remount-ro " + ws + "/secrets",
		"--unshare-net",
	} {
		assert.Contains(t, joined, want, "args missing %q:\n%s", want, joined)
	}
	assert.NotContains(t, joined, "/definitely/missing", "missing writable dir should be skipped (bwrap fails on it)")
	assert.Equal(t, "--", args[len(args)-1], "args must end with --")
	// Order: read-only and masks come after writable binds so they win.
	assert.LessOrEqual(t, slices.Index(args, "--bind"), slices.Index(args, ro), "read-only bind must follow writable binds")
	spec.AllowNetwork = true
	assert.NotContains(t, strings.Join(bwrapArgs("bwrap", spec, nil, nil), " "), "--unshare-net", "network should be shared when allowed")
}

func TestExpandBlocked(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".env"), "x")
	writeFile(t, filepath.Join(ws, "deep", "a", "server.pem"), "x")
	writeFile(t, filepath.Join(ws, "node_modules", "pkg", ".env"), "x") // skipped dir
	writeFile(t, filepath.Join(ws, "ok.txt"), "x")
	writeFile(t, filepath.Join(home, ".ssh", "id_ed25519"), "x")

	m, err := NewPathMatcher([]string{".env", "*.pem", "~/.ssh"}, []string{ws})
	require.NoError(t, err)
	files, dirs := expandBlocked(OSSandboxSpec{WritableDirs: []string{ws}, Blocked: m})
	joined := strings.Join(append(files, dirs...), ",")
	for _, want := range []string{filepath.Join(ws, ".env"), filepath.Join(ws, "deep", "a", "server.pem"), filepath.Join(home, ".ssh")} {
		assert.Contains(t, joined, want, "expected %s blocked; got %s", want, joined)
	}
	assert.NotContains(t, joined, "ok.txt", "unexpected entries: %s", joined)
	assert.NotContains(t, joined, "node_modules", "unexpected entries: %s", joined)
	assert.Contains(t, dirs, filepath.Join(home, ".ssh"), "~/.ssh should be masked as a directory")
	f, d := expandBlocked(OSSandboxSpec{WritableDirs: []string{ws}})
	assert.Equal(t, 0, len(f)+len(d), "no matcher -> nothing blocked")
}
