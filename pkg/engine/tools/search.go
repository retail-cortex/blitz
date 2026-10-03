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
	"context"
	"fmt"

	"github.com/retail-cortex/blitz/pkg/api"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Searcher searches the workspace's index (spec_search_035); the engine
// supplies it.
type Searcher func(ctx context.Context, q api.SearchQuery) (api.SearchResult, error)

// SetSearcher supplies the search_workspace tool's searcher; nil turns it
// off.
func (r *Registry) SetSearcher(s Searcher) {
	r.notesMu.Lock()
	defer r.notesMu.Unlock()
	r.workspaceSearch = s
}

func (r *Registry) getSearcher() Searcher {
	r.notesMu.Lock()
	defer r.notesMu.Unlock()
	return r.workspaceSearch
}

// SearchWorkspaceInput is what search_workspace takes.
type SearchWorkspaceInput struct {
	Query   string   `json:"query" jsonschema:"Words and \"quoted phrases\" to find; identifiers and parts of names match too"`
	Sources []string `json:"sources,omitempty" jsonschema:"Where to look: files, documents (PDFs and notebooks), chats, notes; default files and documents"`
	Limit   int      `json:"limit,omitempty" jsonschema:"Most results (default 20)"`
	Mode    string   `json:"mode,omitempty" jsonschema:"keyword, semantic (by meaning, when an embedding model is set) or hybrid; default hybrid with an embedding model, else keyword"`
}

// SearchWorkspaceHit is one item found: where (path:line for files), what
// section of it (a PDF's page), and the lines around the match.
type SearchWorkspaceHit struct {
	Source  string   `json:"source"`
	Where   string   `json:"where"`
	Title   string   `json:"title,omitempty"`
	Section string   `json:"section,omitempty"`
	Snippet string   `json:"snippet"`
	Summary string   `json:"summary,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// SearchWorkspaceOutput is search_workspace's result, best first.
type SearchWorkspaceOutput struct {
	Hits  []SearchWorkspaceHit `json:"hits"`
	Error string               `json:"error,omitempty"`
}

// NewSearchWorkspaceTool is search_workspace: ranked full-text search of
// the workspace's index, one passage per item.
func NewSearchWorkspaceTool(r *Registry) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{Name: "search_workspace", Description: "Search the whole workspace by words, ranked by relevance: its files, the text of its PDFs and notebooks, its past chats and its notes. Returns the best passage of each item with where it is. Use it to find where something is handled or discussed; use grep for every exact match of a pattern."},
		func(ctx agent.Context, in SearchWorkspaceInput) (SearchWorkspaceOutput, error) {
			search := r.getSearcher()
			if search == nil {
				return SearchWorkspaceOutput{Error: api.ErrSearchDisabled.Error()}, nil
			}
			res, err := search(ctx, api.SearchQuery{Text: in.Query, Sources: in.Sources, Limit: in.Limit, Mode: in.Mode, Hidden: true})
			if err != nil {
				return SearchWorkspaceOutput{Error: err.Error()}, nil
			}
			out := SearchWorkspaceOutput{Hits: make([]SearchWorkspaceHit, 0, len(res.Hits))}
			for _, h := range res.Hits {
				where := h.Ref
				if h.Source != api.SearchChats && h.Line > 0 {
					where = fmt.Sprintf("%s:%d", h.Ref, h.Line)
				}
				out.Hits = append(out.Hits, SearchWorkspaceHit{Source: h.Source, Where: where, Title: h.Title, Section: h.Section, Snippet: h.Snippet, Summary: h.Summary, Tags: h.Tags})
			}
			return out, nil
		})
}
