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

// Package mdpdf typesets Markdown as a PDF in pure Go, for the export_pdf
// tool (spec_filetools_006): GitHub-flavoured Markdown parsed by goldmark,
// laid out with fpdf in embedded Noto fonts, with an outline of the
// headings, page numbers, tables, highlighted code and the images the
// caller lets it read. Raw HTML is skipped and Mermaid diagrams print as
// their code: drawing them needs a browser, which the desktop app's own
// export has.
package mdpdf

import (
	"bytes"
	"compress/gzip"
	"embed"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // registers the GIF decoder
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/go-pdf/fpdf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/image/font/sfnt"
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
	// prints images as their alt text, as it does for any it can't read.
	ReadImage func(dest string) ([]byte, error)
	// Time is the document's creation date (default now).
	Time time.Time
}

// Result is a typeset document.
type Result struct {
	PDF   []byte
	Pages int
}

// PageSizeFor resolves a [pdf] page_size setting: "a4", "letter", or
// "auto" (or empty), which picks Letter where the locale's country uses it.
func PageSizeFor(setting string) string {
	switch strings.ToLower(strings.TrimSpace(setting)) {
	case "a4":
		return A4
	case "letter":
		return Letter
	}
	for _, v := range []string{"LC_ALL", "LC_PAPER", "LANG"} {
		if loc := os.Getenv(v); loc != "" {
			return pageSizeForLocale(loc)
		}
	}
	return A4
}

// letterCountries use US Letter paper.
var letterCountries = map[string]bool{"US": true, "CA": true, "MX": true, "PH": true, "CL": true, "CO": true, "VE": true, "PR": true}

func pageSizeForLocale(loc string) string {
	loc, _, _ = strings.Cut(loc, ".") // en_US.UTF-8
	if _, country, ok := strings.Cut(loc, "_"); ok && letterCountries[strings.ToUpper(country)] {
		return Letter
	}
	return A4
}

//go:embed fonts/*.ttf.gz
var fontFiles embed.FS

// fonts are the embedded families, decompressed once.
var fonts = sync.OnceValues(func() (map[string][]byte, error) {
	out := map[string][]byte{}
	for key, name := range map[string]string{
		"sans":   "NotoSans-Regular",
		"sansB":  "NotoSans-Bold",
		"sansI":  "NotoSans-Italic",
		"sansBI": "NotoSans-BoldItalic",
		"mono":   "NotoSansMono-Regular",
		"math":   "NotoSansMath-Regular",
	} {
		f, err := fontFiles.Open("fonts/" + name + ".ttf.gz")
		if err != nil {
			return nil, err
		}
		zr, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		data, err := io.ReadAll(zr)
		f.Close()
		if err != nil {
			return nil, err
		}
		out[key] = data
	}
	return out, nil
})

// cmaps are the fonts' character maps, for falling back from one font to
// another a character at a time (fpdf can't tell a missing glyph).
var cmaps = sync.OnceValues(func() (map[string]*sfnt.Font, error) {
	fs, err := fonts()
	if err != nil {
		return nil, err
	}
	out := map[string]*sfnt.Font{}
	for _, key := range []string{"sans", "mono", "math"} {
		if out[key], err = sfnt.Parse(fs[key]); err != nil {
			return nil, err
		}
	}
	return out, nil
})

// Layout, in millimetres and points.
const (
	margin      = 20.0
	marginTop   = 18.0
	marginBot   = 18.0
	baseSize    = 10.5
	codeSize    = 8.8
	tableSize   = 9.5
	lineFactor  = 1.5
	paraGap     = 2.6
	listIndent  = 6.5
	quoteIndent = 5.0
	codePad     = 3.0
	cellPad     = 1.6
	ptToMM      = 25.4 / 72
	maxPixels   = 50_000_000
)

// Colours.
var (
	textColor  = [3]int{31, 35, 40}
	mutedColor = [3]int{89, 99, 110}
	linkColor  = [3]int{9, 105, 218}
	codeColor  = [3]int{175, 34, 87}
	ruleColor  = [3]int{208, 215, 222}
	codeFill   = [3]int{246, 248, 250}
	headFill   = [3]int{240, 243, 246}
)

