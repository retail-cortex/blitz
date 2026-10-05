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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/client"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/loginitem"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The real starters, before isolate replaces them.
var realStartShared, realStartPrivate = startShared, startPrivate

// A private service's flags: the run's settings, its prompt in a file.
func TestServiceOptionsArgs(t *testing.T) {
	dir := t.TempDir()
	args, err := serviceOptions{
		Config: "/c.toml", Model: "m", Agent: "a", Agency: "high",
		PluginDirs: []string{"/p"}, AddDirs: []string{"/d1", "/d2"}, SessionDir: "/s",
		TrustProject: true, AppendSystemPrompt: "Be brief.",
	}.args(dir)
	require.NoError(t, err)
	prompt := filepath.Join(dir, "prompt.md")
	assert.Equal(t, []string{"--config", "/c.toml", "--model", "m", "--agent", "a", "--agency", "high", "--plugin-dir", "/p", "--add-dir", "/d1", "--add-dir", "/d2", "--session-dir", "/s", "--trust-project", "--append-system-prompt-file", prompt}, args)
	data, err := os.ReadFile(prompt)
	require.NoError(t, err)
	assert.Equal(t, "Be brief.", string(data))

	none, err := serviceOptions{}.args(dir)
	require.NoError(t, err)
	assert.Empty(t, none)
}

// Which runs need a service of their own.
func TestBackendPrivate(t *testing.T) {
	tests := []struct {
		name string
		o    backendOptions
		want bool
	}{
		{"plain", backendOptions{}, false},
		{"local", backendOptions{local: true}, true},
		{"trust", backendOptions{trustProject: true}, true},
		{"prompt", backendOptions{appendPrompt: "x"}, true},
		{"no persistence", backendOptions{sessionDir: "/s"}, true},
		{"model", backendOptions{flags: &globalFlags{model: "m"}}, true},
		{"agent", backendOptions{flags: &globalFlags{agent: "a"}}, true},
		{"agency", backendOptions{flags: &globalFlags{agency: "high"}}, true},
		{"plugin", backendOptions{flags: &globalFlags{pluginDirs: []string{"p"}}}, true},
		{"add dir", backendOptions{flags: &globalFlags{addDirs: []string{"d"}}}, true},
		{"only a config file", backendOptions{flags: &globalFlags{config: "/c"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.o.private())
		})
	}
}

// The real blitzd, started as the CLI starts it: the shared service stops
// by itself once idle; a private one with the run's settings stops with
// the run.
func TestRealServices(t *testing.T) {
	isolate(t)
	serviceDirs = loginitem.ServiceDirs // the runfiles' blitzd (isolate restores it)
	if _, err := serviceBinary(); err != nil {
		t.Skip("blitzd isn't in the test's runfiles")
	}
	ctx := context.Background()
	sock := os.Getenv("BLITZ_SOCKET")

	old := sharedIdle
	sharedIdle = 2 * time.Second
	t.Cleanup(func() { sharedIdle = old })
	require.NoError(t, realStartShared(ctx, sock))
	assert.True(t, socket.Running(sock))
	_, err := client.Attach(ctx, sock, t.TempDir(), nil)
	require.NoError(t, err)
	for deadline := time.Now().Add(20 * time.Second); socket.Running(sock); time.Sleep(100 * time.Millisecond) {
		require.False(t, time.Now().After(deadline), "the shared service didn't stop when idle")
	}

	psock, err := realStartPrivate(ctx, serviceOptions{SessionDir: t.TempDir(), AppendSystemPrompt: "Be brief."})
	require.NoError(t, err)
	assert.True(t, socket.Running(psock))
	assert.NoFileExists(t, filepath.Join(filepath.Dir(psock), "prompt.md"), "the prompt, once read, isn't left behind")
	stopPrivate()
	assert.False(t, socket.Running(psock), "the private service stopped with the run")
	assert.NoDirExists(t, filepath.Dir(psock), "its folder removed")
}

