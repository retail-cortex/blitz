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
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/engine/table"
)

// maxEnrichBytes is the largest file described: past it, a summary costs
// a lot and says little.
const maxEnrichBytes = 64 * 1024

// enrichBatch is how many candidates a pass looks at a time.
const enrichBatch = 20

// generatedDirs and generatedSuffixes mark vendored and generated files,
// which aren't described.
var (
	generatedDirs     = []string{"vendor", "node_modules", "third_party", "dist", "build", "generated", "gen"}
	generatedSuffixes = []string{".pb.go", "_pb.go", ".pb.ts", "_pb.ts", ".min.js", ".min.css", ".lock", "-lock.json", ".sum"}
)

// generated reports whether a file is vendored or generated, by its path
// or its first line's "Code generated … DO NOT EDIT".
func generated(ref, head string) bool {
	for _, seg := range strings.Split(path.Dir(ref), "/") {
		for _, d := range generatedDirs {
			if seg == d {
				return true
			}
		}
	}
	for _, s := range generatedSuffixes {
		if strings.HasSuffix(ref, s) {
			return true
		}
	}
	first, _, _ := strings.Cut(head, "\n")
	return strings.Contains(first, "Code generated") && strings.Contains(first, "DO NOT EDIT")
}

// enriched is how many items have a summary.
func (x *Index) enriched() (int, error) {
	var n int
	err := x.db.QueryRow(`SELECT count(*) FROM docs WHERE summary != ''`).Scan(&n)
	return n, err
}

// budget is how many files may still be described today, the day's count
// kept in the index so a restart doesn't reset it.
func (x *Index) budget() (day string, used int) {
	day = time.Now().Format(time.DateOnly)
	var d, n string
	_ = x.db.QueryRow(`SELECT value FROM meta WHERE key = 'enrich_day'`).Scan(&d)
	_ = x.db.QueryRow(`SELECT value FROM meta WHERE key = 'enrich_count'`).Scan(&n)
	if d == day {
		used, _ = strconv.Atoi(n)
	}
	return day, used
}

func (x *Index) spend(day string, used int) error {
	for k, v := range map[string]string{"enrich_day": day, "enrich_count": strconv.Itoa(used)} {
		if _, err := x.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return nil
}

// enrichPending describes the files and documents whose summary is
// missing or out of date, newest first, until the day's budget is spent,
// a turn starts, ctx ends or the model fails. Large, generated and
// unreadable items are marked done without a call.
func (x *Index) enrichPending(ctx context.Context) error {
	day, used := x.budget()
	for ctx.Err() == nil && used < x.enrichPerDay {
		if x.paused != nil && x.paused() {
			return nil
		}
		rows, err := x.db.QueryContext(ctx, `SELECT id, ref, title, size, hash FROM docs
			WHERE source IN ('files', 'documents') AND meta_hash != hash
			ORDER BY indexed DESC LIMIT ?`, enrichBatch)
		if err != nil {
			return err
		}
		type item struct {
			id               int64
			ref, title, hash string
			size             int64
		}
		var todo []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.id, &it.ref, &it.title, &it.size, &it.hash); err != nil {
				rows.Close()
				return err
			}
			todo = append(todo, it)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		if len(todo) == 0 {
			return nil
		}
		for _, it := range todo {
			if ctx.Err() != nil || used >= x.enrichPerDay || (x.paused != nil && x.paused()) {
				return ctx.Err()
			}
			text, err := x.docText(ctx, it.id)
			if err != nil {
				return err
			}
			large := it.size > maxEnrichBytes && !table.IsTable(it.ref) // a table is described by its head
			if it.hash == unreadableHash || large || generated(it.ref, text) || strings.TrimSpace(text) == "" {
				if err := x.setAbout(it.id, it.hash, Description{}); err != nil {
					return err
				}
				continue
			}
			d, err := x.enricher.Describe(ctx, it.title, text)
			if err != nil {
				return err
			}
			used++
			if err := errors.Join(x.setAbout(it.id, it.hash, d), x.spend(day, used)); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

// docText is a document's text, its chunks in order.
func (x *Index) docText(ctx context.Context, id int64) (string, error) {
	rows, err := x.db.QueryContext(ctx, `SELECT w.body FROM chunks c JOIN words w ON w.rowid = c.id WHERE c.doc = ? ORDER BY c.id`, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return "", err
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(body)
	}
	return b.String(), rows.Err()
}

// setAbout records a document's description as of hash (empty: done
// without one).
func (x *Index) setAbout(id int64, hash string, d Description) error {
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	joined := strings.Join(d.Tags, ", ")
	var cols []byte
	var colText []string
	if len(d.Columns) > 0 {
		if cols, err = json.Marshal(d.Columns); err != nil {
			return err
		}
		for _, c := range d.Columns {
			colText = append(colText, c.Name+": "+c.Intent)
		}
	}
	if _, err := tx.Exec(`UPDATE docs SET summary = ?, tags = ?, columns = ?, meta_hash = ? WHERE id = ?`, d.Summary, joined, string(cols), hash, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM about WHERE rowid = ?`, id); err != nil {
		return err
	}
	if d.Summary != "" || joined != "" || len(colText) > 0 {
		if _, err := tx.Exec(`INSERT INTO about(rowid, summary, tags, columns) VALUES(?, ?, ?, ?)`, id, d.Summary, joined, strings.Join(colText, "\n")); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Columns is what a model said a table's columns hold (none: not
// described, or not a table).
func (x *Index) Columns(ctx context.Context, source, ref string) ([]ColumnNote, error) {
	var raw string
	err := x.db.QueryRowContext(ctx, `SELECT columns FROM docs WHERE source = ? AND ref = ?`, source, ref).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && raw == "") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cols []ColumnNote
	return cols, json.Unmarshal([]byte(raw), &cols)
}

// aboutMatch is the documents whose summary or tags match, as their first
// chunks, best first.
func (x *Index) aboutMatch(ctx context.Context, expr string, sources []string, limit int) ([]candidate, error) {
	in := strings.TrimSuffix(strings.Repeat("?, ", len(sources)), ", ")
	args := []any{expr}
	for _, s := range sources {
		args = append(args, s)
	}
	args = append(args, limit)
	rows, err := x.db.QueryContext(ctx, `SELECT (SELECT min(c.id) FROM chunks c WHERE c.doc = d.id)
		FROM about JOIN docs d ON d.id = about.rowid
		WHERE about MATCH ? AND d.source IN (`+in+`)
		ORDER BY bm25(about, 2.0, 1.0, 1.0) LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id *int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		if id != nil {
			ids = append(ids, *id)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return x.candidates(ctx, ids)
}

// abouts are the summaries and tags of documents, by source and ref.
func (x *Index) abouts(ctx context.Context, hits []Hit) error {
	for i := range hits {
		var summary, tags string
		err := x.db.QueryRowContext(ctx, `SELECT summary, tags FROM docs WHERE source = ? AND ref = ?`, hits[i].Source, hits[i].Ref).Scan(&summary, &tags)
		if err != nil {
			return err
		}
		hits[i].Summary = summary
		if tags != "" {
			hits[i].Tags = strings.Split(tags, ", ")
		}
	}
	return nil
}