var headingSizes = map[int]float64{1: 20, 2: 16, 3: 13.5, 4: 12, 5: 11, 6: 10.5}

func lineHeight(pt float64) float64 { return pt * ptToMM * lineFactor }

// frontMatter is a leading YAML block, which the desktop preview hides too.
var frontMatter = regexp.MustCompile(`\A---\r?\n(?s:.*?)\r?\n---\r?\n`)

// Render typesets Markdown.
func Render(src []byte, o Options) (*Result, error) {
	fs, err := fonts()
	if err != nil {
		return nil, fmt.Errorf("fonts: %w", err)
	}
	cm, err := cmaps()
	if err != nil {
		return nil, fmt.Errorf("fonts: %w", err)
	}
	src = frontMatter.ReplaceAll(src, nil)
	doc := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(src))

	size := o.PageSize
	if size != Letter {
		size = A4
	}
	f := fpdf.New("P", "mm", size, "")
	for _, ff := range []struct{ family, style, key string }{
		{"sans", "", "sans"}, {"sans", "B", "sansB"}, {"sans", "I", "sansI"}, {"sans", "BI", "sansBI"}, {"mono", "", "mono"}, {"math", "", "math"},
	} {
		f.AddUTF8FontFromBytes(ff.family, ff.style, fs[ff.key])
	}
	f.SetMargins(margin, marginTop, margin)
	f.SetAutoPageBreak(true, marginBot)
	f.AliasNbPages("{nb}")
	f.SetCreator("Blitz", true)
	f.SetCreationDate(cmpTime(o.Time))
	title := o.Title
	if title == "" {
		title = firstHeading(doc, src)
	}
	if title != "" {
		f.SetTitle(title, true)
	}
	f.SetFooterFunc(func() {
		f.SetY(-12)
		f.SetFont("sans", "", 8)
		f.SetTextColor(mutedColor[0], mutedColor[1], mutedColor[2])
		f.CellFormat(0, 5, fmt.Sprintf("%d / {nb}", f.PageNo()), "", 0, "C", false, 0, "")
		f.SetTextColor(textColor[0], textColor[1], textColor[2])
	})
	f.AddPage()

	r := &renderer{f: f, src: src, o: o, lastLevel: -1, cmaps: cm, color: textColor, covered: map[covKey]bool{}}
	r.blocks(doc)
	if f.Err() {
		return nil, f.Error()
	}
	var buf bytes.Buffer
	if err := f.Output(&buf); err != nil {
		return nil, err
	}
	return &Result{PDF: buf.Bytes(), Pages: f.PageCount()}, nil
}

func cmpTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

func firstHeading(doc ast.Node, src []byte) string {
	var title string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			title = plainText(h, src)
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return title
}

// style is how inline text is set.
type style struct {
	bold, italic, code bool
	link               string
	color              [3]int
}

type renderer struct {
	f         *fpdf.Fpdf
	src       []byte
	o         Options
	cmaps     map[string]*sfnt.Font
	buf       sfnt.Buffer
	covered   map[covKey]bool
	color     [3]int // the text colour of the block (muted in a quote)
	st        style
	size      float64 // the block's font size
	lh        float64 // the block's line height
	lastLevel int     // the last outline level (-1: none yet)
	depth     int     // list nesting
	images    int
}

func (r *renderer) blocks(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		r.block(c)
	}
}

func (r *renderer) block(n ast.Node) {
	switch n := n.(type) {
	case *ast.Heading:
		r.heading(n)
	case *ast.Paragraph:
		r.paragraph(n, paraGap)
	case *ast.TextBlock:
		r.paragraph(n, 0.6)
	case *ast.ThematicBreak:
		r.rule()
	case *ast.FencedCodeBlock:
		lang := string(n.Language(r.src))
		r.code(r.lines(n), lang)
		if strings.EqualFold(lang, "mermaid") {
			r.note("Diagram: export from the Blitz desktop app to draw it.")
		}
	case *ast.CodeBlock:
		r.code(r.lines(n), "")
	case *ast.Blockquote:
		r.quote(n)
	case *ast.List:
		r.list(n)
	case *east.Table:
		r.table(n)
	case *ast.HTMLBlock:
		// Raw HTML isn't drawn, as in the app's preview.
	default:
		r.blocks(n)
	}
}

