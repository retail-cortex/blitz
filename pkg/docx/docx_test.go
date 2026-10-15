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

package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unpack reads a .docx's parts by name.
func unpack(t *testing.T, data []byte) map[string]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	parts := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		require.NoError(t, err)
		b, err := io.ReadAll(r)
		require.NoError(t, err)
		parts[f.Name] = string(b)
	}
	return parts
}

// wellFormed checks every XML part parses.
func wellFormed(t *testing.T, parts map[string]string) {
	t.Helper()
	for name, body := range parts {
		if !strings.HasSuffix(name, ".xml") && !strings.HasSuffix(name, ".rels") {
			continue
		}
		d := xml.NewDecoder(strings.NewReader(body))
		for {
			_, err := d.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err, "%s isn't well-formed XML", name)
		}
	}
}

func picture(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

const sample = `---
title: front matter is dropped
---
# Shop &lt;guide&gt; & "notes" <b>raw</b>

Some **bold**, *italic*, ~~gone~~, ` + "`code`" + ` and a [link](https://example.com/a?b=1&c=2), a [jump](#usage) and <https://auto.example>.
A soft break,  
then a hard one. <span>raw</span>

## Usage

1. one
2. two
   - nested **bullet**
     1. deep
3. three

Between the lists.

5. starts at five
6. six

- [x] done
- [ ] to do

> A quote
> over lines

` + "```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```" + `

` + "```mermaid\nflowchart LR\n  a --> b\n```" + `

| Name | Price | Note |
| :--- | ---: | :---: |
| cart | 10 | **bold** |
| short |

---

![a chart](img/chart.png) ![remote](https://example.com/x.png) ![missing](nope.png)

<div>html block</div>

## Usage
`

func TestRender(t *testing.T) {
	when := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	reads := []string{}
	data, err := Render([]byte(sample), Options{PageSize: Letter, Time: when, ReadImage: func(dest string) ([]byte, error) {
		reads = append(reads, dest)
		if dest == "img/chart.png" {
			return picture(t, 2000, 1000), nil
		}
		return nil, fs.ErrNotExist
	}})
	require.NoError(t, err)
	parts := unpack(t, data)
	wellFormed(t, parts)
	for _, p := range []string{"[Content_Types].xml", "_rels/.rels", "docProps/core.xml", "docProps/app.xml", "word/document.xml", "word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels", "word/media/image1.png"} {
		assert.Contains(t, parts, p)
	}
	assert.Equal(t, []string{"img/chart.png", "nope.png"}, reads, "remote images aren't read")

	doc := parts["word/document.xml"]
	for name, want := range map[string]string{
		"heading style":          `<w:pStyle w:val="Heading1"/>`,
		"escaped heading":        `Shop &lt;guide&gt; &amp; &quot;notes&quot;`,
		"bookmark":               `w:name="h_usage"`,
		"a repeated heading":     `w:name="h_usage_1"`,
		"bold":                   `<w:b/>`,
		"italic":                 `<w:i/>`,
		"strikethrough":          `<w:strike/>`,
		"inline code":            `<w:rStyle w:val="CodeChar"/>`,
		"internal link":          `<w:hyperlink w:anchor="h_usage"`,
		"hard break":             `<w:br/>`,
		"numbered":               `<w:numPr><w:ilvl w:val="0"/><w:numId w:val="2"/></w:numPr>`,
		"nested bullet":          `<w:numPr><w:ilvl w:val="1"/><w:numId w:val="1"/></w:numPr>`,
		"nested number":          `<w:numPr><w:ilvl w:val="2"/><w:numId w:val="3"/></w:numPr>`,
		"task":                   `☒ `,
		"open task":              `☐ `,
		"quote":                  `<w:pStyle w:val="Quote"/>`,
		"code line":              `<w:pStyle w:val="Code"/>`,
		"tab kept":               `<w:tab/>`,
		"mermaid note":           `A Mermaid diagram, kept as its code.`,
		"table":                  `<w:tblStyle w:val="TableGrid"/>`,
		"header row":             `<w:tblHeader/>`,
		"right-aligned":          `<w:jc w:val="right"/>`,
		"centred":                `<w:jc w:val="center"/>`,
		"rule":                   `<w:pBdr><w:bottom`,
		"picture":                `<a:blip r:embed=`,
		"remote picture as text": `[image: remote]`,
		"missing picture":        `[image: missing]`,
		"letter paper":           `<w:pgSz w:w="12240" w:h="15840"/>`,
	} {
		assert.Contains(t, doc, want, name)
	}
	for name, unwanted := range map[string]string{"front matter": "front matter is dropped", "raw inline": "<span>", "html block": "html block"} {
		assert.NotContains(t, doc, unwanted, name)
	}
	assert.Equal(t, 9, strings.Count(doc, `<w:tc>`), "three rows of three: the short row is filled out")
	assert.Contains(t, doc, `<wp:extent cx="5943600" cy="2971800"/>`, "a wide picture fits the text width")

	rels := parts["word/_rels/document.xml.rels"]
	assert.Contains(t, rels, `Target="https://example.com/a?b=1&amp;c=2" TargetMode="External"`)
	assert.Contains(t, rels, `Target="media/image1.png"`)
	num := parts["word/numbering.xml"]
	assert.Contains(t, num, `<w:num w:numId="2"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/>`)
	assert.Contains(t, num, `<w:startOverride w:val="5"/>`, "a list starting at five")
	core := parts["docProps/core.xml"]
	assert.Contains(t, core, `<dc:title>Shop &lt;guide&gt; &amp; &quot;notes&quot; raw</dc:title>`)
	assert.Contains(t, core, `2026-10-14T12:00:00Z`)
	assert.Contains(t, parts["word/styles.xml"], `<w:outlineLvl w:val="0"/>`, "headings are in Word's outline")

	again, err := Render([]byte(sample), Options{PageSize: Letter, Time: when, ReadImage: func(dest string) ([]byte, error) {
		if dest == "img/chart.png" {
			return picture(t, 2000, 1000), nil
		}
		return nil, fs.ErrNotExist
	}})
	require.NoError(t, err)
	assert.Equal(t, data, again, "the same Markdown makes the same file")
}

func TestRenderOptions(t *testing.T) {
	data, err := Render([]byte("plain text, no heading"), Options{Title: "Given"})
	require.NoError(t, err)
	parts := unpack(t, data)
	assert.Contains(t, parts["word/document.xml"], `<w:pgSz w:w="11906" w:h="16838"/>`, "A4 by default")
	assert.Contains(t, parts["docProps/core.xml"], "<dc:title>Given</dc:title>")
	_, err = Render([]byte("bad \x00 bytes"), Options{})
	assert.ErrorIs(t, err, ErrNotText)
	_, err = Render([]byte{0xff, 0xfe}, Options{})
	assert.ErrorIs(t, err, ErrNotText)
}

func TestEscapeAndNames(t *testing.T) {
	assert.Equal(t, "a&amp;b&lt;c&gt;&quot;\tok", escape("a&b<c>\"\x01\tok\x0b"))
	assert.Equal(t, "getting-started", anchor("Getting started!"))
	assert.Equal(t, "h_getting_started", bookmarkName("getting-started"))
	assert.LessOrEqual(t, len([]rune(bookmarkName(strings.Repeat("é", 60)))), 40)
}

// Every page of the user guide writes as a well-formed document.
func TestRenderTheDocs(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "content")
	if _, err := os.Stat(root); err != nil {
		t.Skip("the docs aren't here")
	}
	n := 0
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		src, err := os.ReadFile(p)
		require.NoError(t, err)
		data, err := Render(src, Options{})
		require.NoError(t, err, p)
		wellFormed(t, unpack(t, data))
		n++
		return nil
	}))
	assert.Greater(t, n, 50)
}
