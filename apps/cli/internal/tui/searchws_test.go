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

package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /search with no source searches the workspace's defaults; a source, all,
// status and reindex do what they say; none of them leads to a turn.
func TestSlashSearchWorkspace(t *testing.T) {
	app := newTestApp(t, nil)
	dir := app.Workspace.Dir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hangar.md"), []byte("# Hangar\n\nThe zeppelin docks here.\n"), 0o644))
	before := time.Now()
	require.NoError(t, app.Workspace.Reindex())
	require.Eventually(t, func() bool {
		s := app.Workspace.SearchStatus()
		return !s.Scanning && s.LastScan.After(before)
	}, 20*time.Second, 20*time.Millisecond)

	for _, tc := range []struct {
		args string
		want []string
	}{
		{"zeppelin docks", []string{"1. hangar.md:3", "The zeppelin docks here."}},
		{"all zeppelin", []string{"hangar.md:3"}},
		{"chats zeppelin", []string{"Nothing in the workspace matches zeppelin."}},
		{"status", []string{"Search index: files 1, documents 0, chats 0, notes 0."}},
		{"reindex", []string{"Scanning the workspace for search."}},
		{"files", []string{"Usage: /search <terms>"}},
	} {
		t.Run(tc.args, func(t *testing.T) {
			var ok bool
			out := captureStdout(t, func() { _, ok = prepareSearch(context.Background(), app, tc.args, nil) })
			assert.False(t, ok, "no turn")
			for _, w := range tc.want {
				assert.Contains(t, out, w)
			}
		})
	}
}

// The hits and the status print what a reader needs: where, which page or
// chat, the passage; and a status that's off, scanning or failing says so.
func TestPrintSearch(t *testing.T) {
	out := captureStdout(t, func() {
		PrintSearchHits(os.Stdout, []api.SearchHit{
			{Source: api.SearchDocuments, Ref: "report.pdf", Line: 12, Section: "page 2", Snippet: "revenue\nrose", Summary: "The quarterly report."},
			{Source: api.SearchChats, Ref: "s-1", Title: "Airships", Line: 3, Snippet: "lifting gas"},
		})
	})
	for _, w := range []string{"1. report.pdf:12", "· page 2", "The quarterly report.", "revenue", "rose", "2. chat: Airships", "lifting gas"} {
		assert.Contains(t, out, w)
	}
	assert.NotContains(t, out, "s-1:3", "a chat has no line to go to")

	for name, tc := range map[string]struct {
		status api.SearchStatus
		want   []string
	}{
		"off":       {api.SearchStatus{}, []string{"workspace search is off"}},
		"scanning":  {api.SearchStatus{Enabled: true, Scanning: true}, []string{"Last scan: not yet", "still being indexed"}},
		"problems":  {api.SearchStatus{Enabled: true, Unreadable: 2, Error: "disk full", LastScan: time.Now()}, []string{"2 items couldn't be read.", "disk full"}},
		"semantic":  {api.SearchStatus{Enabled: true, LastScan: time.Now(), EmbeddingModel: "gemini/text-embedding-004", Embedded: 40, EmbedError: "quota"}, []string{"Semantic search: gemini/text-embedding-004, 40 chunks embedded.", "Embedding stopped: quota"}},
		"described": {api.SearchStatus{Enabled: true, LastScan: time.Now(), Enriched: 3, EnrichError: "rate limited"}, []string{"3 items have a summary.", "Describing files stopped: rate limited"}},
	} {
		t.Run(name, func(t *testing.T) {
			out := captureStdout(t, func() { PrintSearchStatus(os.Stdout, tc.status) })
			for _, w := range tc.want {
				assert.Contains(t, out, w)
			}
		})
	}
}

// statusOnly is a backend that only answers SearchStatus.
type statusOnly struct {
	api.Backend
	status api.SearchStatus
}

func (s statusOnly) SearchStatus() api.SearchStatus { return s.status }

// WaitIndexed returns once a scan has finished, and otherwise says why it
// stopped waiting: search off, the time up, the caller gone.
func TestWaitIndexed(t *testing.T) {
	done, cancelled := context.Background(), func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}()
	for name, tc := range map[string]struct {
		ctx    context.Context
		status api.SearchStatus
		since  time.Time
		limit  time.Duration
		want   error
		text   string
	}{
		"scanned":       {ctx: done, status: api.SearchStatus{Enabled: true, LastScan: time.Now()}, limit: time.Second},
		"off":           {ctx: done, status: api.SearchStatus{}, limit: time.Second, want: api.ErrSearchDisabled},
		"time up":       {ctx: done, status: api.SearchStatus{Enabled: true, Scanning: true}, limit: 0, text: "still being indexed"},
		"an older scan": {ctx: done, status: api.SearchStatus{Enabled: true, LastScan: time.Now().Add(-time.Hour)}, since: time.Now(), limit: 0, text: "still being indexed"},
		"cancelled":     {ctx: cancelled, status: api.SearchStatus{Enabled: true, Scanning: true}, limit: time.Minute, want: context.Canceled},
	} {
		t.Run(name, func(t *testing.T) {
			err := WaitIndexed(tc.ctx, statusOnly{status: tc.status}, tc.since, tc.limit)
			switch {
			case tc.want != nil:
				assert.ErrorIs(t, err, tc.want)
			case tc.text != "":
				assert.ErrorContains(t, err, tc.text)
			default:
				assert.NoError(t, err)
			}
		})
	}
}

// With search off, /search and /search reindex say so.
func TestSlashSearchOff(t *testing.T) {
	isolateHome(t)
	cfg := config.DefaultConfig()
	cfg.Tools.WorkspaceDir = t.TempDir()
	cfg.Search.Enabled = false
	app := openAppWith(t, cfg, engine.Options{Model: runtime.NewMockLLM("mock-a")})
	for args, want := range map[string]string{
		"zeppelin": "workspace search is off",
		"reindex":  "workspace search is off",
	} {
		out := captureStdout(t, func() { prepareSearch(context.Background(), app, args, nil) })
		assert.Contains(t, out, want, args)
	}
}

// A search that finds nothing while the index is still being built says
// so.
func TestSlashSearchWhileIndexing(t *testing.T) {
	app := newTestApp(t, nil)
	app.Workspace = scanningBackend{Backend: app.Workspace}
	out := captureStdout(t, func() { prepareSearch(context.Background(), app, "zeppelin", nil) })
	assert.Contains(t, out, "still being indexed")
}

// scanningBackend finds nothing, its index still scanning.
type scanningBackend struct{ api.Backend }

func (scanningBackend) Search(context.Context, api.SearchQuery) (api.SearchResult, error) {
	return api.SearchResult{}, nil
}

func (scanningBackend) SearchStatus() api.SearchStatus {
	return api.SearchStatus{Enabled: true, Scanning: true}
}