// begin starts a block of text at size pt.
func (r *renderer) begin(size float64, base style) {
	r.size, r.lh, r.st = size, lineHeight(size), base
	if r.st.color == ([3]int{}) {
		r.st.color = r.color
	}
}

// space adds vertical space, unless at the top of a page.
func (r *renderer) space(h float64) {
	if r.f.GetY() > marginTop+0.01 {
		r.f.SetY(r.f.GetY() + h)
		r.f.SetX(r.left())
	}
}

func (r *renderer) left() float64 {
	l, _, _, _ := r.f.GetMargins()
	return l
}

func (r *renderer) width() float64 {
	l, _, rm, _ := r.f.GetMargins()
	w, _ := r.f.GetPageSize()
	return w - l - rm
}

func (r *renderer) bottom() float64 {
	_, h := r.f.GetPageSize()
	return h - marginBot
}

// keep starts a new page unless h fits on this one.
func (r *renderer) keep(h float64) {
	if r.f.GetY()+h > r.bottom() {
		r.f.AddPage()
		r.f.SetX(r.left())
	}
}

// endLine finishes the current line of inline text.
func (r *renderer) endLine() {
	if r.f.GetX() > r.left()+0.01 {
		r.f.Ln(r.lh)
	}
}

func (r *renderer) heading(n *ast.Heading) {
	size := headingSizes[n.Level]
	r.space(size * 0.45)
	r.keep(lineHeight(size)*2 + 8) // never last on a page
	level := min(n.Level-1, r.lastLevel+1)
	r.lastLevel = level
	r.f.Bookmark(plainText(n, r.src), level, -1)
	r.begin(size, style{bold: true})
	r.f.SetX(r.left())
	r.inlines(n)
	r.endLine()
	if n.Level <= 2 {
		y := r.f.GetY() + 0.8
		r.f.SetDrawColor(ruleColor[0], ruleColor[1], ruleColor[2])
		r.f.SetLineWidth(0.25)
		r.f.Line(r.left(), y, r.left()+r.width(), y)
		r.f.SetY(y + 1.2)
		r.f.SetX(r.left())
	}
	r.space(1.4)
}

func (r *renderer) paragraph(n ast.Node, gap float64) {
	r.begin(baseSize, style{})
	r.inlines(n)
	r.endLine()
	r.space(gap)
}

// note is a short muted line in italics.
func (r *renderer) note(s string) {
	r.begin(baseSize-1, style{italic: true, color: mutedColor})
	r.text(s)
	r.endLine()
	r.space(paraGap)
}

func (r *renderer) rule() {
	r.space(paraGap)
	y := r.f.GetY()
	r.f.SetDrawColor(ruleColor[0], ruleColor[1], ruleColor[2])
	r.f.SetLineWidth(0.5)
	r.f.Line(r.left(), y, r.left()+r.width(), y)
	r.f.SetY(y + paraGap*1.5)
	r.f.SetX(r.left())
}

func (r *renderer) inlines(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		r.inline(c)
	}
}

func (r *renderer) inline(n ast.Node) {
	switch n := n.(type) {
	case *ast.Text:
		v := n.Value(r.src)
		if !n.IsRaw() {
			v = unescape(v)
		}
		r.text(string(v))
		switch {
		case n.HardLineBreak():
			r.f.Ln(r.lh)
		case n.SoftLineBreak():
			r.text(" ")
		}
	case *ast.String:
		r.text(string(n.Value))
	case *ast.CodeSpan:
		saved := r.st
		r.st.code = true
		r.inlines(n)
		r.st = saved
	case *ast.Emphasis:
		saved := r.st
		if n.Level >= 2 {
			r.st.bold = true
		} else {
			r.st.italic = true
		}
		r.inlines(n)
		r.st = saved
	case *ast.Link:
		saved := r.st
		r.st.link = string(n.Destination)
		r.inlines(n)
		r.st = saved
	case *ast.AutoLink:
		saved := r.st
		r.st.link = string(n.URL(r.src))
		r.text(string(n.Label(r.src)))
		r.st = saved
	case *ast.Image:
		r.image(n)
	case *ast.RawHTML:
		// Skipped, as in the preview.
	case *east.TaskCheckBox:
		r.checkbox(n.IsChecked)
	default: // strikethrough and the like: their text
		r.inlines(n)
	}
}

