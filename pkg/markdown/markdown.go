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

// Package markdown parses Markdown the way Blitz's exporters read it
// (GitHub-flavoured, by goldmark, with $…$ and $$…$$ maths, a leading YAML
// front matter dropped, as the desktop preview reads it) and gives the plain text of its nodes, for the
// PDF and Word writers alike.
package markdown

import (
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// frontMatter is a leading YAML block.
var frontMatter = regexp.MustCompile(`\A---\r?\n(?s:.*?)\r?\n---\r?\n`)

// Parse parses src as GitHub-flavoured Markdown, without its front matter.
// It returns the document and the source its nodes point into.
func Parse(src []byte) (ast.Node, []byte) {
	src = frontMatter.ReplaceAll(src, nil)
	return goldmark.New(goldmark.WithExtensions(extension.GFM, mathExt)).Parser().Parse(text.NewReader(src)), src
}

// Unescape resolves backslash escapes and character references, as an
// HTML renderer would.
func Unescape(v []byte) []byte {
	return util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v)))
}

// TextOf is a text node's text, unescaped unless it's raw.
func TextOf(n *ast.Text, src []byte) []byte {
	v := n.Value(src)
	if !n.IsRaw() {
		v = Unescape(v)
	}
	return v
}

// PlainText is the text of a node's inlines, without formatting; raw HTML
// is left out.
func PlainText(n ast.Node, src []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				b.Write(TextOf(c, src))
				if c.SoftLineBreak() || c.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(c.Value)
			case *ast.AutoLink:
				b.Write(c.Label(src))
			case *Math:
				b.WriteString(c.Source)
			case *ast.RawHTML:
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// FirstHeading is the plain text of the document's first heading, or "".
func FirstHeading(doc ast.Node, src []byte) string {
	var title string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			title = PlainText(h, src)
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return title
}

// Lines is a block's lines (a code block's), without the final newline.
func Lines(n ast.Node, src []byte) []string {
	var b strings.Builder
	l := n.Lines()
	for i := range l.Len() {
		seg := l.At(i)
		b.Write(seg.Value(src))
	}
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}

// LocalPath is where a picture or link written dest in the document at
// docPath (workspace-relative, with "/") points in the workspace, as the
// desktop app's preview resolves it (files/mdlinks.ts): percent-escapes
// decoded, a query and a fragment dropped, a leading "/" from the
// workspace's root, else from the document's folder. ok is false for a
// URL (http:, data:, …), a fragment alone, or a path that leaves the
// workspace.
func LocalPath(dest, docPath string) (rel string, ok bool) {
	d := strings.TrimSpace(dest)
	if d == "" || strings.HasPrefix(d, "#") || strings.HasPrefix(d, "//") || scheme.MatchString(d) {
		return "", false
	}
	if i := strings.IndexAny(d, "?#"); i >= 0 {
		d = d[:i]
	}
	if u, err := url.PathUnescape(d); err == nil {
		d = u
	}
	var parts []string
	if !strings.HasPrefix(d, "/") && docPath != "" {
		parts = strings.Split(path.Dir(docPath), "/")
		if parts[0] == "." {
			parts = nil
		}
	}
	for _, p := range strings.Split(d, "/") {
		switch p {
		case "", ".":
		case "..":
			if len(parts) == 0 {
				return "", false
			}
			parts = parts[:len(parts)-1]
		default:
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "/"), true
}

var scheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// Picture is a picture written as HTML.
type Picture struct {
	Src, Alt string
	// Width is its width in pixels when the tag gives one, else 0.
	Width int
}

var (
	loneImg   = regexp.MustCompile(`(?is)^\s*<img\b((?:\s+[\w:-]+(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'=<>` + "`" + `]+))?)*)\s*/?>\s*$`)
	attribute = regexp.MustCompile(`([\w:-]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>` + "`" + `]+)))?`)
)

// HTMLPicture is the picture in html when it's nothing but one <img> tag,
// as the preview shows one (raw HTML is otherwise left out): its src,
// alt and width.
func HTMLPicture(html string) (Picture, bool) {
	m := loneImg.FindStringSubmatch(html)
	if m == nil {
		return Picture{}, false
	}
	attrs := map[string]string{}
	for _, a := range attribute.FindAllStringSubmatch(m[1], -1) {
		attrs[strings.ToLower(a[1])] = htmlUnescape(a[2] + a[3] + a[4])
	}
	if attrs["src"] == "" {
		return Picture{}, false
	}
	p := Picture{Src: attrs["src"], Alt: attrs["alt"]}
	if w, err := strconv.Atoi(strings.TrimSuffix(attrs["width"], "px")); err == nil && w > 0 {
		p.Width = w
	}
	return p, true
}

func htmlUnescape(s string) string {
	return strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(s)
}
