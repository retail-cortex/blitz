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
	"fmt"
	"strings"
	"time"
)

// The package's fixed parts: content types, relationships, styles,
// numbering and properties. The document's own part is written from the
// body the writer built.

const (
	nsW   = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsR   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPkg = "http://schemas.openxmlformats.org/package/2006/relationships"
	xmlHd = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
)

const contentTypes = xmlHd + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Default Extension="png" ContentType="image/png"/>` +
	`<Default Extension="jpeg" ContentType="image/jpeg"/>` +
	`<Default Extension="gif" ContentType="image/gif"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>` +
	`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
	`<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>` +
	`</Types>`

const packageRels = xmlHd + `<Relationships xmlns="` + nsPkg + `">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
	`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>` +
	`</Relationships>`

const appProps = xmlHd + `<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Blitz</Application></Properties>`

// styles are Word's built-in names, so a user's template restyles them.
const styles = xmlHd + `<w:styles xmlns:w="` + nsW + `">` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/><w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="en-US"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="264" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
	`%s` + // headings
	`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:pBdr><w:left w:val="single" w:sz="18" w:space="8" w:color="BFBFBF"/></w:pBdr><w:ind w:left="567"/></w:pPr><w:rPr><w:color w:val="595959"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="60"/><w:ind w:left="720"/><w:contextualSpacing/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:customStyle="1" w:styleId="Code"><w:name w:val="Code"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:shd w:val="clear" w:color="auto" w:fill="F3F3F3"/><w:spacing w:after="0" w:line="240" w:lineRule="auto"/><w:contextualSpacing/></w:pPr><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:sz w:val="19"/><w:szCs w:val="19"/></w:rPr></w:style>` +
	`<w:style w:type="character" w:customStyle="1" w:styleId="CodeChar"><w:name w:val="Code Char"/><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:color w:val="A3155E"/><w:sz w:val="20"/><w:shd w:val="clear" w:color="auto" w:fill="F3F3F3"/></w:rPr></w:style>` +
	`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:rPr><w:color w:val="0B57D0"/><w:u w:val="single"/></w:rPr></w:style>` +
	`<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:tblPr><w:tblBorders>` +
	`<w:top w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/><w:left w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/><w:right w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="BFBFBF"/>` +
	`</w:tblBorders><w:tblCellMar><w:left w:w="108" w:type="dxa"/><w:right w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>` +
	`</w:styles>`

// headingSizes are Heading 1–6's sizes, in half points.
var headingSizes = [6]int{36, 30, 26, 24, 22, 22}

func headingStyles() string {
	var b strings.Builder
	for i, sz := range headingSizes {
		fmt.Fprintf(&b, `<w:style w:type="paragraph" w:styleId="Heading%d"><w:name w:val="heading %d"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="9"/><w:qFormat/>`+
			`<w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="%d" w:after="120"/><w:outlineLvl w:val="%d"/></w:pPr>`+
			`<w:rPr><w:b/><w:color w:val="1F1F1F"/><w:sz w:val="%d"/><w:szCs w:val="%d"/></w:rPr></w:style>`,
			i+1, i+1, 360-40*i, i, sz, sz)
	}
	return b.String()
}

// bullets are the marks of each list level, cycling.
var bullets = []string{"•", "◦", "▪"}

// numbering defines bullet lists (abstract 0, list 1) and numbered lists
// (abstract 1), with an instance per numbered list so each counts from
// its own start.
func (w *writer) numbering() string {
	var b strings.Builder
	b.WriteString(xmlHd + `<w:numbering xmlns:w="` + nsW + `">`)
	for abs, ordered := range []bool{false, true} {
		fmt.Fprintf(&b, `<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="hybridMultilevel"/>`, abs)
		for l := range 9 {
			format, text := "bullet", bullets[l%len(bullets)]
			if ordered {
				format, text = []string{"decimal", "lowerLetter", "lowerRoman"}[l%3], fmt.Sprintf("%%%d.", l+1)
			}
			fmt.Fprintf(&b, `<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%s"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr></w:lvl>`,
				l, format, escape(text), 720*(l+1))
		}
		b.WriteString(`</w:abstractNum>`)
	}
	b.WriteString(`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`)
	for i, l := range w.lists {
		fmt.Fprintf(&b, `<w:num w:numId="%d"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="%d"><w:startOverride w:val="%d"/></w:lvlOverride></w:num>`, i+2, l.level, max(l.start, 0))
	}
	b.WriteString(`</w:numbering>`)
	return b.String()
}

func (w *writer) document(size [2]int) string {
	var b strings.Builder
	b.WriteString(xmlHd + `<w:document xmlns:w="` + nsW + `" xmlns:r="` + nsR + `"` +
		` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"` +
		` xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body>`)
	b.WriteString(w.body.String())
	fmt.Fprintf(&b, `<w:sectPr><w:pgSz w:w="%d" w:h="%d"/><w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>`,
		size[0], size[1], margin, margin, margin, margin)
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

func (w *writer) documentRels() string {
	var b strings.Builder
	b.WriteString(xmlHd + `<Relationships xmlns="` + nsPkg + `">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>`)
	for _, r := range w.rels {
		mode := ""
		if r.external {
			mode = ` TargetMode="External"`
		}
		fmt.Fprintf(&b, `<Relationship Id="%s" Type="%s" Target="%s"%s/>`, r.id, r.kind, escape(r.target), mode)
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

func coreProps(title string, when time.Time) string {
	stamp := when.Format("2006-01-02T15:04:05Z")
	return xmlHd + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:title>` + escape(title) + `</dc:title><dc:creator>Blitz</dc:creator>` +
		`<dcterms:created xsi:type="dcterms:W3CDTF">` + stamp + `</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">` + stamp + `</dcterms:modified>` +
		`</cp:coreProperties>`
}

// pack zips the parts, in a fixed order with the document's time, so the
// same Markdown makes the same file.
func (w *writer) pack(title string, when time.Time, size [2]int) ([]byte, error) {
	parts := []struct {
		name string
		data []byte
	}{
		{"[Content_Types].xml", []byte(contentTypes)},
		{"_rels/.rels", []byte(packageRels)},
		{"docProps/core.xml", []byte(coreProps(title, when))},
		{"docProps/app.xml", []byte(appProps)},
		{"word/document.xml", []byte(w.document(size))},
		{"word/styles.xml", []byte(fmt.Sprintf(styles, headingStyles()))},
		{"word/numbering.xml", []byte(w.numbering())},
		{"word/_rels/document.xml.rels", []byte(w.documentRels())},
	}
	for _, m := range w.media {
		parts = append(parts, struct {
			name string
			data []byte
		}{"word/media/" + m.name, m.data})
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, p := range parts {
		f, err := z.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Deflate, Modified: when})
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(p.data); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