// unescape resolves backslash escapes and character references, as an
// HTML renderer would.
func unescape(v []byte) []byte {
	return util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v)))
}

// text writes a run of inline text in the current style, wrapping at the
// right margin.
func (r *renderer) text(s string) {
	if s == "" {
		return
	}
	c := r.st.color
	family, fontStyle, size := "sans", "", r.size
	switch {
	case r.st.code:
		family, size, c = "mono", r.size*0.92, codeColor
	default:
		if r.st.bold {
			fontStyle += "B"
		}
		if r.st.italic {
			fontStyle += "I"
		}
	}
	if r.st.link != "" {
		c = linkColor
	}
	r.f.SetTextColor(c[0], c[1], c[2])
	for _, run := range r.split(s, family, fontStyle) {
		r.f.SetFont(run.family, run.style, size)
		if r.st.link != "" {
			r.f.WriteLinkString(r.lh, run.text, r.st.link)
		} else {
			r.f.Write(r.lh, run.text)
		}
	}
}

// run is text set in one font.
type run struct{ text, family, style string }

type covKey struct {
	font string
	r    rune
}

// has reports whether a font has a glyph for r.
func (r *renderer) has(font string, ch rune) bool {
	k := covKey{font, ch}
	ok, seen := r.covered[k]
	if !seen {
		i, err := r.cmaps[font].GlyphIndex(&r.buf, ch)
		ok = err == nil && i != 0
		r.covered[k] = ok
	}
	return ok
}

// split cuts s into runs by the font that has each character: family
// first, then Noto Sans for code, then Noto Sans Math (arrows, operators,
// ∇ and ∑). Characters beyond the Basic Multilingual Plane, which fpdf
// can't set, become U+FFFD.
func (r *renderer) split(s, family, fontStyle string) []run {
	var out []run
	for _, ch := range s {
		if ch > 0xFFFF {
			ch = '\uFFFD'
		}
		fam, st := family, fontStyle
		if !unicode.IsSpace(ch) && !r.has(family, ch) {
			switch {
			case family == "mono" && r.has("sans", ch):
				fam, st = "sans", ""
			case r.has("math", ch):
				fam, st = "math", ""
			}
		}
		if n := len(out); n > 0 && out[n-1].family == fam && out[n-1].style == st {
			out[n-1].text += string(ch)
			continue
		}
		out = append(out, run{text: string(ch), family: fam, style: st})
	}
	return out
}

// cell sets one line of text at (x, y), aligned within w, falling back
// font by font as split does.
func (r *renderer) cell(s, family, fontStyle string, size, x, y, w, h float64, align string) {
	runs := r.split(s, family, fontStyle)
	widths := make([]float64, len(runs))
	total := 0.0
	for i, run := range runs {
		r.f.SetFont(run.family, run.style, size)
		widths[i] = r.f.GetStringWidth(run.text)
		total += widths[i]
	}
	switch align {
	case "C":
		x += (w - total) / 2
	case "R":
		x += w - total
	}
	for i, run := range runs {
		r.f.SetFont(run.family, run.style, size)
		r.f.SetXY(x, y)
		r.f.CellFormat(widths[i], h, run.text, "", 0, "L", false, 0, "")
		x += widths[i]
	}
}

func (r *renderer) checkbox(checked bool) {
	const box = 3.0
	x, y := r.f.GetX(), r.f.GetY()+(r.lh-box)/2
	r.f.SetDrawColor(mutedColor[0], mutedColor[1], mutedColor[2])
	r.f.SetLineWidth(0.3)
	r.f.Rect(x, y, box, box, "D")
	if checked {
		r.f.Line(x+0.6, y+1.6, x+1.3, y+2.4)
		r.f.Line(x+1.3, y+2.4, x+2.5, y+0.6)
	}
	r.f.SetX(x + box + 1.6)
}

