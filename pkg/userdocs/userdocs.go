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

// Package userdocs reads and searches the user docs the programs carry
// (spec_067_cli_parity CLP-21, CLP-22, CLP-23): pages split into sections
// at their headings, found by words, each with its address on the site.
package userdocs

import (
	"io/fs"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/retail-cortex/blitz/docs"
)

// Site is where the docs are published.
const Site = "https://retail-cortex.github.io/blitz/"

// Page is one page of the docs.
type Page struct {
	// Path is its file under content/ without .md: guide/mcp, _index.
	Path  string
	Title string
	// Body is its Markdown, without the front matter.
	Body     string
	Sections []Section
}

// URL is the page's address on the site.
func (p Page) URL() string {
	dir := strings.TrimSuffix(strings.TrimSuffix(p.Path, "_index"), "/")
	if dir == "" {
		return Site
	}
	return Site + dir + "/"
}

// Section is a page's text under one heading (the page's start has none).
type Section struct {
	Heading string
	// Anchor is the heading's id on the site ("" for the page's start).
	Anchor string
	Text   string
}

// Hit is a section found for a search.
type Hit struct {
	Page    *Page
	Section Section
	Score   float64
}

// URL is the hit's address on the site, at its heading.
func (h Hit) URL() string {
	if h.Section.Anchor == "" {
		return h.Page.URL()
	}
	return h.Page.URL() + "#" + h.Section.Anchor
}

// Snippet is the section's first paragraph, plain and at most n runes.
func (h Hit) Snippet(n int) string {
	for para := range strings.SplitSeq(h.Section.Text, "\n\n") {
		p := plain(para)
		if p == "" || strings.HasPrefix(strings.TrimSpace(para), "|") || strings.HasPrefix(strings.TrimSpace(para), "```") {
			continue
		}
		if r := []rune(p); len(r) > n {
			return strings.TrimSpace(string(r[:n-1])) + "…"
		}
		return p
	}
	return ""
}

// Index is the docs, read and ready to search.
type Index struct {
	Pages []*Page
	// The search's numbers: each section's terms, how many sections hold
	// each term, and the sections' mean length.
	docs   []indexed
	df     map[string]int
	avgLen float64
}

type indexed struct {
	page    *Page
	section Section
	tf      map[string]int
	head    map[string]bool // terms in the page's title or the heading
	length  int
}

var (
	once    sync.Once
	builtin *Index
)

// Docs is the index of the docs the program carries, read once.
func Docs() *Index {
	once.Do(func() {
		sub, err := fs.Sub(docs.User, "content")
		if err != nil {
			builtin = &Index{df: map[string]int{}}
			return
		}
		builtin = Load(sub)
	})
	return builtin
}

// Load reads every .md file in fsys into an index.
func Load(fsys fs.FS) *Index {
	ix := &Index{df: map[string]int{}}
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil
		}
		ix.Pages = append(ix.Pages, parse(strings.TrimSuffix(p, ".md"), string(b)))
		return nil
	})
	sort.Slice(ix.Pages, func(i, j int) bool { return ix.Pages[i].Path < ix.Pages[j].Path })
	total := 0
	for _, p := range ix.Pages {
		title := terms(p.Title)
		for _, s := range p.Sections {
			d := indexed{page: p, section: s, tf: map[string]int{}, head: map[string]bool{}}
			for _, t := range terms(s.Heading) {
				d.head[t] = true
			}
			for _, t := range title {
				d.head[t] = true
			}
			for _, t := range terms(s.Heading + " " + s.Text) {
				d.tf[t]++
				d.length++
			}
			for t := range d.tf {
				ix.df[t]++
			}
			total += d.length
			ix.docs = append(ix.docs, d)
		}
	}
	if len(ix.docs) > 0 {
		ix.avgLen = float64(total) / float64(len(ix.docs))
	}
	return ix
}

