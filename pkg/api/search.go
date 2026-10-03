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

package api

import (
	"errors"
	"time"
)

// The sources workspace search looks in.
const (
	// SearchFiles are the workspace's text files (.gitignore and blocked
	// paths left out).
	SearchFiles = "files"
	// SearchDocuments are the text of its PDFs and notebooks.
	SearchDocuments = "documents"
	// SearchChats are its chats.
	SearchChats = "chats"
	// SearchNotes are its notes (remember) and plans (.blitz/plans).
	SearchNotes = "notes"
)

// SearchSources are every source, in order.
var SearchSources = []string{SearchFiles, SearchDocuments, SearchChats, SearchNotes}

// The ways to search.
const (
	// SearchKeyword ranks by the words (BM25), the default without an
	// embedding model.
	SearchKeyword = "keyword"
	// SearchSemantic ranks by meaning (embeddings).
	SearchSemantic = "semantic"
	// SearchHybrid fuses both, the default with an embedding model.
	SearchHybrid = "hybrid"
)

// SearchQuery is a workspace search: words and "quoted phrases", in the
// sources named (none: search.sources), up to Limit hits (0: 20), by
// Mode ("": hybrid with an embedding model, else keyword).
type SearchQuery struct {
	Text    string
	Sources []string
	Limit   int
	Mode    string
	// Hidden includes hidden items: dotfiles and dot folders, and files git
	// ignores (search.include_ignored). The desktop app asks for them when
	// it shows hidden files.
	Hidden bool
}

// SearchHit is one item found. Ref is a path relative to the workspace
// (files, documents, plans), a session's ID (chats) or a note's name;
// Line is the passage's line in the item's text (1-based) and Section
// labels it (a PDF's page, a notebook's cell).
type SearchHit struct {
	Source  string
	Ref     string
	Title   string
	Line    int
	Section string
	Snippet string
	Score   float64
	// Summary and Tags are what a model wrote of the item
	// (search.enrich), if it has.
	Summary string
	Tags    []string
}

// SearchResult is a search's hits, best first, and the sources searched.
type SearchResult struct {
	Hits    []SearchHit
	Sources []string
}

// SearchSettings are the [search] settings a workspace follows.
type SearchSettings struct {
	Enabled          bool
	Sources          []string
	EmbeddingModel   string
	Enrich           bool
	EnrichModel      string
	EnrichDailyLimit int
	IncludeIgnored   bool
}

// SearchStatus is how a workspace's search index is: items per source,
// whether a scan runs, when the last one finished, how many items it
// couldn't read, and its error.
type SearchStatus struct {
	Enabled    bool
	Items      map[string]int
	Scanning   bool
	LastScan   time.Time
	Unreadable int
	Error      string
	// EmbeddingModel is semantic search's model ("": off); Embedded counts
	// the chunks with vectors, and EmbedError is why embedding stopped.
	EmbeddingModel string
	Embedded       int
	EmbedError     string
	// Enriched counts the items with a summary (search.enrich), and
	// EnrichError is why describing stopped.
	Enriched    int
	EnrichError string
	// Settings are the [search] settings followed (even with search off);
	// Problem is what kept them from applying in full (an embedding model
	// or describer that couldn't be built, an index that couldn't open).
	Settings SearchSettings
	Problem  string
}

// ErrSearchDisabled reports workspace search turned off.
var ErrSearchDisabled = errors.New("workspace search is off (search.enabled = false)")

// ErrEmptySearch reports a query with nothing to search for.
var ErrEmptySearch = errors.New("nothing to search for")

// ErrNoEmbeddings reports a semantic or hybrid search with no embedding
// model set.
var ErrNoEmbeddings = errors.New("semantic search needs an embedding model (search.embedding_model)")

// ErrUnknownSearchMode reports a mode that isn't keyword, semantic or
// hybrid.
var ErrUnknownSearchMode = errors.New("unknown search mode (keyword, semantic or hybrid)")

// ErrUnknownSearchSource reports a source search doesn't have.
var ErrUnknownSearchSource = errors.New("unknown search source (files, documents, chats or notes)")
