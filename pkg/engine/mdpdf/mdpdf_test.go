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

package mdpdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/ledongthuc/pdf"
	"github.com/retail-cortex/blitz/pkg/pdftext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func render(t *testing.T, md string, o Options) (*Result, string) {
	t.Helper()
	res, err := Render([]byte(md), o)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(res.PDF, []byte("%PDF-")))
	text, pages, err := pdftext.Text(context.Background(), res.PDF, 0)
	require.NoError(t, err)
	assert.Equal(t, res.Pages, pages)
	return res, strings.Join(strings.Fields(text), " ")
}

func TestRender(t *testing.T) {
	tests := []struct {
		name    string
		md      string
		want    []string
		notWant []string
	}{
		{"heading and paragraph", "# Notes\n\nGradient descent **minimises** a *loss*.", []string{"Notes", "minimises", "loss"}, nil},
		{"escapes and entities", `a\*b\* &amp; c &#169;`, []string{"a*b*", "& c ©"}, nil},
		{"lists", "1. first\n2. second\n   - inner\n\n- [x] done\n- [ ] todo", []string{"1.", "first", "2.", "inner", "done", "todo"}, nil},
		{"table", "| A | B |\n|---|--:|\n| one | two |\n| three | four four four |", []string{"A", "one", "two", "three", "four"}, nil},
		{"code", "```go\nfunc main() {}\n```", []string{"func", "main"}, nil},
		{"mermaid kept as code", "```mermaid\ngraph TD; A-->B\n```", []string{"graph", "Diagram: export from the Blitz desktop app"}, nil},
		{"non-Latin", "Ελληνικά, кириллица, café", []string{"Ελληνικά", "кириллица", "café"}, nil},
		{"math falls back", "θ ← θ − η∇L", []string{"∇"}, nil},
		{"raw HTML skipped", "before\n\n<div>hidden html</div>\n\nafter", []string{"before", "after"}, []string{"hidden"}},
		{"front matter hidden", "---\ntitle: x\nsecret: y\n---\n# Body", []string{"Body"}, []string{"secret"}},
		{"links", "[site](https://example.com) and https://example.org", []string{"site", "example.org"}, nil},
		{"quote and rule", "> quoted words\n\n---\n\nafter", []string{"quoted words", "after"}, nil},
		{"emoji replaced", "ok 🎉 done", []string{"ok", "done"}, nil},
		{"footer", "text", []string{"1 / 1"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, text := render(t, tc.md, Options{})
			for _, w := range tc.want {
				assert.Contains(t, text, w)
			}
			for _, w := range tc.notWant {
				assert.NotContains(t, text, w)
			}
		})
	}
}

func TestRenderPagesAndOutline(t *testing.T) {
	var md strings.Builder
	md.WriteString("# Course\n\n")
	for i := range 6 {
		fmt.Fprintf(&md, "## Week %d\n\n### Reading\n\n", i+1)
		fmt.Fprintf(&md, "%s\n\n", strings.Repeat("A long paragraph of study notes that fills the page. ", 40))
		fmt.Fprintf(&md, "| a | b |\n|---|---|\n%s\n", strings.Repeat("| x | y |\n", 30))
		fmt.Fprintf(&md, "```\n%s```\n\n", strings.Repeat("line\n", 40))
	}
	res, err := Render([]byte(md.String()), Options{PageSize: Letter, Title: "My notes"})
	require.NoError(t, err)
	assert.Greater(t, res.Pages, 3)

	r, err := pdf.NewReader(bytes.NewReader(res.PDF), int64(len(res.PDF)))
	require.NoError(t, err)
	assert.Equal(t, res.Pages, r.NumPage())
	box := r.Trailer().Key("Root").Key("Pages").Key("MediaBox") // inherited by each page
	assert.InDelta(t, 612, box.Index(2).Float64(), 1, "Letter is 8.5in wide")

	outline := r.Outline()
	require.Len(t, outline.Child, 1)
	assert.Equal(t, "Course", outline.Child[0].Title)
	require.Len(t, outline.Child[0].Child, 6)
	assert.Equal(t, "Week 1", outline.Child[0].Child[0].Title)
	assert.Equal(t, "Reading", outline.Child[0].Child[0].Child[0].Title)
	assert.Equal(t, "My notes", r.Trailer().Key("Info").Key("Title").Text())
}

func TestOutlineLevelsNeverSkip(t *testing.T) {
	res, err := Render([]byte("### Deep first\n\n# Top\n\n#### Skips a level"), Options{})
	require.NoError(t, err)
	r, err := pdf.NewReader(bytes.NewReader(res.PDF), int64(len(res.PDF)))
	require.NoError(t, err)
	o := r.Outline()
	require.Len(t, o.Child, 2)
	assert.Equal(t, "Deep first", o.Child[0].Title)
	require.Len(t, o.Child[1].Child, 1)
	assert.Equal(t, "Skips a level", o.Child[1].Child[0].Title)
	assert.Equal(t, "Deep first", r.Trailer().Key("Info").Key("Title").Text(), "the first heading titles it")
}

func encode(t *testing.T, format string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src) // opaque: no soft mask
	for x := range 40 {
		img.Set(x, 10, color.RGBA{200, 0, 0, 255})
	}
	var buf bytes.Buffer
	switch format {
	case "png":
		require.NoError(t, png.Encode(&buf, img))
	case "jpeg":
		require.NoError(t, jpeg.Encode(&buf, img, nil))
	case "gif":
		require.NoError(t, gif.Encode(&buf, img, nil))
	}
	return buf.Bytes()
}

