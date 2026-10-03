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

// Package search is workspace search: an index of what a workspace holds
// (its files, the text of its PDFs and notebooks, its chats, its notes),
// kept on disk per workspace, brought up to date by scans that read only
// what changed, and searched by words (SQLite FTS5, ranked by BM25) and by
// substrings for code (spec_search_035). The engine hands it the sources
// and says which items may be shown; it knows nothing else of the engine.
package search

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/philippgille/chromem-go"
	_ "modernc.org/sqlite" // the database/sql driver "sqlite", in pure Go
)

// Source is one kind of thing to search: files, documents, chats, notes.
type Source interface {
	// Name is the source's name in queries and results.
	Name() string
	// List is the source's items as they are now, without their text.
	List(ctx context.Context) ([]Item, error)
	// Read is an item's text; ErrSkip leaves it out of the index.
	Read(ctx context.Context, ref string) (Doc, error)
}

// Item is one thing a source holds: its reference (a path relative to the
// workspace, a session's ID), what tells a change, and whether it's hidden
// (a dotfile, a file git ignores: shown only when asked for).
type Item struct {
	Ref      string
	Size     int64
	Modified time.Time
	Hidden   bool
}

// Doc is an item's searchable text, with its title and the sections it
// falls into (a PDF's pages, a notebook's cells).
type Doc struct {
	Title    string
	Text     string
	Sections []Section
}

// Section labels the text from Line (1-based) to the next section.
type Section struct {
	Line  int
	Label string
}

// ErrSkip is what Read returns for an item that isn't searchable (binary,
// too large): it's left out, and dropped if it was indexed.
var ErrSkip = errors.New("not searchable")

// Allowed reports whether a source's item may be indexed and shown (a
// blocked path may not).
type Allowed func(source, ref string) bool

// schemaVersion changes when the tables do; an index of another version
// is rebuilt.
const schemaVersion = "4"

const schema = `
CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS docs(
	id INTEGER PRIMARY KEY,
	source TEXT NOT NULL,
	ref TEXT NOT NULL,
	title TEXT NOT NULL,
	size INTEGER NOT NULL,
	mtime INTEGER NOT NULL,
	hash TEXT NOT NULL,
	indexed INTEGER NOT NULL,
	summary TEXT NOT NULL DEFAULT '',
	tags TEXT NOT NULL DEFAULT '',
	meta_hash TEXT NOT NULL DEFAULT '',
	hidden INTEGER NOT NULL DEFAULT 0,
	columns TEXT NOT NULL DEFAULT '',
	UNIQUE(source, ref)
);
CREATE TABLE IF NOT EXISTS chunks(
	id INTEGER PRIMARY KEY,
	doc INTEGER NOT NULL,
	start_line INTEGER NOT NULL,
	section TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS chunks_doc ON chunks(doc);
CREATE VIRTUAL TABLE IF NOT EXISTS words USING fts5(title, body, tokenize = 'porter unicode61');
CREATE VIRTUAL TABLE IF NOT EXISTS grams USING fts5(body, tokenize = 'trigram');
CREATE VIRTUAL TABLE IF NOT EXISTS about USING fts5(summary, tags, columns, tokenize = 'porter unicode61');
CREATE TABLE IF NOT EXISTS vectors(chunk INTEGER PRIMARY KEY, source TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS stale(chunk INTEGER PRIMARY KEY, source TEXT NOT NULL);
`

