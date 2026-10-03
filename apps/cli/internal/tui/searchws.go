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
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/i18n"
	"github.com/retail-cortex/blitz/pkg/textutil"
)

// WorkspaceSearchSources are /search's (and blitz search --in's) words for
// what to search: a source, or all of them.
var WorkspaceSearchSources = map[string][]string{
	api.SearchFiles:     {api.SearchFiles},
	api.SearchDocuments: {api.SearchDocuments},
	api.SearchChats:     {api.SearchChats},
	api.SearchNotes:     {api.SearchNotes},
	"all":               api.SearchSources,
}

// workspaceSearch handles "/search [files|documents|chats|notes|all]
// <terms>", "/search status" and "/search reindex": it shows what it found
// and leads to no turn.
func workspaceSearch(ctx context.Context, app *App, sub, terms string, interrupts <-chan os.Signal) {
	switch sub {
	case "status":
		PrintSearchStatus(os.Stdout, app.Workspace.SearchStatus())
		return
	case "reindex":
		if err := app.Workspace.Reindex(); err != nil {
			fmt.Printf("%s✗ %s%s\n", Red, safe(err.Error()), Reset)
			return
		}
		fmt.Printf("%s%s%s\n", Dim, i18n.T("search.ws_reindex"), Reset)
		return
	}
	sources := WorkspaceSearchSources[sub]
	if sources == nil { // no source named: all the words are the query
		terms = strings.TrimSpace(sub + " " + terms)
	}
	sctx, stop := cancelOnSignal(ctx, interrupts)
	res, err := app.Workspace.Search(sctx, api.SearchQuery{Text: terms, Sources: sources})
	stop()
	if err != nil {
		fmt.Printf("%s✗ %s%s\n", Red, i18n.T("search.failed", "error", safe(err.Error())), Reset)
		return
	}
	if len(res.Hits) == 0 {
		fmt.Printf("%s%s%s\n", Yellow, i18n.T("search.ws_none", "query", safe(terms)), Reset)
		if st := app.Workspace.SearchStatus(); st.Scanning || st.LastScan.IsZero() {
			fmt.Printf("%s%s%s\n", Dim, i18n.T("search.ws_scanning"), Reset)
		}
		return
	}
	PrintSearchHits(os.Stdout, res.Hits)
}

// PrintSearchHits writes workspace search's hits: where each is (path:line
// for files, the title for a chat), its section, and its passage.
func PrintSearchHits(out io.Writer, hits []api.SearchHit) {
	for i, h := range hits {
		where := h.Ref
		switch {
		case h.Source == api.SearchChats:
			where = i18n.T("search.ws_chat", "title", h.Title)
		case h.Line > 0:
			where = fmt.Sprintf("%s:%d", h.Ref, h.Line)
		}
		section := ""
		if h.Section != "" {
			section = Dim + "  · " + safe(h.Section) + Reset
		}
		fmt.Fprintf(out, "  %s%d. %s%s%s\n", Bold, i+1, safe(where), Reset, section)
		if h.Summary != "" {
			fmt.Fprintf(out, "     %s%s%s\n", Cyan, safe(textutil.Ellipsize(h.Summary, 200)), Reset)
		}
		for _, l := range strings.Split(h.Snippet, "\n") {
			fmt.Fprintf(out, "     %s%s%s\n", Dim, safe(textutil.Ellipsize(l, 160)), Reset)
		}
	}
}

// PrintSearchStatus writes how the workspace's search index is.
func PrintSearchStatus(out io.Writer, s api.SearchStatus) {
	if !s.Enabled {
		fmt.Fprintln(out, api.ErrSearchDisabled.Error())
		return
	}
	var parts []string
	for _, src := range api.SearchSources {
		parts = append(parts, fmt.Sprintf("%s %d", src, s.Items[src]))
	}
	when := i18n.T("search.ws_never")
	if !s.LastScan.IsZero() {
		when = s.LastScan.Local().Format(time.DateTime)
	}
	fmt.Fprintln(out, i18n.T("search.ws_status", "items", strings.Join(parts, ", "), "when", when))
	if s.Scanning {
		fmt.Fprintln(out, i18n.T("search.ws_scanning"))
	}
	if s.Unreadable > 0 {
		fmt.Fprintln(out, i18n.N("search.ws_unreadable", s.Unreadable))
	}
	if s.EmbeddingModel != "" {
		fmt.Fprintln(out, i18n.T("search.ws_semantic", "model", s.EmbeddingModel, "chunks", s.Embedded))
	}
	if s.EmbedError != "" {
		fmt.Fprintln(out, i18n.T("search.ws_embed_error", "error", s.EmbedError))
	}
	if s.Enriched > 0 {
		fmt.Fprintln(out, i18n.N("search.ws_enriched", s.Enriched))
	}
	if s.EnrichError != "" {
		fmt.Fprintln(out, i18n.T("search.ws_enrich_error", "error", s.EnrichError))
	}
	if s.Error != "" {
		fmt.Fprintln(out, i18n.T("search.failed", "error", s.Error))
	}
}

// WaitIndexed waits until the workspace's index has finished a scan after
// since (a workspace just opened here scans it; the service's is kept
// current, so any scan will do), for up to limit or until ctx ends.
func WaitIndexed(ctx context.Context, b api.Backend, since time.Time, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for {
		s := b.SearchStatus()
		if !s.Enabled {
			return api.ErrSearchDisabled
		}
		if !s.Scanning && s.LastScan.After(since) {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New(i18n.T("search.ws_scanning"))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
