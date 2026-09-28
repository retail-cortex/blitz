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
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addWorker(t *testing.T, ws, name, content string) {
	t.Helper()
	dir := filepath.Join(ws, "workers", name)
	os.MkdirAll(dir, 0o755)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "WORKER.md"), []byte(content), 0o644))
}

// runCLIWithInput is runCLI with stdin.
func runCLIWithInput(t *testing.T, in string, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(in))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// fakeModel answers every model call with "done", as an OpenAI-compatible
// server set in the settings of home (from isolate): worker runs need a
// working model, and a model that can't be built fails them.
func fakeModel(t *testing.T, home string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_1","model":"fake","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}`)
	}))
	t.Cleanup(srv.Close)
	dir := filepath.Join(home, ".blitz")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	settings := fmt.Sprintf("[llm]\nprovider = \"ollama\"\n\n[llm.openai]\nbase_url = %q\nmodel = \"fake\"\n", srv.URL+"/v1")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.toml"), []byte(settings), 0o600))
}

func TestWorkersCommandsLocally(t *testing.T) {
	fakeModel(t, isolate(t))
	t.Setenv("BLITZ_SOCKET", filepath.Join(t.TempDir(), "none.sock")) // no service
	ws := t.TempDir()
	addWorker(t, ws, "deps", "---\ndescription: Report outdated modules\nschedule: Weekdays at 9:30\npermissions: [\"write:reports/\"]\n---\nWrite reports/deps.md.\n")

	out, err := runCLI(t, "-d", ws, "workers")
	require.NoError(t, err, "list: %v\n%s", err, out)
	require.Contains(t, out, "deps", "list: %v\n%s", err, out)
	require.Contains(t, out, "new", "list: %v\n%s", err, out)
	require.Contains(t, out, "30 9 * * 1-5", "list: %v\n%s", err, out)
	out, err = runCLIWithInput(t, "n\n", "-d", ws, "workers", "enable", "deps")
	assert.Error(t, err, "declined enable: %v\n%s", err, out)
	assert.Contains(t, out, "write:reports/", "declined enable: %v\n%s", err, out)
	assert.Contains(t, out, "sha256:", "declined enable: %v\n%s", err, out)
	listing, _ := runCLI(t, "-d", ws, "workers")
	assert.Contains(t, listing, "new", "declining enabled it")
	out, err = runCLI(t, "-d", ws, "workers", "enable", "deps", "--yes")
	require.NoError(t, err, "enable: %v\n%s", err, out)
	require.Contains(t, out, "deps enabled", "enable: %v\n%s", err, out)
	require.Contains(t, out, "blitzd", "enable: %v\n%s", err, out)
	out, err = runCLI(t, "-d", ws, "workers", "run", "deps")
	require.NoError(t, err, "run: %v\n%s", err, out)
	require.Contains(t, out, "succeeded", "run: %v\n%s", err, out)
	out, err = runCLI(t, "-d", ws, "workers", "runs", "deps")
	assert.NoError(t, err, "runs: %v\n%s", err, out)
	assert.Contains(t, out, "succeeded", "runs: %v\n%s", err, out)
	assert.Contains(t, out, "manual", "runs: %v\n%s", err, out)
	_, err = runCLI(t, "-d", ws, "workers", "run", "nope")
	assert.Equal(t, exitUsage, exitCodeFor(err), "unknown worker: %v", err)
	out, err = runCLI(t, "-d", ws, "workers", "disable", "deps")
	assert.NoError(t, err, "disable: %v\n%s", err, out)
	assert.Contains(t, out, "disabled", "disable: %v\n%s", err, out)
	_, err = runCLI(t, "-d", ws, "workers", "run", "deps")
	assert.Equal(t, exitUsage, exitCodeFor(err), "running a disabled worker: %v", err)
}

func TestWorkersCommandsThroughTheService(t *testing.T) {
	fakeModel(t, isolate(t))
	dir, err := os.MkdirTemp("/tmp", "cp") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	t.Setenv("BLITZ_SOCKET", sock)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- servicetest.Run(ctx, sock) }()
	defer func() { cancel(); <-done }()
	for deadline := time.Now().Add(10 * time.Second); !socket.Running(sock); time.Sleep(20 * time.Millisecond) {
		require.False(t, time.Now().After(deadline), "service didn't start")
	}

	ws := t.TempDir()
	addWorker(t, ws, "hello", "---\nschedule: daily at noon\n---\nSay hello.\n")
	enabled, enableErr := runCLI(t, "-d", ws, "workers", "enable", "hello", "--yes")
	require.NoError(t, enableErr, "enable through the service")
	require.Contains(t, enabled, "hello enabled")
	require.NotContains(t, enabled, "blitzd")
	out, err := runCLI(t, "-d", ws, "workers", "run", "hello")
	require.NoError(t, err, "run through the service: %v\n%s", err, out)
	require.Contains(t, out, "succeeded", "run through the service: %v\n%s", err, out)
}