// Embedder turns texts into vectors: a provider's embedding model, for
// semantic search (SRCH-50).
type Embedder interface {
	// Model names the embedding model; vectors of another model are
	// thrown away.
	Model() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Option changes how Open opens an index.
type Option func(*Index)

// Enricher describes a file: a summary and tags a model writes, and for a
// table what each column holds, for search to find it by what it's about
// (SRCH-60).
type Enricher interface {
	Describe(ctx context.Context, title, text string) (Description, error)
}

// Description is what an Enricher wrote of an item.
type Description struct {
	Summary string
	Tags    []string
	// Columns are a table's columns and what each holds.
	Columns []ColumnNote
}

// ColumnNote is a table column and what it holds ("water temperature in
// °C"), as a model read it.
type ColumnNote struct {
	Name   string `json:"name"`
	Intent string `json:"intent"`
}

// WithEnricher has e describe up to perDay files and documents a day,
// after each scan, while paused reports false (no turn runs).
func WithEnricher(e Enricher, perDay int, paused func() bool) Option {
	return func(x *Index) { x.enricher, x.enrichPerDay, x.paused = e, perDay, paused }
}

// WithEmbedder adds semantic search: chunks are embedded by e, after each
// scan, into vectors kept beside the index.
func WithEmbedder(e Embedder) Option { return func(x *Index) { x.embedder = e } }

// Index is one workspace's search index.
type Index struct {
	db      *sql.DB
	sources []Source
	allowed Allowed

	// embedder and vectors are semantic search (nil: keywords only).
	embedder Embedder
	vectors  *chromem.DB
	// enricher describes files, enrichPerDay a day, unless paused.
	enricher     Enricher
	enrichPerDay int
	paused       func() bool

	scanMu sync.Mutex // one scan at a time

	mu     sync.Mutex
	status Status
}

// Status is how the index is: items per source, whether a scan runs, and
// how the last one went.
type Status struct {
	Items    map[string]int
	Scanning bool
	LastScan time.Time
	// EmbeddingModel is the model semantic search uses ("": none);
	// Embedded counts the chunks it has vectors for, and EmbedErr is why
	// the last pass stopped.
	EmbeddingModel string
	Embedded       int
	EmbedErr       string
	// Enriched counts the items with a summary; EnrichErr is why the last
	// pass stopped.
	Enriched  int
	EnrichErr string
	// Unreadable counts items that couldn't be read (they're read again
	// once they change).
	Unreadable int
	Err        string
}

// Open opens (or makes) the index in dir for sources; allowed filters what
// is indexed and shown. An index of another version is rebuilt.
func Open(dir string, sources []Source, allowed Allowed, opts ...Option) (*Index, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "index.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	x := &Index{db: db, sources: sources, allowed: allowed}
	for _, o := range opts {
		o(x)
	}
	if err := x.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("the search index %s: %w", path, err)
	}
	if x.embedder != nil {
		if err := x.openVectors(filepath.Join(dir, "vectors")); err != nil {
			db.Close()
			return nil, fmt.Errorf("the search index's vectors: %w", err)
		}
		x.status.EmbeddingModel = x.embedder.Model()
	}
	x.status.Items, _ = x.counts()
	x.status.Unreadable, _ = x.unreadable()
	x.status.Embedded, _ = x.embedded()
	x.status.Enriched, _ = x.enriched()
	var last string
	if x.db.QueryRow(`SELECT value FROM meta WHERE key = 'last_scan'`).Scan(&last) == nil {
		if n, err := strconv.ParseInt(last, 10, 64); err == nil {
			x.status.LastScan = time.Unix(0, n)
		}
	}
	return x, nil
}

// migrate makes the tables, rebuilding them for an index of another
// version.
func (x *Index) migrate() error {
	var v string
	err := x.db.QueryRow(`SELECT value FROM meta WHERE key = 'version'`).Scan(&v)
	if err == nil && v == schemaVersion {
		return nil
	}
	for _, t := range []string{"meta", "docs", "chunks", "words", "grams", "about", "vectors", "stale"} {
		if _, err := x.db.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
			return err
		}
	}
	if _, err := x.db.Exec(schema); err != nil {
		return err
	}
	_, err = x.db.Exec(`INSERT INTO meta(key, value) VALUES('version', ?)`, schemaVersion)
	return err
}

// Close closes the index; a scan running is the caller's to stop first.
func (x *Index) Close() error { return x.db.Close() }

// Status is the index's status now.
func (x *Index) Status() Status {
	x.mu.Lock()
	defer x.mu.Unlock()
	s := x.status
	s.Items = maps.Clone(x.status.Items)
	return s
}

// Sources are the names of the index's sources, in order.
func (x *Index) Sources() []string {
	names := make([]string, len(x.sources))
	for i, s := range x.sources {
		names[i] = s.Name()
	}
	return names
}

// unreadable is how many items the index holds that couldn't be read.
func (x *Index) unreadable() (int, error) {
	var n int
	err := x.db.QueryRow(`SELECT count(*) FROM docs WHERE hash = ?`, unreadableHash).Scan(&n)
	return n, err
}

// counts is how many items each source has in the index (readable ones).
func (x *Index) counts() (map[string]int, error) {
	out := map[string]int{}
	rows, err := x.db.Query(`SELECT source, count(*) FROM docs WHERE hash != ? GROUP BY source`, unreadableHash)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return out, err
		}
		out[s] = n
	}
	return out, rows.Err()
}
