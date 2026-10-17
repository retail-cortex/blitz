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

// Package textutil holds small string helpers shared by tools and the TUI.
package textutil

import (
	"strings"

	"golang.org/x/net/html"
)

// HTMLToText extracts readable text from HTML: scripts and styles are
// dropped, block elements become line breaks, headings and list items keep
// light Markdown markers, and links keep their absolute targets.
func HTMLToText(src string) string {
	z := html.NewTokenizer(strings.NewReader(src))
	var sb strings.Builder
	skip := 0
	var href string
	newline := func() {
		s := sb.String()
		if len(s) > 0 && !strings.HasSuffix(s, "\n") {
			sb.WriteString("\n")
		}
	}
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return collapseBlankLines(sb.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			switch tag {
			case "script", "style", "noscript", "svg", "template", "iframe":
				if tt == html.StartTagToken {
					skip++
				}
			case "br", "p", "div", "section", "article", "tr", "table", "pre", "blockquote", "hr":
				newline()
			case "h1", "h2", "h3", "h4", "h5", "h6":
				newline()
				sb.WriteString(strings.Repeat("#", int(tag[1]-'0')) + " ")
			case "li":
				newline()
				sb.WriteString("- ")
			case "a":
				href = ""
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					if string(k) == "href" && (strings.HasPrefix(string(v), "http://") || strings.HasPrefix(string(v), "https://")) {
						href = string(v)
					}
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch tag := string(name); tag {
			case "script", "style", "noscript", "svg", "template", "iframe":
				if skip > 0 {
					skip--
				}
			case "p", "div", "section", "article", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr", "pre", "blockquote":
				newline()
			case "a":
				if href != "" {
					sb.WriteString(" (" + href + ")")
					href = ""
				}
			}
		case html.TextToken:
			if skip > 0 {
				continue
			}
			text := strings.Join(strings.Fields(string(z.Text())), " ")
			if text == "" {
				continue
			}
			if s := sb.String(); len(s) > 0 && !strings.HasSuffix(s, "\n") && !strings.HasSuffix(s, " ") {
				sb.WriteString(" ")
			}
			sb.WriteString(text)
		}
	}
}

func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " ")
		if l == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
