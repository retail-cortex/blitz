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

// Package docx writes Markdown as a Word document (.docx, Office Open XML)
// in pure Go, for Export as Word, the export_docx tool and /export
// (spec_filetools_006 FS-75): GitHub-flavoured Markdown parsed by
// goldmark, written as WordprocessingML with Word's own heading, list,
// quote, code and table styles, so the navigation pane, a table of
// contents and restyling work as in any Word document. Links stay links
// (to a heading too), local images are embedded, raw HTML is skipped, and
// Mermaid diagrams and math are kept as their source.
package docx

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // registers the GIF decoder
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/retail-cortex/blitz/pkg/markdown"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

// Page sizes.
const (
	A4     = "A4"
	Letter = "Letter"
)

// Options shape a document.
type Options struct {
	// PageSize is A4 or Letter (default A4).
	PageSize string
	// Title is the document's title in its properties; default the first
	// heading.
	Title string
	// ReadImage returns the bytes of an image the Markdown refers to, by
	// its destination as written (relative to the Markdown file). nil
	// writes images as their alt text, as it does for any it can't read.
	ReadImage func(dest string) ([]byte, error)
	// Time is the document's creation date (default now).
	Time time.Time
}

// ErrNotText is returned for input that isn't text.
var ErrNotText = errors.New("not a text file")

// maxPixels bounds an image's size, so a hostile one can't exhaust memory.
const maxPixels = 40_000_000

// Page geometry, in twentieths of a point (twips): 1 inch margins.
const (
	margin     = 1440
	emuPerTwip = 635
	emuPerPx   = 9525 // at 96 dpi
)

var pages = map[string][2]int{A4: {11906, 16838}, Letter: {12240, 15840}}

// Render writes Markdown as a .docx file's bytes.
func Render(src []byte, o Options) ([]byte, error) {
	if !utf8.Valid(src) || bytes.IndexByte(src, 0) >= 0 {
		return nil, ErrNotText
	}
	doc, src := markdown.Parse(src)
	size, ok := pages[o.PageSize]
	if !ok {
		size = pages[A4]
	}
	w := &writer{src: src, o: o, slugs: map[string]int{}, textWidth: (size[0] - 2*margin) * emuPerTwip}
	w.blocks(doc, para{})
	title := o.Title
	if title == "" {
		title = markdown.FirstHeading(doc, src)
	}
	when := o.Time
	if when.IsZero() {
		when = time.Now()
	}
	return w.pack(title, when.UTC(), size)
}

// para is how the paragraphs of a block are set: their style, list
// numbering and indent.
type para struct {
	style  string
	numID  int // 0: not numbered
	level  int // list nesting, from 0
	indent int // twips, for a list item's later paragraphs
	listed bool
}

// rel is one of the document's relationships: a hyperlink or an image.
type rel struct {
	id, kind, target string
	external         bool
}

type media struct {
	name string
	data []byte
}

// list is a numbered list's numbering instance: Word restarts a list's
// numbers only with a new one.
type list struct {
	start   int
	level   int
	ordered bool
}

type writer struct {
	src       []byte
	o         Options
	body      strings.Builder
	rels      []rel
	media     []media
	lists     []list
	slugs     map[string]int
	textWidth int // EMU
	pics      int
	marks     int
	// inline state
	bold, italic, strike, code bool
	link                       bool
}

func (w *writer) blocks(n ast.Node, p para) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		w.block(c, p)
		if p.numID != 0 {
			// Only an item's first paragraph has its number; the rest
			// line up under its text.
			p.numID, p.style = 0, "ListParagraph"
		}
	}
}

