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
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// Query is what to search for: words and "quoted phrases", in which
// sources (none: all of the index's), and how (Mode).
type Query struct {
	Text    string
	Sources []string
	Limit   int
	Mode    string
	// Hidden includes hidden items (dotfiles, files git ignores).
	Hidden bool
}

// The ways to search: by the words (keyword), by meaning (semantic, with
// an embedding model), or both fused (hybrid, the default with one).
const (
	ModeKeyword  = "keyword"
	ModeSemantic = "semantic"
	ModeHybrid   = "hybrid"
)

// ErrUnknownMode is returned for a mode that isn't one of the three.
var ErrUnknownMode = errors.New("unknown search mode (keyword, semantic or hybrid)")

// Hit is one item found: where, its best passage and how well it matched.
// Line is the passage's line in the item's text (1-based); Section labels
// it (a PDF's page).
type Hit struct {
	Source  string
	Ref     string
	Title   string
	Line    int
	Section string
	Snippet string
	Score   float64
	// Summary and Tags are what a model wrote of the item (SRCH-51), if
	// it has.
	Summary string
	Tags    []string
}

// DefaultLimit is how many hits a query without a limit returns.
const DefaultLimit = 20

// ErrNoTerms is returned for a query with nothing to search for.
var ErrNoTerms = errors.New("nothing to search for")

// rrfK damps reciprocal rank fusion: a hit's score is the sum over the
// lists it's in of 1/(rrfK + rank).
const rrfK = 60

var (
	termRE   = regexp.MustCompile(`"([^"]*)"|(\S+)`)
	codeLike = regexp.MustCompile(`[._:/\\()\[\]<>]|[a-z][A-Z]|^[A-Za-z_]\w*$`)
)

// terms splits text into words and "quoted phrases".
func terms(text string) []string {
	var out []string
	for _, m := range termRE.FindAllStringSubmatch(text, -1) {
		t := m[1] + m[2]
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// quote is t as an FTS5 string, which matches it as a phrase.
func quote(t string) string { return `"` + strings.ReplaceAll(t, `"`, `""`) + `"` }

// wordsMatch is the FTS5 query for the words table: every term, the last
// word also as a prefix (so a word being typed matches).
func wordsMatch(ts []string, last bool) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = quote(t)
		if last && i == len(ts)-1 && !strings.Contains(t, " ") {
			parts[i] += "*"
		}
	}
	return strings.Join(parts, " AND ")
}

// gramsMatch is the FTS5 query for the trigram table: every term of three
// characters or more, as a substring ("" when there's none).
func gramsMatch(ts []string) string {
	var parts []string
	for _, t := range ts {
		if utf8.RuneCountInString(t) >= 3 {
			parts = append(parts, quote(t))
		}
	}
	return strings.Join(parts, " AND ")
}

// candidate is a chunk a list found.
type candidate struct {
	chunk   int64
	source  string
	ref     string
	title   string
	line    int
	section string
	body    string
	hidden  bool
}