func TestImages(t *testing.T) {
	files := map[string][]byte{
		"a.png": encode(t, "png"), "b.jpg": encode(t, "jpeg"), "c.gif": encode(t, "gif"),
		"bad.png": []byte("not an image"),
	}
	var asked []string
	read := func(dest string) ([]byte, error) {
		asked = append(asked, dest)
		if data, ok := files[dest]; ok {
			return data, nil
		}
		return nil, errors.New("outside the workspace")
	}
	tests := []struct {
		name   string
		md     string
		images int
		text   string
		read   func(string) ([]byte, error)
	}{
		{"png", "![chart](a.png)", 1, "", read},
		{"jpeg", "![photo](b.jpg)", 1, "", read},
		{"gif", "![anim](c.gif)", 1, "", read},
		{"refused", "![secret](../../etc/x.png)", 0, "[image: secret]", read},
		{"not an image", "![broken](bad.png)", 0, "[image: broken]", read},
		{"remote never read", "![remote](https://example.com/x.png)", 0, "[image: remote]", read},
		{"no reader", "![chart](a.png)", 0, "[image: chart]", nil},
		{"inline with text", "before ![chart](a.png) after", 1, "after", read},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			asked = nil
			res, text := render(t, tc.md, Options{ReadImage: tc.read})
			assert.Equal(t, tc.images, bytes.Count(res.PDF, []byte("/Subtype /Image")))
			if tc.text != "" {
				assert.Contains(t, text, tc.text)
			}
			if tc.name == "remote never read" {
				assert.Empty(t, asked)
			}
		})
	}
}

func TestPageSizeFor(t *testing.T) {
	tests := []struct {
		setting, lang, want string
	}{
		{"a4", "en_US.UTF-8", A4},
		{"Letter", "de_DE.UTF-8", Letter},
		{"auto", "en_US.UTF-8", Letter},
		{"", "en_CA", Letter},
		{"auto", "en_GB.UTF-8", A4},
		{"auto", "C", A4},
	}
	for _, tc := range tests {
		t.Run(tc.setting+"/"+tc.lang, func(t *testing.T) {
			t.Setenv("LC_ALL", "")
			t.Setenv("LC_PAPER", "")
			t.Setenv("LANG", tc.lang)
			assert.Equal(t, tc.want, PageSizeFor(tc.setting))
		})
	}
}

func TestWrapSegments(t *testing.T) {
	red, blue := [3]int{1, 0, 0}, [3]int{0, 0, 1}
	tests := []struct {
		name string
		segs []segment
		per  int
		want [][]segment
	}{
		{"fits", []segment{{"ab", red}}, 4, [][]segment{{{"ab", red}}}},
		{"wraps", []segment{{"abcdef", red}}, 4, [][]segment{{{"abcd", red}}, {{"ef", red}}}},
		{"newlines and colours", []segment{{"a\n", red}, {"b", blue}}, 4, [][]segment{{{"a", red}}, {{"b", blue}}}},
		{"tabs", []segment{{"\tx", red}}, 8, [][]segment{{{"    x", red}}}},
		{"merges a colour", []segment{{"a", red}, {"b", red}}, 4, [][]segment{{{"ab", red}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, wrapSegments(tc.segs, tc.per))
		})
	}
}

func TestCheckText(t *testing.T) {
	assert.NoError(t, CheckText([]byte("# hi")))
	assert.ErrorIs(t, CheckText([]byte{0x89, 'P', 0}), ErrNotMarkdown)
}

// The less common constructs, each laid out without failing.
func TestRenderEverything(t *testing.T) {
	wide := image.NewGray(image.Rect(0, 0, 2400, 300))
	tall := image.NewGray(image.Rect(0, 0, 200, 3000))
	var w, h bytes.Buffer
	require.NoError(t, png.Encode(&w, wide))
	require.NoError(t, png.Encode(&h, tall))
	files := map[string][]byte{"wide.png": w.Bytes(), "tall.png": h.Bytes()}
	md := `Setext heading
over two lines
==============

    indented code, θ and ∇ in mono

Inline ` + "`code with θ ∇`" + `, ~~struck~~, <kbd>raw</kbd> html,
a hard break  
after it, and a [link with **bold**](https://example.com).

` + "```nosuchlanguage\nplain text\n```" + `

| left | centre | right |
|:---|:---:|---:|
| ` + strings.Repeat("long words in a cell ", 12) + ` | ` + strings.Repeat("more long words ", 12) + ` | ` + strings.Repeat("and more ", 15) + ` |

![wide](wide.png) ![tall](tall.png)

- [ ] a task

- 
`
	res, text := render(t, md, Options{Time: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
		ReadImage: func(d string) ([]byte, error) { return files[d], nil }})
	for _, want := range []string{"Setext heading over two lines", "indented code", "code with", "struck", "hard break", "after it", "plain text", "centre", "a task"} {
		assert.Contains(t, text, want)
	}
	assert.NotContains(t, text, "kbd", "inline tags are dropped, their text kept")
	assert.Equal(t, 2, bytes.Count(res.PDF, []byte("/Subtype /Image")))

	r, err := pdf.NewReader(bytes.NewReader(res.PDF), int64(len(res.PDF)))
	require.NoError(t, err)
	assert.Equal(t, "Setext heading over two lines", r.Outline().Child[0].Title)
}

func TestCmpString(t *testing.T) {
	assert.Equal(t, "a", cmpString("a", "b"))
	assert.Equal(t, "b", cmpString("", "b"))
}
