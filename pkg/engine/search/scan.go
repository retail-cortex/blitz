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

package search

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Chunks are about this many lines or bytes, so a hit lands near its line
// and a long file ranks by its best part.
const (
	chunkLines = 60
	chunkBytes = 2048
	// maxLine cuts a very long line (minified code) in the index.
	maxLine = 4096
)

// batchSize is how many changed items go in one transaction.
const batchSize = 100

// unreadableHash marks an item its source couldn't read: kept, without
// text, so it isn't read again until it changes (a PDF that takes the
// whole extraction timeout would otherwise cost that on every scan).
const unreadableHash = "!unreadable"

// known is what the index holds of an item.
type known struct {
	id     int64
	size   int64
	mtime  int64
	hash   string
	hidden bool
}

// Scan brings the index up to date with every source: changed items are
// read again, new ones added, gone or no longer allowed ones dropped.
// Unchanged items (same size and time) aren't read. One scan runs at a
// time; ctx stops it between items.
func (x *Index) Scan(ctx context.Context) error {
	x.scanMu.Lock()
	defer x.scanMu.Unlock()
	x.setStatus(func(s *Status) { s.Scanning = true })
	var errs []error
	for _, src := range x.sources {
		if err := x.scanSource(ctx, src); err != nil {
			errs = append(errs, err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	var embedErr, enrichErr error
	if x.embedder != nil && ctx.Err() == nil {
		embedErr = x.embedPending(ctx)
	}
	if x.enricher != nil && ctx.Err() == nil {
		enrichErr = x.enrichPending(ctx)
	}
	err := errors.Join(errs...)
	finished := time.Now()
	if ctx.Err() == nil { // kept, so the next process knows how fresh the index is
		_, _ = x.db.Exec(`INSERT INTO meta(key, value) VALUES('last_scan', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, strconv.FormatInt(finished.UnixNano(), 10))
	}
	counts, _ := x.counts()
	unreadable, _ := x.unreadable()
	embedded, _ := x.embedded()
	enriched, _ := x.enriched()
	x.setStatus(func(s *Status) {
		s.Enriched = enriched
		s.EnrichErr = ""
		if enrichErr != nil {
			s.EnrichErr = enrichErr.Error()
		}
		s.Scanning = false
		s.Items = counts
		s.Unreadable = unreadable
		s.Embedded = embedded
		s.EmbedErr = ""
		if embedErr != nil {
			s.EmbedErr = embedErr.Error()
		}
		s.Err = ""
		if err != nil {
			s.Err = err.Error()
		}
		if ctx.Err() == nil {
			s.LastScan = finished
		}
	})
	return err
}

func (x *Index) setStatus(f func(*Status)) {
	x.mu.Lock()
	defer x.mu.Unlock()
	f(&x.status)
}

// scanSource scans one source.
func (x *Index) scanSource(ctx context.Context, src Source) error {
	name := src.Name()
	items, err := src.List(ctx)
	if err != nil {
		return err
	}
	have, err := x.known(name)
	if err != nil {
		return err
	}
	b := &batch{x: x}
	defer b.rollback()
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		if ctx.Err() != nil {
			return b.commit()
		}
		if x.allowed != nil && !x.allowed(name, it.Ref) {
			continue // dropped below, if it was indexed
		}
		seen[it.Ref] = true
		k, ok := have[it.Ref]
		if ok && k.size == it.Size && k.mtime == it.Modified.UnixNano() {
			if k.hidden != it.Hidden { // a .gitignore changed, not the file
				tx, err := b.tx()
				if err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE docs SET hidden = ? WHERE id = ?`, it.Hidden, k.id); err != nil {
					return err
				}
				if err := b.done(); err != nil {
					return err
				}
			}
			continue
		}
		doc, err := src.Read(ctx, it.Ref)
		if errors.Is(err, ErrSkip) {
			delete(seen, it.Ref)
			continue
		}
		hash := digest(doc)
		if err != nil { // remembered, so it isn't read again until it changes
			doc, hash = Doc{Title: it.Ref}, unreadableHash
		}
		tx, err := b.tx()
		if err != nil {
			return err
		}
		if ok && k.hash == hash { // touched, not changed
			if _, err := tx.Exec(`UPDATE docs SET size = ?, mtime = ?, hidden = ? WHERE id = ?`, it.Size, it.Modified.UnixNano(), it.Hidden, k.id); err != nil {
				return err
			}
		} else if err := put(tx, name, it, doc, hash, k.id); err != nil {
			return err
		}
		if err := b.done(); err != nil {
			return err
		}
	}
	for ref, k := range have {
		if seen[ref] {
			continue
		}
		tx, err := b.tx()
		if err != nil {
			return err
		}
		if err := drop(tx, k.id); err != nil {
			return err
		}
		if err := b.done(); err != nil {
			return err
		}
	}
	return b.commit()
}

// known is what the index holds of source's items, by reference.
func (x *Index) known(source string) (map[string]known, error) {
	rows, err := x.db.Query(`SELECT id, ref, size, mtime, hash, hidden FROM docs WHERE source = ?`, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]known{}
	for rows.Next() {
		var ref string
		var k known
		if err := rows.Scan(&k.id, &ref, &k.size, &k.mtime, &k.hash, &k.hidden); err != nil {
			return nil, err
		}
		out[ref] = k
	}
	return out, rows.Err()
}

// batch groups changes into transactions of batchSize.
type batch struct {
	x  *Index
	t  *sql.Tx
	n  int
	ok bool
}

func (b *batch) tx() (*sql.Tx, error) {
	if b.t == nil {
		t, err := b.x.db.Begin()
		if err != nil {
			return nil, err
		}
		b.t, b.n = t, 0
	}
	return b.t, nil
}

// done counts a change, committing every batchSize.
func (b *batch) done() error {
	if b.n++; b.n >= batchSize {
		return b.commit()
	}
	return nil
}

func (b *batch) commit() error {
	if b.t == nil {
		return nil
	}
	err := b.t.Commit()
	b.t = nil
	return err
}

func (b *batch) rollback() {
	if b.t != nil {
		b.t.Rollback()
		b.t = nil
	}
}

// put stores an item's document in place of what id held (0: new).
func put(tx *sql.Tx, source string, it Item, doc Doc, hash string, id int64) error {
	now := time.Now().UnixNano()
	if id != 0 {
		if err := dropChunks(tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE docs SET title = ?, size = ?, mtime = ?, hash = ?, indexed = ?, hidden = ? WHERE id = ?`,
			doc.Title, it.Size, it.Modified.UnixNano(), hash, now, it.Hidden, id); err != nil {
			return err
		}
	} else {
		res, err := tx.Exec(`INSERT INTO docs(source, ref, title, size, mtime, hash, indexed, hidden) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
			source, it.Ref, doc.Title, it.Size, it.Modified.UnixNano(), hash, now, it.Hidden)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
	}
	for _, c := range chunk(doc) {
		res, err := tx.Exec(`INSERT INTO chunks(doc, start_line, section) VALUES(?, ?, ?)`, id, c.line, c.section)
		if err != nil {
			return err
		}
		cid, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO words(rowid, title, body) VALUES(?, ?, ?)`, cid, doc.Title, c.text); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO grams(rowid, body) VALUES(?, ?)`, cid, c.text); err != nil {
			return err
		}
	}
	return nil
}