func (w *writer) block(n ast.Node, p para) {
	switch n := n.(type) {
	case *ast.Heading:
		w.heading(n)
	case *ast.Paragraph, *ast.TextBlock:
		w.paragraph(n, p)
	case *ast.ThematicBreak:
		w.body.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="BFBFBF"/></w:pBdr></w:pPr></w:p>`)
	case *ast.FencedCodeBlock:
		lang := string(n.Language(w.src))
		w.codeBlock(markdown.Lines(n, w.src), p)
		if strings.EqualFold(lang, "mermaid") {
			w.note("A Mermaid diagram, kept as its code.", p)
		}
	case *ast.CodeBlock:
		w.codeBlock(markdown.Lines(n, w.src), p)
	case *ast.Blockquote:
		p.style = "Quote"
		w.blocks(n, p)
	case *ast.List:
		w.list(n, p)
	case *east.Table:
		w.table(n)
	case *ast.HTMLBlock:
		// Raw HTML isn't written, as the app's preview doesn't draw it.
	default:
		w.blocks(n, p)
	}
}

// pPr is a paragraph's properties.
func (w *writer) pPr(p para) string {
	var b strings.Builder
	if p.style != "" {
		fmt.Fprintf(&b, `<w:pStyle w:val="%s"/>`, p.style)
	}
	if p.numID != 0 {
		fmt.Fprintf(&b, `<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, p.level, p.numID)
	} else if p.indent > 0 {
		fmt.Fprintf(&b, `<w:ind w:left="%d"/>`, p.indent)
	}
	if b.Len() == 0 {
		return ""
	}
	return "<w:pPr>" + b.String() + "</w:pPr>"
}

func (w *writer) heading(n *ast.Heading) {
	level := min(max(n.Level, 1), 6)
	name := w.bookmark(markdown.PlainText(n, w.src))
	w.marks++
	fmt.Fprintf(&w.body, `<w:p><w:pPr><w:pStyle w:val="Heading%d"/></w:pPr><w:bookmarkStart w:id="%d" w:name="%s"/>`, level, w.marks, name)
	w.inlines(n)
	fmt.Fprintf(&w.body, `<w:bookmarkEnd w:id="%d"/></w:p>`, w.marks)
}

func (w *writer) paragraph(n ast.Node, p para) {
	w.body.WriteString("<w:p>" + w.pPr(p))
	w.inlines(n)
	w.body.WriteString("</w:p>")
}

// note is a short muted line, in italics.
func (w *writer) note(s string, p para) {
	w.body.WriteString("<w:p>" + w.pPr(p) + `<w:r><w:rPr><w:i/><w:color w:val="767676"/></w:rPr>` + textRun(s) + "</w:r></w:p>")
}

// codeBlock is a line per paragraph in the Code style, spaces and tabs
// kept, with room above and below the block.
func (w *writer) codeBlock(lines []string, p para) {
	for i, l := range lines {
		w.body.WriteString(`<w:p><w:pPr><w:pStyle w:val="Code"/>`)
		switch {
		case len(lines) == 1:
			w.body.WriteString(`<w:spacing w:before="120" w:after="200"/>`)
		case i == 0:
			w.body.WriteString(`<w:spacing w:before="120"/>`)
		case i == len(lines)-1:
			w.body.WriteString(`<w:spacing w:after="200"/>`)
		}
		if p.indent > 0 || p.numID != 0 {
			fmt.Fprintf(&w.body, `<w:ind w:left="%d"/>`, max(p.indent, 720*(p.level+1)))
		}
		w.body.WriteString("</w:pPr><w:r>")
		for i, part := range strings.Split(l, "\t") {
			if i > 0 {
				w.body.WriteString("<w:tab/>")
			}
			w.body.WriteString(textRun(part))
		}
		w.body.WriteString("</w:r></w:p>")
	}
}

func (w *writer) list(n *ast.List, p para) {
	level := 0
	if p.listed {
		level = p.level + 1
	}
	numID := 1 // bullets: one shared list
	if n.IsOrdered() {
		w.lists = append(w.lists, list{start: n.Start, level: level, ordered: true})
		numID = len(w.lists) + 1
	}
	for item := n.FirstChild(); item != nil; item = item.NextSibling() {
		ip := para{style: "ListParagraph", numID: numID, level: level, indent: 720 * (level + 1), listed: true}
		if isTask(item) {
			ip.numID = 0 // its checkbox stands in for the bullet
		}
		w.blocks(item, ip)
	}
}

func isTask(item ast.Node) bool {
	if first := item.FirstChild(); first != nil {
		_, ok := first.FirstChild().(*east.TaskCheckBox)
		return ok
	}
	return false
}