var (
	frontMatter = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n`)
	titleLine   = regexp.MustCompile(`(?m)^title:\s*"?(.*?)"?\s*$`)
	headingLine = regexp.MustCompile(`(?m)^(#{1,4})\s+(.+?)\s*#*\s*$`)
)

// parse splits a page into its title and sections.
func parse(p, src string) *Page {
	page := &Page{Path: p}
	if m := frontMatter.FindStringSubmatchIndex(src); m != nil {
		if t := titleLine.FindStringSubmatch(src[m[2]:m[3]]); t != nil {
			page.Title = t[1]
		}
		src = src[m[1]:]
	}
	page.Body = strings.TrimSpace(src)
	if page.Title == "" {
		page.Title = path.Base(p)
	}
	locs := headingLine.FindAllStringSubmatchIndex(src, -1)
	start, heading := 0, ""
	add := func(end int) {
		if text := strings.TrimSpace(src[start:end]); text != "" || heading != "" {
			page.Sections = append(page.Sections, Section{Heading: heading, Anchor: anchor(heading), Text: text})
		}
	}
	for _, l := range locs {
		if inFence(src[:l[0]]) {
			continue
		}
		add(l[0])
		heading = plain(src[l[4]:l[5]])
		start = l[1]
	}
	add(len(src))
	return page
}

// inFence reports whether text ends inside a ``` block.
func inFence(text string) bool { return strings.Count(text, "\n```")%2 == 1 }

// anchor is a heading's id as the site makes it: lower case, words joined
// by "-", without punctuation.
func anchor(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

var (
	links    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	markup   = regexp.MustCompile("\\*\\*|`|(?m)^\\s*(#+|>)\\s?")
	shortcde = regexp.MustCompile(`\{\{[<%].*?[%>]\}\}`)
	spaces   = regexp.MustCompile(`\s+`)
)

// plain is Markdown as words: links as their text, no emphasis, code
// marks or shortcodes, on one line.
func plain(md string) string {
	s := shortcde.ReplaceAllString(md, "")
	s = links.ReplaceAllString(s, "$1")
	s = markup.ReplaceAllString(s, "")
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

// stop are words too common to find anything by.
var stop = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true, "by": true, "can": true, "do": true,
	"does": true, "for": true, "from": true, "how": true, "i": true, "in": true, "is": true, "it": true, "its": true, "my": true,
	"of": true, "on": true, "or": true, "the": true, "this": true, "to": true, "what": true, "when": true, "where": true, "which": true,
	"with": true, "you": true, "your": true, "me": true, "we": true, "that": true, "there": true, "if": true, "use": true, "using": true,
}

// terms are text's words for searching: lower case, letters and digits
// (and the "_", "-" and "." inside a name), stop words left out, simple
// plurals made singular.
func terms(text string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.')
	}) {
		w = strings.Trim(w, "-.")
		if len(w) < 2 || stop[w] {
			continue
		}
		out = append(out, stem(w))
	}
	return out
}

// stem makes a simple plural singular: servers → server, policies → policy.
func stem(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us"):
		return w[:len(w)-1]
	}
	return w
}

// Search finds the n sections that best match query (BM25, with words in
// the page's title or the heading counting more); none when no word of it
// is in the docs.
func (ix *Index) Search(query string, n int) []Hit {
	q := terms(query)
	if len(q) == 0 || len(ix.docs) == 0 {
		return nil
	}
	const k1, b = 1.2, 0.75
	N := float64(len(ix.docs))
	var hits []Hit
	for _, d := range ix.docs {
		score := 0.0
		for _, t := range q {
			tf := float64(d.tf[t])
			if tf == 0 && !d.head[t] {
				continue
			}
			idf := math.Log(1 + (N-float64(ix.df[t])+0.5)/(float64(ix.df[t])+0.5))
			score += idf * (tf * (k1 + 1)) / (tf + k1*(1-b+b*float64(d.length)/ix.avgLen))
			if d.head[t] {
				score += idf * 1.5
			}
		}
		if strings.HasPrefix(d.page.Path, "releases/") {
			score *= 0.6 // what changed, after how it works
		}
		if score > 0 {
			hits = append(hits, Hit{Page: d.page, Section: d.section, Score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > n {
		hits = hits[:n]
	}
	return hits
}

// Find is the page a topic names: its path (guide/mcp, mcp), or its title,
// in any case; false when none does.
func (ix *Index) Find(topic string) (*Page, bool) {
	t := strings.Trim(strings.ToLower(strings.TrimSpace(topic)), "/")
	t = strings.TrimSuffix(t, ".md")
	if t == "" {
		return nil, false
	}
	for _, p := range ix.Pages {
		if p.Path == t || strings.EqualFold(p.Title, topic) {
			return p, true
		}
	}
	for _, p := range ix.Pages { // guide/mcp from mcp; a section's _index from its name
		if path.Base(p.Path) == t || p.Path == t+"/_index" {
			return p, true
		}
	}
	return nil, false
}

// Excerpt is hits as the agent reads them: each section's page, heading,
// address and text, at most max runes in all.
func Excerpt(hits []Hit, max int) string {
	var b strings.Builder
	used := 0 // runes
	for _, h := range hits {
		title := h.Page.Title
		if h.Section.Heading != "" {
			title += " › " + h.Section.Heading
		}
		block := "## " + title + "\n" + h.URL() + "\n\n" + h.Section.Text + "\n\n"
		r := []rune(block)
		if used+len(r) > max {
			left := max - used
			if left < 200 {
				break
			}
			r = append(r[:left], []rune("…\n\n")...)
		}
		used += len(r)
		b.WriteString(string(r))
	}
	return strings.TrimSpace(b.String())
}