// Without a blitzd, neither service starts, and the run says why.
func TestStartersWithoutBlitzd(t *testing.T) {
	isolate(t)
	t.Setenv("PATH", t.TempDir())
	ctx := context.Background()
	assert.Error(t, realStartShared(ctx, filepath.Join(t.TempDir(), "s.sock")))
	_, err := realStartPrivate(ctx, serviceOptions{})
	assert.Error(t, err)

	startShared = func(context.Context, string) error { return errors.New("no blitzd") }
	_, err = ensureService(ctx)
	assert.ErrorContains(t, err, "starting the Blitz service: no blitzd")
	_, _, _, err = openBackend(ctx, &config.Config{}, backendOptions{}, func(string) {})
	assert.ErrorContains(t, err, "no blitzd")
	startPrivate = func(context.Context, serviceOptions) (string, error) { return "", errors.New("no blitzd") }
	_, _, _, err = openBackend(ctx, &config.Config{}, backendOptions{local: true}, func(string) {})
	assert.ErrorContains(t, err, "starting a Blitz service for this run: no blitzd")
}

// Waiting for a service: one that stops first, or a run that's cancelled,
// is said; what it printed goes with the error.
func TestWaitService(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	exited := make(chan struct{})
	close(exited)
	assert.ErrorContains(t, waitService(context.Background(), sock, exited), "stopped before answering")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, waitService(ctx, sock, nil), context.Canceled)

	assert.EqualError(t, withStderr(errors.New("failed"), "  bad flag\n"), "failed: bad flag")
	assert.EqualError(t, withStderr(errors.New("failed"), ""), "failed")
}

// A model error from the service reads on one line, credentials masked;
// a price is known by its model's name or prefix.
func TestModelErrorTextAndPrice(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.Gemini.APIKey = "AIzaSyTESTSECRET-0123456789"
	text := modelErrorText(errors.New("bad key AIzaSyTESTSECRET-0123456789\nmore"), cfg)
	assert.True(t, strings.HasPrefix(text, "bad key "), text)
	assert.NotContains(t, text, "SECRET")
	assert.NotContains(t, text, "more", "one line")
	cfg.Pricing = map[string]config.ModelPrice{"gemini-3.8-flash": {InputPerMTok: 1}}
	assert.True(t, hasPrice(cfg, "gemini-3.8-flash-001"))
	assert.False(t, hasPrice(cfg, "mystery"))
}

// A blitzd that stops before answering: the run says so, with what it
// printed, and a private one's folder is cleaned up at the end.
func TestStartersWhenBlitzdFails(t *testing.T) {
	isolate(t)
	blitzdOnPath(t, `echo "boom: $*" >&2; exit 1`)
	ctx := context.Background()
	err := realStartShared(ctx, filepath.Join(t.TempDir(), "s.sock"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom: --socket")
	assert.Contains(t, err.Error(), "--idle-exit")

	_, err = realStartPrivate(ctx, serviceOptions{Model: "m", AppendSystemPrompt: "Be brief."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--exit-with-stdin")
	assert.Contains(t, err.Error(), "--append-system-prompt-file")
	stopPrivate()
}

// A run of its own takes the run's flags: folders made absolute.
func TestServiceOptionsFromFlags(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, _ := os.Getwd()
	o := backendOptions{appendPrompt: "p", sessionDir: "/s", trustProject: true, flags: &globalFlags{config: "/c", model: "m", agent: "a", agency: "high", pluginDirs: []string{"plugins"}, addDirs: []string{"extra"}}}
	so := o.serviceOptions(&config.Config{})
	assert.Equal(t, serviceOptions{Config: "/c", Model: "m", Agent: "a", Agency: "high", PluginDirs: []string{filepath.Join(cwd, "plugins")}, AddDirs: []string{filepath.Join(cwd, "extra")}, SessionDir: "/s", TrustProject: true, AppendSystemPrompt: "p"}, so)
	assert.Equal(t, serviceOptions{}, backendOptions{}.serviceOptions(&config.Config{}))
}

// A blitzd that can't start, a temporary folder that can't be made, and a
// service already running (nothing to start).
func TestStartersEdges(t *testing.T) {
	isolate(t)
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "blitzd"), []byte("#!/nonexistent/interpreter\n"), 0o755))
	t.Setenv("PATH", bin)
	ctx := context.Background()
	assert.Error(t, realStartShared(ctx, filepath.Join(t.TempDir(), "s.sock")), "can't start")
	_, err := realStartPrivate(ctx, serviceOptions{})
	assert.Error(t, err, "can't start")
	stopPrivate()

	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	_, err = realStartPrivate(ctx, serviceOptions{})
	assert.Error(t, err, "no temporary folder")

	sock := os.Getenv("BLITZ_SOCKET")
	require.NoError(t, startShared(ctx, sock)) // the test's own
	got, err := ensureService(ctx)
	require.NoError(t, err)
	assert.Equal(t, sock, got, "running: nothing to start")
}

// A prompt that can't be written into the service's folder is the run's
// error.
func TestServiceOptionsPromptUnwritable(t *testing.T) {
	_, err := serviceOptions{AppendSystemPrompt: "p"}.args(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err)
}

// Clients starting the service at the same moment take turns: it starts
// once, and every one of them gets it.
func TestEnsureServiceTakesTurns(t *testing.T) {
	isolate(t)
	inner := startShared
	var starts atomic.Int32
	startShared = func(ctx context.Context, sock string) error {
		starts.Add(1)
		time.Sleep(300 * time.Millisecond) // a slow start: the others must wait for it
		return inner(ctx, sock)
	}
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = ensureService(context.Background())
		}()
	}
	wg.Wait()
	for _, err := range errs {
		assert.NoError(t, err)
	}
	assert.Equal(t, int32(1), starts.Load(), "started once")
	assert.NoFileExists(t, os.Getenv("BLITZ_SOCKET")+".start", "the lock released")
}