// image draws a picture on its own line, scaled to fit the text width.
func (r *renderer) image(n *ast.Image) {
	alt := plainText(n, r.src)
	data, kind := r.loadImage(string(n.Destination))
	if data == nil {
		saved := r.st
		r.st.italic, r.st.color = true, mutedColor
		r.text("[image: " + cmpString(alt, string(n.Destination)) + "]")
		r.st = saved
		return
	}
	r.images++
	name := fmt.Sprintf("img%d", r.images)
	info := r.f.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: kind}, bytes.NewReader(data))
	if r.f.Err() || info == nil {
		r.f.ClearError()
		r.text("[image: " + cmpString(alt, string(n.Destination)) + "]")
		return
	}
	info.SetDpi(96)
	w, h := info.Extent()
	if maxW := r.width(); w > maxW {
		w, h = maxW, h*maxW/w
	}
	if maxH := (r.bottom() - marginTop) * 0.85; h > maxH {
		w, h = w*maxH/h, maxH
	}
	r.endLine()
	r.f.ImageOptions(name, r.left(), -1, w, h, true, fpdf.ImageOptions{ImageType: kind}, 0, "")
	r.f.SetX(r.left())
}

// loadImage reads and checks an image the caller allows, as PNG or JPEG
// bytes for fpdf; nil when it can't be drawn.
func (r *renderer) loadImage(dest string) ([]byte, string) {
	if r.o.ReadImage == nil || dest == "" || strings.Contains(dest, ":") {
		return nil, "" // remote (http:, data:) or nothing to read with
	}
	data, err := r.o.ReadImage(dest)
	if err != nil {
		return nil, ""
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, ""
	}
	if format == "jpeg" && http.DetectContentType(data) == "image/jpeg" {
		return data, "JPG"
	}
	// PNG and GIF are re-encoded: fpdf reads plain PNG only (no
	// interlacing, no 16-bit palettes).
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ""
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, ""
	}
	return buf.Bytes(), "PNG"
}

func cmpString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (r *renderer) lines(n ast.Node) []string {
	var b strings.Builder
	l := n.Lines()
	for i := range l.Len() {
		seg := l.At(i)
		b.Write(seg.Value(r.src))
	}
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}

type segment struct {
	text  string
	color [3]int
}

// code draws a code block: monospace on a tinted panel, coloured by
// chroma, long lines wrapped.
func (r *renderer) code(src []string, lang string) {
	r.f.SetFont("mono", "", codeSize)
	lh := codeSize * ptToMM * 1.4
	w := r.width()
	perLine := max(8, int((w-2*codePad)/r.f.GetStringWidth("M")))
	lines := wrapSegments(highlight(strings.Join(src, "\n"), lang), perLine)

	saved := r.f.GetCellMargin()
	r.f.SetCellMargin(0)
	x0 := r.left()
	y := r.f.GetY()
	top := true
	for i, line := range lines {
		padTop, padBot := 0.0, 0.0
		if top {
			padTop = codePad
		}
		if i == len(lines)-1 {
			padBot = codePad
		}
		if y+padTop+lh+padBot > r.bottom() {
			r.f.AddPage()
			y, padTop, top = r.f.GetY(), codePad, true
		}
		top = false
		r.f.SetFillColor(codeFill[0], codeFill[1], codeFill[2])
		r.f.Rect(x0, y, w, padTop+lh+padBot, "F")
		x := x0 + codePad
		y += padTop
		for _, s := range line {
			r.f.SetTextColor(s.color[0], s.color[1], s.color[2])
			r.f.SetFont("mono", "", codeSize)
			sw := float64(utf8.RuneCountInString(s.text)) * r.f.GetStringWidth("M")
			r.cell(s.text, "mono", "", codeSize, x, y, sw, lh, "L")
			x += sw
		}
		y += lh + padBot
	}
	r.f.SetCellMargin(saved)
	r.f.SetXY(x0, y)
	r.space(paraGap + 1)
}

