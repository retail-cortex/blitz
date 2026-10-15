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

package markdown

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yuin/goldmark/ast"
)

func TestParse(t *testing.T) {
	doc, src := Parse([]byte("---\ntitle: x\n---\n# A *b* &amp; \\*c\\* `d`\n\ntext <b>raw</b> https://example.com\nline\n\n```go\nx := 1\ny := 2\n```\n"))
	assert.Equal(t, "A b & *c* d", FirstHeading(doc, src))
	para := doc.FirstChild().NextSibling()
	assert.Equal(t, "text raw https://example.com line", PlainText(para, src))
	code := para.NextSibling()
	assert.Equal(t, []string{"x := 1", "y := 2"}, Lines(code, src))
	assert.Equal(t, ast.KindFencedCodeBlock, code.Kind())
	empty, src := Parse([]byte("no heading"))
	assert.Empty(t, FirstHeading(empty, src))
}

func TestMath(t *testing.T) {
	src := "Inline $\\beta_1 = 0.5$, prices $5 and $10, \\$x\\$, $$x^2$$ shown, and $ not $.\n\n$$\n\\sum_{i=1}^n x_i\n$$\n\n$$E = mc^2$$\n\n# On $x$\n"
	doc, s := Parse([]byte(src))
	var inline []string
	var display []bool
	var blocks []string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *Math:
			inline, display = append(inline, n.Source), append(display, n.Display)
		case *MathBlock:
			blocks = append(blocks, n.Source)
		}
		return ast.WalkContinue, nil
	})
	assert.Equal(t, []string{`\beta_1 = 0.5`, "x^2", "x"}, inline)
	assert.Equal(t, []bool{false, true, false}, display)
	assert.Equal(t, []string{`\sum_{i=1}^n x_i`, "E = mc^2"}, blocks)
	para := doc.FirstChild()
	assert.Contains(t, PlainText(para, s), "prices $5 and $10")
	assert.Contains(t, PlainText(para, s), "$ not $")
	assert.Equal(t, "On x", FirstHeading(doc, s))
}

func TestLocalPath(t *testing.T) {
	for _, tt := range []struct {
		dest, doc, want string
		ok              bool
	}{
		{"figs/plot.png", "notes/week1.md", "notes/figs/plot.png", true},
		{"./figs/plot.png", "notes/week1.md", "notes/figs/plot.png", true},
		{"my%20figs/plot%20two.png", "a.md", "my figs/plot two.png", true},
		{"/figs/plot.png", "notes/week1.md", "figs/plot.png", true},
		{"../img/a.png?raw=1#x", "notes/week1.md", "img/a.png", true},
		{"a.png", "", "a.png", true},
		{"../../out.png", "notes/w.md", "", false},
		{"https://example.com/a.png", "a.md", "", false},
		{"data:image/png;base64,xx", "a.md", "", false},
		{"#top", "a.md", "", false},
		{"", "a.md", "", false},
	} {
		got, ok := LocalPath(tt.dest, tt.doc)
		assert.Equal(t, tt.ok, ok, tt.dest)
		assert.Equal(t, tt.want, got, tt.dest)
	}
}

func TestHTMLPicture(t *testing.T) {
	p, ok := HTMLPicture(`<img src="figs/plot.png" width="300" alt="A &amp; B">`)
	assert.True(t, ok)
	assert.Equal(t, Picture{Src: "figs/plot.png", Alt: "A & B", Width: 300}, p)
	p, ok = HTMLPicture("<IMG SRC='x.png' width=40px />\n")
	assert.True(t, ok)
	assert.Equal(t, 40, p.Width)
	for _, h := range []string{`<img alt="no src">`, `<p><img src="a.png"></p>`, `<b>x</b>`} {
		_, ok := HTMLPicture(h)
		assert.False(t, ok, h)
	}
}

// Displayed maths' edges: $$ that doesn't close on its line, an unclosed
// block, and the nodes' kinds and dumps.
func TestMathEdges(t *testing.T) {
	doc, src := Parse([]byte("$$x$ and more\n\n$$\nnever closed"))
	var kinds []ast.NodeKind
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && (n.Kind() == KindMath || n.Kind() == KindMathBlock) {
			kinds = append(kinds, n.Kind())
			n.Dump(src, 0)
		}
		return ast.WalkContinue, nil
	})
	assert.Equal(t, []ast.NodeKind{KindMathBlock}, kinds, "only the unclosed block, to the end")
	inline, src := Parse([]byte("$a$"))
	inline.FirstChild().FirstChild().Dump(src, 0)
	assert.Equal(t, KindMath, inline.FirstChild().FirstChild().Kind())
}