// A start lock its holder left behind (it died) is taken over once stale;
// a run cancelled while waiting for it says so.
func TestStartLockStale(t *testing.T) {
	isolate(t)
	sock := os.Getenv("BLITZ_SOCKET")
	require.NoError(t, os.WriteFile(sock+".start", nil, 0o600))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := startLock(ctx, sock)
	assert.ErrorIs(t, err, context.Canceled, "held, fresh: waits")

	old := staleLock
	staleLock = 100 * time.Millisecond
	t.Cleanup(func() { staleLock = old })
	_, err = ensureService(context.Background())
	require.NoError(t, err, "the stale lock taken over")
	assert.True(t, socket.Running(sock))
}

// A started service's output goes to a file (never a pipe it could outlive),
// read back for an error and removed afterwards.
func TestServiceOutput(t *testing.T) {
	f, read, done, err := serviceOutput()
	require.NoError(t, err)
	fmt.Fprint(f, "listening")
	assert.Equal(t, "listening", read())
	done()
	assert.NoFileExists(t, f.Name())
	assert.DirExists(t, serviceDir())
}

// A socket folder that can't be made, or written, stops the start with
// the reason.
func TestStartLockUnwritable(t *testing.T) {
	isolate(t)
	file := filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	t.Setenv("BLITZ_SOCKET", filepath.Join(file, "s.sock"))
	_, err := ensureService(context.Background())
	assert.ErrorContains(t, err, "starting the Blitz service")

	ro := t.TempDir()
	require.NoError(t, os.Chmod(ro, 0o500))
	t.Cleanup(func() { os.Chmod(ro, 0o700) })
	_, err = startLock(context.Background(), filepath.Join(ro, "s.sock"))
	assert.Error(t, err)
}

// A service that never answers, a start lock that's never released, no
// temporary file for a service's output, and no service for a command
// that needs one: each says so.
func TestStartersGiveUp(t *testing.T) {
	isolate(t)
	oldWait := startWait
	startWait = 200 * time.Millisecond
	t.Cleanup(func() { startWait = oldWait })
	sock := os.Getenv("BLITZ_SOCKET")
	assert.ErrorContains(t, waitService(context.Background(), sock, nil), "didn't answer")

	blitzdOnPath(t, "exit 1")
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, realStartShared(context.Background(), sock), "no file for its output")
	_, _, _, err := serviceOutput()
	assert.Error(t, err)

	file := filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	t.Setenv("BLITZ_SOCKET", filepath.Join(file, "s.sock"))
	_, err = attachWorkspace(context.Background(), &globalFlags{dir: t.TempDir()})
	assert.ErrorContains(t, err, "starting the Blitz service")
}
