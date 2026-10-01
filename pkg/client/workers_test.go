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

package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/apps/service/servicetest"
	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/socket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runService runs the whole service, as blitzd does, with its model an
// OpenAI-compatible server that answers "done", and returns its socket.
func runService(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MODENV_PREFIX", "")
	for _, k := range []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "LLM_PROVIDER", "OPENAI_BASE_URL", "BLITZ_SOCKET"} {
		t.Setenv(k, "")
	}
	t.Chdir(t.TempDir())
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_1","model":"fake","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}`)
	}))
	t.Cleanup(model.Close)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".blitz"), 0o700))
	settings := "[blitz]\ndefault_model = \"fake\"\n\n[llm]\nprovider = \"ollama\"\n\n[llm.openai]\nbase_url = \"" + model.URL + "/v1\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(home, ".blitz", ".env.toml"), []byte(settings), 0o600))

	dir, err := os.MkdirTemp("/tmp", "bc") // socket paths must be short
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- servicetest.Run(ctx, sock) }()
	t.Cleanup(func() { cancel(); <-done })
	require.Eventually(t, func() bool { return socket.Running(sock) }, 10*time.Second, 20*time.Millisecond, "the service didn't start")
	return sock
}

// A workspace's workers over the service: listed, enabled as shown, run
// now with their events, their runs listed, disabled, and their typed
// errors.
func TestRemoteWorkers(t *testing.T) {
	sock := runService(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	wd := filepath.Join(dir, "workers", "report")
	require.NoError(t, os.MkdirAll(wd, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wd, "WORKER.md"), []byte("---\nschedule: Daily at 6 AM\n---\nWrite the report.\n"), 0o644))
	w := AttachWorkers(sock, dir)

	list, err := w.ListWorkers()
	require.NoError(t, err)
	require.Len(t, list, 1)
	wk := list[0]
	assert.Equal(t, "report", wk.Name)
	assert.Equal(t, api.StateNew, wk.State)
	assert.Equal(t, "0 6 * * *", wk.Cron)

	_, err = w.RunWorker(context.Background(), "report", nil)
	assert.ErrorIs(t, err, api.ErrWorkerNotEnabled, "run before enabling")
	_, err = w.EnableWorker("report", "sha256:stale")
	assert.ErrorIs(t, err, api.ErrHashMismatch)
	on, err := w.EnableWorker("report", wk.Hash)
	require.NoError(t, err)
	assert.Equal(t, api.StateEnabled, on.State)
	assert.False(t, on.Next.IsZero(), "the next run is scheduled")

	var texts []string
	run, err := w.RunWorker(context.Background(), "report", func(e api.Event) {
		if e.Text != nil {
			texts = append(texts, e.Text.Text)
		}
	})
	require.NoError(t, err)
	assert.Equal(t, api.RunSucceeded, run.Status, "run %+v", run)
	assert.True(t, run.Manual)
	assert.NotEmpty(t, run.SessionID)
	runs, err := w.WorkerRuns("report", 5)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, run.ID, runs[0].ID)
	list, err = w.ListWorkers()
	require.NoError(t, err)
	require.NotNil(t, list[0].LastRun, "the last run is listed")
	assert.Equal(t, run.ID, list[0].LastRun.ID)

	_, err = w.UndoWorkerRun(run.ID, false)
	assert.ErrorIs(t, err, api.ErrNothingToUndo, "a run that changed nothing")
	off, err := w.DisableWorker("report")
	require.NoError(t, err)
	assert.Equal(t, api.StateDisabled, off.State)
	_, err = w.DisableWorker("nope")
	assert.ErrorIs(t, err, api.ErrUnknownWorker)

	// The workspace and the background runs over the same socket.
	r, err := Attach(context.Background(), sock, dir, nil)
	require.NoError(t, err)
	assert.Equal(t, dir, r.Dir())
	assert.NoError(t, r.ModelErr())
	bg, err := AttachBackground(sock).List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, bg)
}

// With the service gone, every worker and background run call fails, and
// attaching does.
func TestRemoteClientsWithoutTheService(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	ctx := context.Background()
	w := AttachWorkersHTTP(http.DefaultClient, srv.URL, t.TempDir())
	b := AttachBackgroundHTTP(http.DefaultClient, srv.URL)
	for name, call := range map[string]func() error{
		"Attach":        func() error { _, err := AttachHTTP(ctx, http.DefaultClient, srv.URL, t.TempDir(), nil); return err },
		"List":          func() error { _, err := b.List(ctx); return err },
		"Find":          func() error { _, err := b.Find(ctx, "bg-1"); return err },
		"Stop":          func() error { _, err := b.Stop(ctx, "bg-1"); return err },
		"Logs":          func() error { _, _, err := b.Logs(ctx, "bg-1", false, func(api.Event) {}); return err },
		"ListWorkers":   func() error { _, err := w.ListWorkers(); return err },
		"EnableWorker":  func() error { _, err := w.EnableWorker("x", "h"); return err },
		"DisableWorker": func() error { _, err := w.DisableWorker("x"); return err },
		"RunWorker":     func() error { _, err := w.RunWorker(context.Background(), "x", nil); return err },
		"WorkerRuns":    func() error { _, err := w.WorkerRuns("x", 1); return err },
		"UndoWorkerRun": func() error { _, err := w.UndoWorkerRun("x", false); return err },
	} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, call())
		})
	}
}
