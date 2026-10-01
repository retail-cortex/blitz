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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The OS backend: which directories it hands the sandbox, the environment
// it sets, and how a command's end is reported.
func TestOSBoxRun(t *testing.T) {
	var specs []OSSandboxSpec
	wrap, probe := platformSandbox, platformSandboxProbe
	t.Cleanup(func() { platformSandbox, platformSandboxProbe = wrap, probe })
	platformSandbox = func(s OSSandboxSpec) (sandboxWrapper, error) {
		specs = append(specs, s)
		return prefixWrapper(), nil
	}
	platformSandboxProbe = func() error { return nil }
	t.Setenv("RUNSC_PATH", filepath.Join(t.TempDir(), "missing-runsc"))

	box, note, err := NewScriptBox(ScriptBoxConfig{StateDir: t.TempDir()})
	require.NoError(t, err)
	assert.Equal(t, "os", box.Name(), "auto falls back to the OS sandbox")
	assert.Contains(t, note, "gVisor unavailable")

	ws, _ := filepath.EvalSymlinks(t.TempDir())
	out := filepath.Join(ws, "out")
	require.NoError(t, os.Mkdir(out, 0o700))
	other, _ := filepath.EvalSymlinks(t.TempDir())
	run := func(ctx context.Context, req ScriptRequest) (ScriptResult, string, error) {
		var buf bytes.Buffer
		req.Stdout, req.Stderr = &buf, &buf
		res, err := box.Run(ctx, req)
		return res, buf.String(), err
	}

	res, got, err := run(context.Background(), ScriptRequest{
		Argv: []string{"/bin/sh", "-c", `echo "$GIVEN $HOME $LANG"; pwd`}, Dir: ws, Env: []string{"GIVEN=yes"},
		Writable: []string{ws, filepath.Join(ws, "missing")}, ReadOnly: []string{out, other, filepath.Join(ws, "missing")}, Network: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.Contains(t, got, "yes ")
	assert.Contains(t, got, " C.UTF-8\n"+ws+"\n")
	spec := specs[len(specs)-1]
	assert.Equal(t, []string{ws}, spec.WritableDirs[1:], "the private temp directory, then the writable ones that exist")
	assert.Equal(t, []string{out}, spec.ReadOnlyDirs, "only read-only paths inside writable ones are listed")
	assert.True(t, spec.AllowNetwork)
	assert.Equal(t, SandboxRequired, spec.Mode)

	for _, c := range []struct {
		name     string
		req      ScriptRequest
		exit     int
		timedOut bool
		err      string
	}{
		{name: "no command", err: "no command"},
		{name: "exit code", req: ScriptRequest{Argv: []string{"/bin/sh", "-c", "exit 7"}, Dir: ws}, exit: 7},
		{name: "timeout", req: ScriptRequest{Argv: []string{"/bin/sh", "-c", "sleep 30"}, Dir: ws, Timeout: 200 * time.Millisecond}, exit: -1, timedOut: true},
		{name: "can't start", req: ScriptRequest{Argv: []string{"/bin/true"}, Dir: filepath.Join(ws, "missing")}, err: "no such file or directory"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res, _, err := run(context.Background(), c.req)
			if c.err != "" {
				assert.ErrorContains(t, err, c.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.exit, res.ExitCode)
			assert.Equal(t, c.timedOut, res.TimedOut)
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = run(ctx, ScriptRequest{Argv: []string{"/bin/true"}, Dir: ws})
	assert.ErrorIs(t, err, context.Canceled)

	platformSandbox = func(OSSandboxSpec) (sandboxWrapper, error) { return nil, errors.New("gone") }
	_, _, err = run(context.Background(), ScriptRequest{Argv: []string{"/bin/true"}, Dir: ws})
	assert.ErrorIs(t, err, ErrNoScriptBox)
}

// Asking for gVisor where it isn't fails rather than falling back.
func TestScriptBoxGVisorRequired(t *testing.T) {
	t.Setenv("RUNSC_PATH", filepath.Join(t.TempDir(), "missing-runsc"))
	t.Setenv("PATH", t.TempDir())
	_, _, err := NewScriptBox(ScriptBoxConfig{Mode: "GVisor", StateDir: t.TempDir()})
	assert.ErrorContains(t, err, `skills.policy.sandbox is "gvisor"`)
}
