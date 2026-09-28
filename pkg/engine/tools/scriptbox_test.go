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
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptDirs are host directories for a script: one it may write, one it
// may only read, and one outside everything it was given.
type scriptDirs struct{ writable, readOnly, outside string }

func newScriptDirs(t *testing.T) scriptDirs {
	t.Helper()
	d := scriptDirs{writable: t.TempDir(), readOnly: t.TempDir(), outside: t.TempDir()}
	for _, p := range []string{d.writable, d.readOnly, d.outside} {
		t.Run(p, func(t *testing.T) {
			// Canonical paths: on macOS the temp dir is behind a symlink.
			c, err := filepath.EvalSymlinks(p)
			require.NoError(t, err)
			os.WriteFile(filepath.Join(c, "hello.txt"), []byte("hello"), 0o644)
			switch p {
			case d.writable:
				d.writable = c
			case d.readOnly:
				d.readOnly = c
			default:
				d.outside = c
			}
		})
	}
	return d
}

func runScript(t *testing.T, box ScriptBox, d scriptDirs, script string, timeout time.Duration, env ...string) (ScriptResult, string, error) {
	t.Helper()
	var out bytes.Buffer
	res, err := box.Run(context.Background(), ScriptRequest{
		Argv: []string{"/bin/sh", "-c", script}, Dir: d.writable, Env: env,
		ReadOnly: []string{d.readOnly}, Writable: []string{d.writable},
		Stdout: &out, Stderr: &out, Timeout: timeout,
	})
	return res, out.String(), err
}

// testScriptBoxBehaviour checks what every backend must guarantee.
func testScriptBoxBehaviour(t *testing.T, box ScriptBox) {
	d := newScriptDirs(t)
	t.Setenv("CP_TEST_SECRET", "leaked")

	res, out, err := runScript(t, box, d, "cat "+d.readOnly+"/hello.txt && echo made > "+d.writable+"/out.txt && pwd", 0)
	require.NoError(t, err, "allowed work: %+v %v\n%s", res, err, out)
	require.Equal(t, 0, res.ExitCode, "allowed work: %+v %v\n%s", res, err, out)
	require.Contains(t, out, "hello", "allowed work: %+v %v\n%s", res, err, out)
	require.Contains(t, out, d.writable, "allowed work: %+v %v\n%s", res, err, out)
	b, _ := os.ReadFile(filepath.Join(d.writable, "out.txt"))
	require.Equal(t, "made\n", string(b), "write to the writable dir didn't reach the host: %q", b)
	for _, target := range []string{d.readOnly + "/x.txt", d.outside + "/x.txt"} {
		t.Run(target, func(t *testing.T) {
			res, out, _ := runScript(t, box, d, "echo pwned > "+target, 0)
			assert.NotEqual(t, 0, res.ExitCode, "wrote %s: %s", target, out)
			_, err := os.Stat(target)
			assert.Error(t, err, "%s exists on the host", target)
		})
	}

	res, out, _ = runScript(t, box, d, `echo "secret=[$CP_TEST_SECRET] given=[$GIVEN] home=[$HOME]"; echo tmp > "$TMPDIR/t" && cat "$TMPDIR/t"`, 0, "GIVEN=yes")
	assert.Contains(t, out, "secret=[]", "environment: %+v\n%s", res, out)
	assert.Contains(t, out, "given=[yes]", "environment: %+v\n%s", res, out)
	assert.NotContains(t, out, "home=[]", "environment: %+v\n%s", res, out)
	assert.Contains(t, out, "\ntmp", "environment: %+v\n%s", res, out)

	start := time.Now()
	res, _, err = runScript(t, box, d, "sleep 30", 500*time.Millisecond)
	assert.NoError(t, err, "timeout: %+v %v after %v", res, err, time.Since(start))
	assert.True(t, res.TimedOut, "timeout: %+v %v after %v", res, err, time.Since(start))
	assert.NotEqual(t, 0, res.ExitCode, "timeout: %+v %v after %v", res, err, time.Since(start))
	assert.LessOrEqual(t, time.Since(start), 20*time.Second, "timeout: %+v %v after %v", res, err, time.Since(start))

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(500 * time.Millisecond); cancel() }()
	_, err = box.Run(ctx, ScriptRequest{Argv: []string{"/bin/sh", "-c", "sleep 30"}, Dir: d.writable, Writable: []string{d.writable}})
	assert.ErrorIs(t, err, context.Canceled, "cancel: %v", err)
}

func TestOSScriptBox(t *testing.T) {
	box, _, err := NewScriptBox(ScriptBoxConfig{Mode: "os"})
	if err != nil {
		t.Skipf("no OS sandbox on %s: %v", runtime.GOOS, err)
	}
	require.Equal(t, "os", box.Name(), "name %q", box.Name())
	testScriptBoxBehaviour(t, box)
}

func TestScriptBoxSelection(t *testing.T) {
	_, _, err := NewScriptBox(ScriptBoxConfig{Mode: "docker"})
	assert.Error(t, err, "unknown mode")
	assert.Contains(t, err.Error(), "unknown", "unknown mode: %v", err)
	if runtime.GOOS != "linux" {
		_, _, err := NewScriptBox(ScriptBoxConfig{Mode: "gvisor"})
		assert.Error(t, err, "gvisor off Linux")
		assert.Contains(t, err.Error(), "only on Linux", "gvisor off Linux: %v", err)
	}
	// Without any sandbox, scripts don't run: there is no unsandboxed fallback.
	orig := platformSandboxProbe
	platformSandboxProbe = func() error { return errors.New("no sandbox here") }
	defer func() { platformSandboxProbe = orig }()
	t.Setenv("RUNSC_PATH", filepath.Join(t.TempDir(), "missing-runsc"))
	_, note, err := NewScriptBox(ScriptBoxConfig{Mode: "auto", StateDir: t.TempDir()})
	assert.ErrorIs(t, err, ErrNoScriptBox, "auto with nothing available: %q %v", note, err)
	assert.NotEqual(t, "", note, "auto with nothing available: %q %v", note, err)
}