// highlight splits code into coloured runs (GitHub's light colours).
func highlight(code, lang string) []segment {
	plain := []segment{{text: code, color: textColor}}
	lexer := lexers.Get(lang)
	if lang == "" || lexer == nil {
		return plain
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, code+"\n")
	if err != nil {
		return plain
	}
	st := styles.Get("github")
	var out []segment
	for tok := it(); tok != chroma.EOF; tok = it() {
		c := textColor
		if col := st.Get(tok.Type).Colour; col.IsSet() {
			c = [3]int{int(col.Red()), int(col.Green()), int(col.Blue())}
		}
		out = append(out, segment{text: tok.Value, color: c})
	}
	return out
}

// wrapSegments lays coloured runs out in lines of at most perLine
// characters, tabs as four spaces.
func wrapSegments(segs []segment, perLine int) [][]segment {
	lines := [][]segment{nil}
	n := 0
	add := func(s string, c [3]int) {
		if s == "" {
			return
		}
		last := &lines[len(lines)-1]
		if k := len(*last); k > 0 && (*last)[k-1].color == c {
			(*last)[k-1].text += s
			return
		}
		*last = append(*last, segment{text: s, color: c})
	}
	for _, s := range segs {
		var run strings.Builder
		for _, ch := range strings.ReplaceAll(s.text, "\t", "    ") {
			if ch == '\n' {
				add(run.String(), s.color)
				run.Reset()
				lines, n = append(lines, nil), 0
				continue
			}
			if n == perLine {
				add(run.String(), s.color)
				run.Reset()
				lines, n = append(lines, nil), 0
			}
			run.WriteRune(ch)
			n++
		}
		add(run.String(), s.color)
	}
	if len(lines) > 1 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1] // the newline highlight adds
	}
	return lines
}

func (r *renderer) quote(n *ast.Blockquote) {
	l := r.left()
	r.f.SetLeftMargin(l + quoteIndent)
	r.f.SetX(l + quoteIndent)
	page0, y0 := r.f.PageNo(), r.f.GetY()
	saved := r.color
	r.color = mutedColor
	r.blocks(n)
	r.color = saved
	page1, y1 := r.f.PageNo(), r.f.GetY()-paraGap
	// The bar down the left, on each page the quote spans.
	r.f.SetDrawColor(ruleColor[0], ruleColor[1], ruleColor[2])
	r.f.SetLineWidth(0.9)
	for p := page0; p <= page1; p++ {
		r.f.SetPage(p)
		from, to := marginTop, r.bottom()
		if p == page0 {
			from = y0
		}
		if p == page1 {
			to = y1
		}
		if to > from {
			r.f.Line(l+1, from, l+1, to)
		}
	}
	r.f.SetPage(page1)
	r.f.SetLeftMargin(l)
	r.f.SetX(l)
}

var bullets = []string{"•", "–", "•"}

func (r *renderer) list(n *ast.List) {
	l := r.left()
	r.f.SetLeftMargin(l + listIndent)
	r.depth++
	num := n.Start
	for item := n.FirstChild(); item != nil; item = item.NextSibling() {
		r.begin(baseSize, style{})
		r.keep(r.lh)
		if !isTask(item) {
			marker := bullets[(r.depth-1)%len(bullets)]
			if n.IsOrdered() {
				marker = fmt.Sprintf("%d%c", num, n.Marker)
			}
			r.f.SetFont("sans", "", baseSize)
			r.f.SetTextColor(r.st.color[0], r.st.color[1], r.st.color[2])
			y := r.f.GetY()
			r.f.SetXY(l, y)
			r.f.CellFormat(listIndent-1.2, r.lh, marker, "", 0, "R", false, 0, "")
			r.f.SetXY(l+listIndent, y)
		} else {
			r.f.SetX(l + listIndent)
		}
		r.blocks(item)
		num++
	}
	r.depth--
	r.f.SetLeftMargin(l)
	r.f.SetX(l)
	if r.depth == 0 {
		r.space(paraGap)
	}
}

func isTask(item ast.Node) bool {
	if first := item.FirstChild(); first != nil {
		_, ok := first.FirstChild().(*east.TaskCheckBox)
		return ok
	}
	return false
}