func (w *writer) table(t *east.Table) {
	cols := len(t.Alignments)
	w.body.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/><w:tblLook w:val="04A0" w:firstRow="1" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="0" w:noVBand="1"/></w:tblPr><w:tblGrid>`)
	for range cols {
		fmt.Fprintf(&w.body, `<w:gridCol w:w="%d"/>`, (w.textWidth/emuPerTwip)/max(cols, 1))
	}
	w.body.WriteString("</w:tblGrid>")
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		_, header := row.(*east.TableHeader)
		w.body.WriteString("<w:tr>")
		if header {
			w.body.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		i := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			w.body.WriteString(`<w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/></w:tcPr><w:p><w:pPr><w:spacing w:after="0"/>`)
			if i < cols {
				if jc := justify(t.Alignments[i]); jc != "" {
					fmt.Fprintf(&w.body, `<w:jc w:val="%s"/>`, jc)
				}
			}
			w.body.WriteString("</w:pPr>")
			saved := w.bold
			w.bold = w.bold || header
			w.inlines(cell)
			w.bold = saved
			w.body.WriteString("</w:p></w:tc>")
			i++
		}
		// A row short of cells is filled out, as Word wants every row whole.
		for ; i < cols; i++ {
			w.body.WriteString(`<w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/></w:tcPr><w:p/></w:tc>`)
		}
		w.body.WriteString("</w:tr>")
	}
	// A table right after a table would merge with it; an empty
	// paragraph keeps them apart.
	w.body.WriteString(`</w:tbl><w:p/>`)
}

func justify(a east.Alignment) string {
	switch a {
	case east.AlignCenter:
		return "center"
	case east.AlignRight:
		return "right"
	}
	return ""
}

// bookmark is a heading's bookmark name, from its GitHub-style anchor
// ("Getting started" → "getting-started", a repeat "-1"), so a link to
// "#getting-started" finds it.
func (w *writer) bookmark(heading string) string {
	slug := anchor(heading)
	if n := w.slugs[slug]; n > 0 {
		w.slugs[slug] = n + 1
		slug = fmt.Sprintf("%s-%d", slug, n)
	} else {
		w.slugs[slug] = 1
	}
	return bookmarkName(slug)
}

// anchor is a heading's anchor as GitHub makes it: lower case, spaces as
// hyphens, punctuation dropped.
func anchor(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// bookmarkName is an anchor as Word allows a bookmark's name: letters,
// digits and underscores, a letter first, at most 40 characters.
func bookmarkName(slug string) string {
	var b strings.Builder
	b.WriteString("h_")
	for _, r := range slug {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := b.String()
	for utf8.RuneCountInString(name) > 40 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

func (w *writer) inlines(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		w.inline(c)
	}
}

func (w *writer) inline(n ast.Node) {
	switch n := n.(type) {
	case *ast.Text:
		w.run(string(markdown.TextOf(n, w.src)))
		switch {
		case n.HardLineBreak():
			w.body.WriteString("<w:r><w:br/></w:r>")
		case n.SoftLineBreak():
			w.run(" ")
		}
	case *ast.String:
		w.run(string(n.Value))
	case *ast.CodeSpan:
		saved := w.code
		w.code = true
		w.inlines(n)
		w.code = saved
	case *ast.Emphasis:
		bold, italic := w.bold, w.italic
		if n.Level >= 2 {
			w.bold = true
		} else {
			w.italic = true
		}
		w.inlines(n)
		w.bold, w.italic = bold, italic
	case *east.Strikethrough:
		saved := w.strike
		w.strike = true
		w.inlines(n)
		w.strike = saved
	case *ast.Link:
		w.hyperlink(string(n.Destination), func() { w.inlines(n) })
	case *ast.AutoLink:
		w.hyperlink(string(n.URL(w.src)), func() { w.run(string(n.Label(w.src))) })
	case *ast.Image:
		w.image(n)
	case *ast.RawHTML:
		// Skipped, as in the preview.
	case *east.TaskCheckBox:
		if n.IsChecked {
			w.run("☒ ")
		} else {
			w.run("☐ ")
		}
	default:
		w.inlines(n)
	}
}

// hyperlink writes a link: to a heading ("#…"), or to anything else,
// which Word opens (a web page, a file beside the document).
func (w *writer) hyperlink(dest string, inner func()) {
	if w.link || dest == "" {
		inner()
		return
	}
	if target, ok := strings.CutPrefix(dest, "#"); ok {
		fmt.Fprintf(&w.body, `<w:hyperlink w:anchor="%s" w:history="1">`, bookmarkName(target))
	} else {
		id := w.rel("http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink", dest, true)
		fmt.Fprintf(&w.body, `<w:hyperlink r:id="%s" w:history="1">`, id)
	}
	w.link = true
	inner()
	w.link = false
	w.body.WriteString("</w:hyperlink>")
}

// run writes text in the current style.
func (w *writer) run(s string) {
	if s == "" {
		return
	}
	var pr strings.Builder
	switch {
	case w.link:
		pr.WriteString(`<w:rStyle w:val="Hyperlink"/>`)
	case w.code:
		pr.WriteString(`<w:rStyle w:val="CodeChar"/>`)
	}
	if w.bold {
		pr.WriteString("<w:b/>")
	}
	if w.italic {
		pr.WriteString("<w:i/>")
	}
	if w.strike {
		pr.WriteString("<w:strike/>")
	}
	w.body.WriteString("<w:r>")
	if pr.Len() > 0 {
		w.body.WriteString("<w:rPr>" + pr.String() + "</w:rPr>")
	}
	w.body.WriteString(textRun(s) + "</w:r>")
}

// textRun is a run's text element, its spaces kept.
func textRun(s string) string {
	return `<w:t xml:space="preserve">` + escape(s) + "</w:t>"
}

// escape makes text safe in XML, dropping characters XML can't hold.
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r != 0xFFFE && r != 0xFFFF && !(r >= 0xD800 && r <= 0xDFFF)):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// rel adds a relationship and returns its id.
func (w *writer) rel(kind, target string, external bool) string {
	id := fmt.Sprintf("rId%d", len(w.rels)+10) // rId1–9: the fixed parts
	w.rels = append(w.rels, rel{id: id, kind: kind, target: target, external: external})
	return id
}

// image embeds a picture the caller lets it read, scaled to the text
// width; anything else is written as its alt text.
func (w *writer) image(n *ast.Image) {
	alt := markdown.PlainText(n, w.src)
	dest := string(n.Destination)
	data, ext, wpx, hpx := w.loadImage(dest)
	if data == nil {
		saved := w.italic
		w.italic = true
		w.run("[image: " + cmp(alt, dest) + "]")
		w.italic = saved
		return
	}
	w.pics++
	name := fmt.Sprintf("image%d.%s", w.pics, ext)
	w.media = append(w.media, media{name: name, data: data})
	id := w.rel("http://schemas.openxmlformats.org/officeDocument/2006/relationships/image", "media/"+name, false)
	cx, cy := wpx*emuPerPx, hpx*emuPerPx
	if cx > w.textWidth {
		cx, cy = w.textWidth, cy*w.textWidth/cx
	}
	fmt.Fprintf(&w.body, `<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="Picture %d" descr="%s"/><wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="%d" name="%s" descr="%s"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		cx, cy, w.pics, w.pics, escape(alt), w.pics, name, escape(alt), id, cx, cy)
}

// loadImage reads and checks an image: its bytes, extension and size in
// pixels; nil when it can't be embedded.
func (w *writer) loadImage(dest string) ([]byte, string, int, int) {
	if w.o.ReadImage == nil || dest == "" || strings.Contains(dest, ":") {
		return nil, "", 0, 0 // remote (http:, data:) or nothing to read with
	}
	data, err := w.o.ReadImage(dest)
	if err != nil {
		return nil, "", 0, 0
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, "", 0, 0
	}
	return data, map[string]string{"jpeg": "jpeg", "png": "png", "gif": "gif"}[format], cfg.Width, cfg.Height
}

func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
