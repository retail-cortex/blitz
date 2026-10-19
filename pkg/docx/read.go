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
	"fmt"
	"io"
	"strings"
)

// ErrNotDocx means the bytes aren't a Word document (a zip with
// word/document.xml).
var ErrNotDocx = errors.New("not a Word document")

// maxDocumentXML bounds how much of word/document.xml is read.
const maxDocumentXML = 64 << 20

// ToMarkdown reads a Word document's text as Markdown, for a preview:
// paragraphs, headings (Heading1–6, Title), bold and italic runs, bulleted
// and numbered list items, tables (their cells' text), hyperlinks' text and
// line breaks. Images, comments, headers and footers are left out.
func ToMarkdown(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotDocx, err)
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
		}
	}
	if doc == nil {
		return "", fmt.Errorf("%w: no word/document.xml", ErrNotDocx)
	}
	rc, err := doc.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	return readBody(xml.NewDecoder(io.LimitReader(rc, maxDocumentXML)))
}

// readPara is a paragraph as read: its style, whether it's a list item, and
// its text.
type readPara struct {
	style string
	list  bool
	text  strings.Builder
}

func readBody(dec *xml.Decoder) (string, error) {
	var out []string
	var p *readPara
	var bold, italic, inText bool
	// A table: its rows of cells, each cell's paragraphs joined.
	var table [][]string
	var row []string
	var cell []string
	depth := 0 // table nesting: inner tables' text joins their cell
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrNotDocx, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "tbl":
				depth++
				if depth == 1 {
					table = nil
				}
			case "tr":
				if depth == 1 {
					row = nil
				}
			case "tc":
				if depth == 1 {
					cell = nil
				}
			case "p":
				p = &readPara{}
			case "pStyle":
				if p != nil {
					p.style = attr(t, "val")
				}
			case "numPr":
				if p != nil {
					p.list = true
				}
			case "r":
				bold, italic = false, false
			case "b":
				bold = attr(t, "val") != "0" && attr(t, "val") != "false"
			case "i":
				italic = attr(t, "val") != "0" && attr(t, "val") != "false"
			case "t":
				inText = true
			case "br":
				if p != nil {
					p.text.WriteString("  \n")
				}
			case "tab":
				if p != nil {
					p.text.WriteString("\t")
				}
			}
		case xml.CharData:
			if inText && p != nil {
				s := string(t)
				switch {
				case bold && italic && strings.TrimSpace(s) != "":
					s = "***" + s + "***"
				case bold && strings.TrimSpace(s) != "":
					s = "**" + s + "**"
				case italic && strings.TrimSpace(s) != "":
					s = "*" + s + "*"
				}
				p.text.WriteString(s)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if p == nil {
					continue
				}
				line := paraMarkdown(p)
				if depth > 0 {
					cell = append(cell, strings.TrimSpace(p.text.String()))
				} else if line != "" {
					out = append(out, line)
				}
				p = nil
			case "tc":
				if depth == 1 {
					row = append(row, strings.Join(cell, " "))
				}
			case "tr":
				if depth == 1 {
					table = append(table, row)
				}
			case "tbl":
				if depth == 1 {
					out = append(out, tableMarkdown(table))
				}
				depth--
			}
		}
	}
	return strings.Join(out, "\n\n") + "\n", nil
}

func paraMarkdown(p *readPara) string {
	text := strings.TrimRight(p.text.String(), " \t")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	style := strings.ToLower(p.style)
	switch {
	case style == "title":
		return "# " + text
	case strings.HasPrefix(style, "heading") && len(style) == len("heading")+1 && style[len(style)-1] >= '1' && style[len(style)-1] <= '6':
		return strings.Repeat("#", int(style[len(style)-1]-'0')) + " " + text
	case p.list || strings.Contains(style, "list"):
		return "- " + text
	}
	return text
}

func tableMarkdown(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	line := func(cells []string) string {
		parts := make([]string, width)
		for i := range parts {
			if i < len(cells) {
				parts[i] = strings.ReplaceAll(cells[i], "|", `\|`)
			}
		}
		return "| " + strings.Join(parts, " | ") + " |"
	}
	lines := []string{line(rows[0]), "|" + strings.Repeat(" --- |", width)}
	for _, r := range rows[1:] {
		lines = append(lines, line(r))
	}
	return strings.Join(lines, "\n")
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