// table draws a GFM table: columns fitted to their text, cells wrapped,
// the header repeated on each page it spans.
func (r *renderer) table(t *east.Table) {
	var rows [][]string
	header := 0
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []string
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			cells = append(cells, plainText(c, r.src))
		}
		if _, ok := row.(*east.TableHeader); ok {
			header++
		}
		rows = append(rows, cells)
	}
	cols := len(t.Alignments)
	if cols == 0 || len(rows) == 0 {
		return
	}
	widths := r.columnWidths(rows, header, cols)
	lh := lineHeight(tableSize) * 0.9
	saved := r.f.GetCellMargin()
	r.f.SetCellMargin(0)
	r.f.SetDrawColor(ruleColor[0], ruleColor[1], ruleColor[2])
	r.f.SetLineWidth(0.25)

	var drawRow func(cells []string, head bool)
	drawRow = func(cells []string, head bool) {
		fontStyle := ""
		if head {
			fontStyle = "B"
		}
		r.f.SetFont("sans", fontStyle, tableSize)
		wrapped := make([][]string, cols)
		n := 1
		for i := range cols {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			wrapped[i] = r.f.SplitText(cell, widths[i]-2*cellPad)
			n = max(n, len(wrapped[i]))
		}
		h := float64(n)*lh + 2*cellPad
		if r.f.GetY()+h > r.bottom() {
			r.f.AddPage()
			if !head && header > 0 {
				for _, hr := range rows[:header] {
					drawRow(hr, true)
				}
				r.f.SetFont("sans", fontStyle, tableSize)
			}
		}
		x, y := r.left(), r.f.GetY()
		for i := range cols {
			style := "D"
			if head {
				r.f.SetFillColor(headFill[0], headFill[1], headFill[2])
				style = "FD"
			}
			r.f.Rect(x, y, widths[i], h, style)
			r.f.SetTextColor(textColor[0], textColor[1], textColor[2])
			for j, line := range wrapped[i] {
				r.cell(line, "sans", fontStyle, tableSize, x+cellPad, y+cellPad+float64(j)*lh, widths[i]-2*cellPad, lh, align(t.Alignments[i]))
			}
			x += widths[i]
		}
		r.f.SetFont("sans", fontStyle, tableSize)
		r.f.SetXY(r.left(), y+h)
	}
	for i, row := range rows {
		drawRow(row, i < header)
	}
	r.f.SetCellMargin(saved)
	r.space(paraGap + 1)
}

func align(a east.Alignment) string {
	switch a {
	case east.AlignCenter:
		return "C"
	case east.AlignRight:
		return "R"
	}
	return "L"
}

// columnWidths fits columns to their text: as wide as their longest cell
// when the table fits, otherwise shared out in proportion, never narrower
// than their longest word.
func (r *renderer) columnWidths(rows [][]string, header, cols int) []float64 {
	natural := make([]float64, cols)
	least := make([]float64, cols)
	for i, row := range rows {
		st := ""
		if i < header {
			st = "B"
		}
		r.f.SetFont("sans", st, tableSize)
		for c := range min(cols, len(row)) {
			natural[c] = max(natural[c], r.f.GetStringWidth(row[c])+2*cellPad+0.5)
			for word := range strings.FieldsSeq(row[c]) {
				least[c] = max(least[c], r.f.GetStringWidth(word)+2*cellPad+0.5)
			}
		}
	}
	total := r.width()
	sum := 0.0
	for _, w := range natural {
		sum += w
	}
	if sum <= total {
		return natural
	}
	out := make([]float64, cols)
	used := 0.0
	for i := range natural {
		out[i] = max(min(least[i], total/float64(cols)), natural[i]*total/sum)
		used += out[i]
	}
	for i := range out { // scale back if the minimums overflowed
		out[i] *= total / used
	}
	return out
}

// plainText is the text of a node's inlines, without formatting.
func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				v := c.Value(src)
				if !c.IsRaw() {
					v = unescape(v)
				}
				b.Write(v)
				if c.SoftLineBreak() || c.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(c.Value)
			case *ast.AutoLink:
				b.Write(c.Label(src))
			case *ast.RawHTML:
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// ErrNotMarkdown is returned for input that isn't text.
var ErrNotMarkdown = errors.New("not a text file")

// CheckText reports whether data looks like text (valid UTF-8, no NULs).
func CheckText(data []byte) error {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return ErrNotMarkdown
	}
	return nil
}