// Search finds the items that best match q: words ranked by BM25, fused
// with substring matches (for identifiers and paths) by reciprocal rank,
// the best passage of each item once. Items the index may no longer show
// are left out.
func (x *Index) Search(ctx context.Context, q Query) ([]Hit, error) {
	ts := terms(q.Text)
	if len(ts) == 0 {
		return nil, ErrNoTerms
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	sources := q.Sources
	if len(sources) == 0 {
		sources = x.Sources()
	}
	mode := q.Mode
	switch mode {
	case "":
		mode = ModeKeyword
		if x.embedder != nil {
			mode = ModeHybrid
		}
	case ModeKeyword, ModeSemantic, ModeHybrid:
	default:
		return nil, ErrUnknownMode
	}
	if mode != ModeKeyword && x.embedder == nil {
		return nil, ErrNoEmbeddings
	}
	want := limit * 4 // room for the fusion and one hit per item
	var lists [][]candidate
	if mode != ModeSemantic {
		words, err := x.match(ctx, "words", wordsMatch(ts, true), "bm25(words, 4.0, 1.0)", sources, want)
		if err != nil {
			return nil, err
		}
		lists = append(lists, words)
		about, err := x.aboutMatch(ctx, wordsMatch(ts, false), sources, want)
		if err != nil {
			return nil, err
		}
		if len(about) > 0 {
			lists = append(lists, about)
		}
		if g := gramsMatch(ts); g != "" && codeLike.MatchString(q.Text) {
			grams, err := x.match(ctx, "grams", g, "bm25(grams)", sources, want)
			if err != nil {
				return nil, err
			}
			lists = append(lists, grams)
		}
	}
	if mode != ModeKeyword {
		near, err := x.similar(ctx, q.Text, sources, want)
		if err != nil {
			return nil, err
		}
		lists = append(lists, near)
	}
	hits := x.fuse(lists, ts, limit, q.Hidden)
	if err := x.abouts(ctx, hits); err != nil {
		return nil, err
	}
	return hits, nil
}

// match runs one FTS5 query over table, best first.
func (x *Index) match(ctx context.Context, table, expr, rank string, sources []string, limit int) ([]candidate, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	in := strings.TrimSuffix(strings.Repeat("?, ", len(sources)), ", ")
	args := []any{expr}
	for _, s := range sources {
		args = append(args, s)
	}
	args = append(args, limit)
	rows, err := x.db.QueryContext(ctx, `SELECT c.id, d.source, d.ref, d.title, c.start_line, c.section, `+table+`.body, d.hidden
		FROM `+table+` JOIN chunks c ON c.id = `+table+`.rowid JOIN docs d ON d.id = c.doc
		WHERE `+table+` MATCH ? AND d.source IN (`+in+`)
		ORDER BY `+rank+` LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.chunk, &c.source, &c.ref, &c.title, &c.line, &c.section, &c.body, &c.hidden); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// fuse merges ranked lists by reciprocal rank, keeps each item's best
// chunk, drops what may not be shown, and makes the hits.
func (x *Index) fuse(lists [][]candidate, ts []string, limit int, hidden bool) []Hit {
	score := map[int64]float64{}
	byChunk := map[int64]candidate{}
	for _, list := range lists {
		for rank, c := range list {
			score[c.chunk] += 1 / float64(rrfK+rank+1)
			byChunk[c.chunk] = c
		}
	}
	chunks := make([]int64, 0, len(score))
	for id := range score {
		chunks = append(chunks, id)
	}
	sort.Slice(chunks, func(i, j int) bool {
		if score[chunks[i]] != score[chunks[j]] {
			return score[chunks[i]] > score[chunks[j]]
		}
		return chunks[i] < chunks[j]
	})
	var hits []Hit
	seen := map[[2]string]bool{}
	for _, id := range chunks {
		c := byChunk[id]
		key := [2]string{c.source, c.ref}
		if seen[key] || (c.hidden && !hidden) || (x.allowed != nil && !x.allowed(c.source, c.ref)) {
			continue
		}
		seen[key] = true
		line, snippet := passage(c.body, ts)
		hits = append(hits, Hit{Source: c.source, Ref: c.ref, Title: c.title, Line: c.line + line, Section: c.section, Snippet: snippet, Score: score[id]})
		if len(hits) == limit {
			break
		}
	}
	return hits
}

// snippetWidth is the most runes a snippet's line keeps.
const snippetWidth = 240

// passage finds the first line of body with one of the terms and returns
// its offset and the lines around it, each cut to snippetWidth.
func passage(body string, ts []string) (int, string) {
	lines := strings.Split(body, "\n")
	lower := make([]string, len(ts))
	for i, t := range ts {
		lower[i] = strings.ToLower(t)
	}
	at := 0
	for i, l := range lines {
		ll := strings.ToLower(l)
		if slices.ContainsFunc(lower, func(t string) bool { return strings.Contains(ll, stem(t)) }) {
			at = i
			break
		}
	}
	from, to := max(at-1, 0), min(at+2, len(lines))
	out := make([]string, 0, to-from)
	for _, l := range lines[from:to] {
		l = strings.TrimRight(l, " \t\r")
		if utf8.RuneCountInString(l) > snippetWidth {
			l = string([]rune(l)[:snippetWidth]) + "…"
		}
		out = append(out, l)
	}
	return at, strings.Join(out, "\n")
}

// stem is a term short enough to be found in the words a stemmer matched
// it to ("indexing" finds "indexed").
func stem(t string) string {
	r := []rune(t)
	if len(r) > 5 {
		return string(r[:len(r)-3])
	}
	return t
}
