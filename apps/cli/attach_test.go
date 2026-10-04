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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The CLI attaches to a workspace the service holds, and --local, a run of
// its own, takes it from the service when no turn runs there.
func TestAttachToTheService(t *testing.T) {
	isolate(t)
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	dir, err := os.MkdirTemp("/tmp", "cp") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	t.Setenv("BLITZ_SOCKET", sock) // where the CLI looks for the service

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- servicetest.Run(ctx, sock) }()
	deadline := time.Now().Add(10 * time.Second)
	for !socket.Running(sock) {
		require.False(t, time.Now().After(deadline), "service didn't start")
		time.Sleep(20 * time.Millisecond)
	}

	ws := t.TempDir()
	// The CLI attaches to the service's workspace: here it fails on the
	// service's unconfigured model (exit 1). Opening the workspace itself
	// would have failed on the lock instead (exit 2).
	_, err = runCLI(t, "-d", ws, "--output-format", "json", "hello")
	assert.Equal(t, exitFailure, exitCodeFor(err), "attached one-shot: %v", err)
	assert.Contains(t, err.Error(), "model initialization failed", "attached one-shot: %v", err)
	// --local starts a service of its own, which the shared one lets have
	// the workspace: it fails on the same unconfigured model, not the lock.
	_, err = runCLI(t, "--local", "-d", ws, "hello")
	assert.Equal(t, exitFailure, exitCodeFor(err), "--local on a workspace the service holds: %v", err)
	assert.Contains(t, err.Error(), "model initialization failed", "--local on a workspace the service holds: %v", err)

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err, "service")
	case <-time.After(15 * time.Second):
		t.Fatal("service didn't stop")
	}
}

// controlledModel is fakeModel whose answers can be slowed down or made
// to fail while it runs.
type controlledModel struct{ slow, fail atomic.Bool }

func newControlledModel(t *testing.T, home string) *controlledModel {
	t.Helper()
	m := &controlledModel{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if m.slow.Load() {
			select {
			case <-time.After(2 * time.Second):
			case <-r.Context().Done():
				return
			}
		}
		if m.fail.Load() {
			http.Error(w, `{"error":{"message":"refused"}}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_1","model":"fake","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}`)
	}))
	t.Cleanup(srv.Close)
	dir := filepath.Join(home, ".blitz")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	settings := fmt.Sprintf("[llm]\nprovider = \"ollama\"\n\n[llm.openai]\nbase_url = %q\nmodel = \"fake\"\n", srv.URL+"/v1")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), []byte(settings), 0o600))
	return m
}

// startService runs the service on a short socket path for the test.
func startService(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sv") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	t.Setenv("BLITZ_SOCKET", sock)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- servicetest.Run(ctx, sock) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("service didn't stop")
		}
	})
	require.Eventually(t, func() bool { return socket.Running(sock) }, 10*time.Second, 20*time.Millisecond)
	return sock
}

// With the service running, the commands say so and work through it: the
// service's status, restart hints, project trust, the attached REPL, and
// following and attaching to background runs as they go and fail.
func TestCommandsWithTheService(t *testing.T) {
	model := newControlledModel(t, isolate(t))
	sock := startService(t)

	out, err := runCLI(t, "service", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "service:    answering on "+sock)
	out, err = runCLI(t, "mcp", "add", "x", "some-server")
	require.NoError(t, err)
	assert.Contains(t, out, "picks it up when it restarts")

	// Project settings, through the service's workspace.
	ws := projectDir(t)
	out, err = runCLI(t, "trust", ws)
	require.NoError(t, err)
	assert.Contains(t, out, "Not reviewed yet")
	cfg := mustConfig(t, ws)
	var warnings []string
	asked := false
	// --trust-project is a run of its own: a private service, which the
	// shared one lets have the workspace (no turn runs there).
	own, _, ownShared, err := openBackend(context.Background(), cfg, backendOptions{trustProject: true}, func(s string) { warnings = append(warnings, s) })
	require.NoError(t, err)
	assert.False(t, ownShared)
	assert.True(t, own.ProjectSettings().Loaded, "trusted for the run")
	stopPrivate()
	b, _, remote, err := openBackend(context.Background(), cfg, backendOptions{askTrust: func(api.ProjectSettings) string {
		asked = true
		return "trust"
	}}, func(s string) { warnings = append(warnings, s) })
	require.NoError(t, err)
	assert.True(t, remote)
	assert.True(t, asked, "the project's settings were asked about")
	assert.Equal(t, api.TrustTrusted, b.ProjectSettings().State)
	require.NoError(t, b.Close())

	// The REPL attaches.
	repl := stdio(t, "")
	_, err = runCLI(t, "-d", t.TempDir(), "-i")
	require.NoError(t, err, repl())
	assert.Contains(t, repl(), "Attached to the Blitz service")

	// A slow background run: still running, then followed to its end.
	model.slow.Store(true)
	runWS := t.TempDir()
	out, err = runCLI(t, "-d", runWS, "--bg", "take", "your", "time")
	require.NoError(t, err, out)
	id := strings.Fields(strings.TrimPrefix(out, "Started "))[0]
	out, err = runCLI(t, "logs", id)
	require.NoError(t, err, out)
	assert.Contains(t, out, "still running")
	stdio(t, "")
	_, err = runCLI(t, "attach", id)
	require.NoError(t, err, "attach follows the run, then the REPL ends with its input")
	out, err = runCLI(t, "logs", "-f", id)
	require.NoError(t, err, out)
	assert.NotContains(t, out, "still running")

	// A failing one.
	model.slow.Store(false)
	model.fail.Store(true)
	out, err = runCLI(t, "-d", runWS, "--bg", "fail")
	require.NoError(t, err, out)
	id = strings.Fields(strings.TrimPrefix(out, "Started "))[0]
	out, err = runCLI(t, "logs", "-f", id)
	require.NoError(t, err, out)
	assert.Contains(t, out, "The run failed")
}
