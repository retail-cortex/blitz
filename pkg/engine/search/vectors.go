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
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/philippgille/chromem-go"
)

// embedPassSize is how many chunks an embedding pass reads at a time.
const embedPassSize = 64

// ErrNoEmbeddings is returned for a semantic search when no embedding
// model is set.
var ErrNoEmbeddings = errors.New("semantic search needs an embedding model (search.embedding_model)")

// openVectors opens the vectors in dir: one chromem-go collection per
// source, files compressed. Vectors made by another model are thrown away
// (their dimensions and meaning differ), and every chunk is embedded
// again.
func (x *Index) openVectors(dir string) error {
	var model string
	_ = x.db.QueryRow(`SELECT value FROM meta WHERE key = 'embedding_model'`).Scan(&model)
	if model != x.embedder.Model() {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		for _, q := range []string{`DELETE FROM vectors`, `DELETE FROM stale`} {
			if _, err := x.db.Exec(q); err != nil {
				return err
			}
		}
		if _, err := x.db.Exec(`INSERT INTO meta(key, value) VALUES('embedding_model', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, x.embedder.Model()); err != nil {
			return err
		}
	}
	db, err := chromem.NewPersistentDB(dir, true)
	if err != nil {
		return err
	}
	x.vectors = db
	return nil
}

// embedded is how many chunks have vectors.
func (x *Index) embedded() (int, error) {
	var n int
	err := x.db.QueryRow(`SELECT count(*) FROM vectors`).Scan(&n)
	return n, err
}

// collection is source's collection of vectors, made if need be.
func (x *Index) collection(source string) (*chromem.Collection, error) {
	return x.vectors.GetOrCreateCollection(source, nil, nil)
}

// embedPending removes the vectors of chunks gone since, then embeds the
// chunks without one, a batch at a time, until none is left, ctx ends or
// the embedder fails (the next scan goes on from there).
func (x *Index) embedPending(ctx context.Context) error {
	if err := x.dropStale(ctx); err != nil {
		return err
	}
	for ctx.Err() == nil {
		rows, err := x.db.QueryContext(ctx, `SELECT c.id, d.source, d.title, w.body
			FROM chunks c JOIN docs d ON d.id = c.doc JOIN words w ON w.rowid = c.id
			WHERE c.id NOT IN (SELECT chunk FROM vectors) ORDER BY c.id LIMIT ?`, embedPassSize)
		if err != nil {
			return err
		}
		var ids []int64
		var sources, texts []string
		for rows.Next() {
			var id int64
			var source, title, body string
			if err := rows.Scan(&id, &source, &title, &body); err != nil {
				rows.Close()
				return err
			}
			ids, sources, texts = append(ids, id), append(sources, source), append(texts, title+"\n\n"+body)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		vecs, err := x.embedder.Embed(ctx, texts)
		if err != nil {
			return err
		}
		if err := x.addVectors(ctx, ids, sources, vecs); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// addVectors stores the chunks' vectors and records them.
func (x *Index) addVectors(ctx context.Context, ids []int64, sources []string, vecs [][]float32) error {
	bySource := map[string][]chromem.Document{}
	for i, id := range ids {
		bySource[sources[i]] = append(bySource[sources[i]], chromem.Document{ID: strconv.FormatInt(id, 10), Embedding: vecs[i]})
	}
	for source, docs := range bySource {
		c, err := x.collection(source)
		if err != nil {
			return err
		}
		if err := c.AddDocuments(ctx, docs, 4); err != nil {
			return err
		}
	}
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO vectors(chunk, source) VALUES(?, ?)`, id, sources[i]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// dropStale removes the vectors of chunks dropped since the last pass.
func (x *Index) dropStale(ctx context.Context) error {
	rows, err := x.db.QueryContext(ctx, `SELECT chunk, source FROM stale`)
	if err != nil {
		return err
	}
	bySource := map[string][]string{}
	for rows.Next() {
		var id int64
		var source string
		if err := rows.Scan(&id, &source); err != nil {
			rows.Close()
			return err
		}
		bySource[source] = append(bySource[source], strconv.FormatInt(id, 10))
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for source, ids := range bySource {
		if c := x.vectors.GetCollection(source, nil); c != nil {
			if err := c.Delete(ctx, nil, nil, ids...); err != nil {
				return err
			}
		}
		if _, err := x.db.ExecContext(ctx, `DELETE FROM stale WHERE source = ? AND chunk IN (`+strings.Join(ids, ",")+`)`, source); err != nil {
			return err
		}
	}
	return nil
}

// similar is the chunks most like the query's vector, across sources,
// best first.
func (x *Index) similar(ctx context.Context, text string, sources []string, limit int) ([]candidate, error) {
	vecs, err := x.embedder.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	type found struct {
		id  int64
		sim float32
	}
	var all []found
	for _, s := range sources {
		c := x.vectors.GetCollection(s, nil)
		if c == nil || c.Count() == 0 {
			continue
		}
		res, err := c.QueryEmbedding(ctx, vecs[0], min(limit, c.Count()), nil, nil)
		if err != nil {
			return nil, err
		}
		for _, r := range res {
			if id, err := strconv.ParseInt(r.ID, 10, 64); err == nil {
				all = append(all, found{id, r.Similarity})
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].sim > all[j].sim })
	ids := make([]int64, len(all))
	for i, f := range all {
		ids[i] = f.id
	}
	return x.candidates(ctx, ids)
}

// candidates are the chunks with ids, in that order (gone ones left out).
func (x *Index) candidates(ctx context.Context, ids []int64) ([]candidate, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	in := make([]string, len(ids))
	for i, id := range ids {
		in[i] = strconv.FormatInt(id, 10)
	}
	rows, err := x.db.QueryContext(ctx, `SELECT c.id, d.source, d.ref, d.title, c.start_line, c.section, w.body, d.hidden
		FROM chunks c JOIN docs d ON d.id = c.doc JOIN words w ON w.rowid = c.id
		WHERE c.id IN (`+strings.Join(in, ",")+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[int64]candidate{}
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.chunk, &c.source, &c.ref, &c.title, &c.line, &c.section, &c.body, &c.hidden); err != nil {
			return nil, err
		}
		byID[c.chunk] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]candidate, 0, len(ids))
	for _, id := range ids {
		if c, ok := byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}
