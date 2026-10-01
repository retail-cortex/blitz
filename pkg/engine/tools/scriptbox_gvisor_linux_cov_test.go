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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRunsc is a runsc that lists the given sandboxes, logs every other
// call, and fails it; it returns its path and the log's.
func fakeRunsc(t *testing.T, ids ...string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\ncase \" $* \" in\n*\" list \"*) printf '" + strings.Join(ids, `\n`) + `\n'; exit 0;;` + "\nesac\necho 'runsc: not really' >&2\nexit 1\n"
	runsc := filepath.Join(dir, "runsc")
	require.NoError(t, os.WriteFile(runsc, []byte(script), 0o755))
	return runsc, log
}

// runsc is found in RUNSC_PATH, then on PATH, then in ~/.blitz/bin.
func TestFindRunsc(t *testing.T) {
	runsc, _ := fakeRunsc(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, c := range []struct {
		name, env, path, want, err string
		inHome                     bool
	}{
		{name: "RUNSC_PATH", env: runsc, want: runsc},
		{name: "RUNSC_PATH missing", env: filepath.Join(home, "nope"), err: "RUNSC_PATH"},
		{name: "on PATH", path: filepath.Dir(runsc), want: runsc},
		{name: "in ~/.blitz/bin", inHome: true, want: filepath.Join(home, ".blitz", "bin", "runsc")},
		{name: "nowhere", err: "runsc not found"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("RUNSC_PATH", c.env)
			t.Setenv("PATH", cmpOr(c.path, t.TempDir()))
			p := filepath.Join(home, ".blitz", "bin", "runsc")
			os.RemoveAll(filepath.Dir(p))
			if c.inHome {
				require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
				require.NoError(t, os.WriteFile(p, nil, 0o755))
			}
			got, err := findRunsc()
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

// A runsc that can't run sandboxes fails the box's test run, after the
// box has swept away sandboxes whose Blitz is gone.
func TestGVisorBoxProbeAndSweep(t *testing.T) {
	dead := exec.Command("/bin/true")
	require.NoError(t, dead.Run())
	orphan := "cp-" + strconv.Itoa(dead.Process.Pid) + "-1"
	live := "cp-" + strconv.Itoa(os.Getpid()) + "-1"
	runsc, log := fakeRunsc(t, orphan, live, "someone-else")
	t.Setenv("RUNSC_PATH", runsc)
	state := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(state, "bundles", orphan), 0o700))

	_, _, err := NewScriptBox(ScriptBoxConfig{Mode: "gvisor", StateDir: state})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `skills.policy.sandbox is "gvisor": test run failed: start gVisor sandbox`)
	b, _ := os.ReadFile(log)
	calls := string(b)
	assert.Contains(t, calls, "kill "+orphan+" SIGKILL")
	assert.Contains(t, calls, "delete --force "+orphan)
	assert.NotContains(t, calls, "kill "+live, "a live Blitz's sandbox is left alone")
	assert.NotContains(t, calls, "someone-else")
	assert.NoDirExists(t, filepath.Join(state, "bundles", orphan))

	// The probe's answer is kept per runsc: no second test run.
	runs := func() int {
		b, _ := os.ReadFile(log)
		n := 0
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if !strings.Contains(line, " list ") && !strings.Contains(line, " kill ") && !strings.Contains(line, " delete ") {
				n++
			}
		}
		return n
	}
	before := runs()
	assert.Positive(t, before, "the first box tried a test run")
	_, _, err = NewScriptBox(ScriptBoxConfig{Mode: "gvisor", StateDir: state})
	assert.Error(t, err)
	assert.Equal(t, before, runs(), "the second didn't")

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, _, err = NewScriptBox(ScriptBoxConfig{Mode: "gvisor", StateDir: filepath.Join(file, "state")})
	assert.Error(t, err, "a state directory that can't be made")
}

// Sandbox names carry their Blitz's process ID.
func TestSandboxOwner(t *testing.T) {
	for _, c := range []struct {
		id  string
		pid int
		ok  bool
	}{
		{id: "cp-42-7", pid: 42, ok: true},
		{id: "cp-0-1"},
		{id: "cp-x-1"},
		{id: "cp-42"},
		{id: "other-42-1"},
	} {
		t.Run(c.id, func(t *testing.T) {
			pid, ok := sandboxOwner(c.id)
			assert.Equal(t, c.ok, ok)
			if c.ok {
				assert.Equal(t, c.pid, pid)
			}
		})
	}
	assert.True(t, processAlive(os.Getpid()))
}