// drop removes a document and its chunks.
func drop(tx *sql.Tx, id int64) error {
	if err := dropChunks(tx, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM about WHERE rowid = ?`, id); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM docs WHERE id = ?`, id)
	return err
}

func dropChunks(tx *sql.Tx, id int64) error {
	for _, q := range []string{
		// Their vectors go at the next embedding pass.
		`INSERT OR IGNORE INTO stale(chunk, source) SELECT v.chunk, v.source FROM vectors v JOIN chunks c ON c.id = v.chunk WHERE c.doc = ?`,
		`DELETE FROM vectors WHERE chunk IN (SELECT id FROM chunks WHERE doc = ?)`,
		`DELETE FROM words WHERE rowid IN (SELECT id FROM chunks WHERE doc = ?)`,
		`DELETE FROM grams WHERE rowid IN (SELECT id FROM chunks WHERE doc = ?)`,
		`DELETE FROM chunks WHERE doc = ?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return nil
}

// digest identifies a document's content, so a file touched but not
// changed isn't indexed again.
func digest(doc Doc) string {
	h := sha256.New()
	h.Write([]byte(doc.Title))
	h.Write([]byte{0})
	h.Write([]byte(doc.Text))
	return hex.EncodeToString(h.Sum(nil))
}

// piece is one chunk of a document: its text, first line and section.
type piece struct {
	text    string
	line    int
	section string
}

// chunk splits a document into pieces of about chunkLines lines or
// chunkBytes bytes, never across a section's start.
func chunk(doc Doc) []piece {
	lines := strings.Split(doc.Text, "\n")
	label := func(line int) string { // the section line falls in
		s := ""
		for _, sec := range doc.Sections {
			if sec.Line <= line {
				s = sec.Label
			}
		}
		return s
	}
	starts := map[int]bool{}
	for _, sec := range doc.Sections {
		starts[sec.Line] = true
	}
	var out []piece
	var b strings.Builder
	first, n := 1, 0
	flush := func(next int) {
		if n > 0 && strings.TrimSpace(b.String()) != "" {
			out = append(out, piece{text: b.String(), line: first, section: label(first)})
		}
		b.Reset()
		first, n = next, 0
	}
	for i, l := range lines {
		line := i + 1
		if n > 0 && (starts[line] || n >= chunkLines || b.Len() >= chunkBytes) {
			flush(line)
		}
		if len(l) > maxLine {
			l = l[:maxLine]
		}
		if n > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l)
		n++
	}
	flush(0)
	return out
}
