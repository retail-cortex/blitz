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

//go:build linux

package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gvisor.dev/gvisor/sandboxexec/sandbox"
)

func gvisorBoxForTest(t *testing.T) *gvisorBox {
	t.Helper()
	if _, err := findRunsc(); err != nil {
		t.Skipf("gVisor tests need runsc: %v", err)
	}
	box, _, err := NewScriptBox(ScriptBoxConfig{Mode: "gvisor", StateDir: t.TempDir()})
	require.NoError(t, err, "runsc is installed but gVisor doesn't work")
	return box.(*gvisorBox)
}

func runscList(t *testing.T, b *gvisorBox) []string {
	t.Helper()
	out, err := exec.Command(b.runsc, "--root", b.stateDir, "list", "-quiet").Output()
	require.NoError(t, err, "runsc list")
	return strings.Fields(string(out))
}

func TestGVisorScriptBox(t *testing.T) {
	b := gvisorBoxForTest(t)
	testScriptBoxBehaviour(t, b)
	d := newScriptDirs(t)

	// Only what is mounted exists: not the outside directory, not $HOME.
	home, _ := os.UserHomeDir()
	for _, p := range []string{d.outside, home} {
		res, out, _ := runScript(t, b, d, "ls "+p, 0)
		assert.NotEqual(t, 0, res.ExitCode, "%s exists inside the sandbox:\n%s", p, out)
	}
	_, uname, _ := runScript(t, b, d, "uname -r", 0)
	assert.Contains(t, uname, "gvisor", "running under gVisor")
	// Every run cleans up after itself, including timeouts and cancellations
	// (run above by testScriptBoxBehaviour).
	require.Empty(t, runscList(t, b), "sandboxes left behind")
	bundles, _ := os.ReadDir(b.bundles)
	require.Empty(t, bundles, "bundles left behind")
}

// A sandbox whose Blitz died (so its Close never ran) is removed the
// next time a box is set up.
func TestGVisorSweepsOrphans(t *testing.T) {
	b := gvisorBoxForTest(t)
	dead := exec.Command("/bin/true")
	require.NoError(t, dead.Run())
	orphan := "cp-" + strconv.Itoa(dead.Process.Pid) + "-1"
	live := "cp-" + strconv.Itoa(os.Getpid()) + "-999"
	os.Setenv(sandbox.RunscPathEnvVar, b.runsc)
	for _, id := range []string{orphan, live} {
		_, err := sandbox.New(context.Background(), sandbox.WithID(id), sandbox.WithStateDir(b.stateDir), sandbox.WithRuntimeDir(b.bundles))
		require.NoError(t, err)
	}
	require.Len(t, runscList(t, b), 2, "before the sweep")
	b.sweep(context.Background())
	require.Equal(t, []string{live}, runscList(t, b), "only the live sandbox is left")
	require.NoDirExists(t, filepath.Join(b.bundles, orphan), "the orphan's bundle is removed")
	exec.Command(b.runsc, "--root", b.stateDir, "kill", live, "SIGKILL").Run()
	exec.Command(b.runsc, "--root", b.stateDir, "delete", "--force", live).Run()
}

// Blocked files and directories inside a mounted path are hidden.
func TestGVisorHidesBlockedPaths(t *testing.T) {
	if _, err := findRunsc(); err != nil {
		t.Skipf("gVisor tests need runsc: %v", err)
	}
	m, err := NewPathMatcher([]string{".env", "secrets"}, nil)
	require.NoError(t, err)
	box, _, err := NewScriptBox(ScriptBoxConfig{Mode: "gvisor", StateDir: t.TempDir(), Blocked: m})
	require.NoError(t, err)
	d := newScriptDirs(t)
	os.WriteFile(filepath.Join(d.readOnly, ".env"), []byte("API_KEY=topsecret"), 0o600)
	os.MkdirAll(filepath.Join(d.readOnly, "secrets"), 0o700)
	os.WriteFile(filepath.Join(d.readOnly, "secrets", "key.pem"), []byte("PRIVATE"), 0o600)
	_, out, _ := runScript(t, box, d, "cat "+d.readOnly+"/.env; ls "+d.readOnly+"/secrets; cat "+d.readOnly+"/hello.txt", 0)
	require.NotContains(t, out, "topsecret", "a blocked file is visible")
	require.NotContains(t, out, "key.pem", "a blocked folder is visible")
	require.Contains(t, out, "hello", "an allowed file is hidden")
}
