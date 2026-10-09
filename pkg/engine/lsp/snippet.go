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

package lsp

// Code blocks as documents (spec_visual_editor_037 §7): a block in a
// Markdown file is opened as a file of its own that's never written, and a
// Go fragment is made a whole file first, so the server can read it.

import (
	"path/filepath"
	"regexp"
	"strings"
)

// wrapping is what was added around a block's text for its server: lines
// before it (prefix) and whether its statements are inside a function.
type wrapping struct {
	prefix  int
	wrapped bool // statements inside func _() { … }
}

var (
	goDecl      = regexp.MustCompile(`^(package|import|func|type)\b`)
	goVarConst  = regexp.MustCompile(`^(var|const)\b`)
	goIdent     = regexp.MustCompile(`^[\p{L}_][\p{L}\p{N}_]*$`)
	goStatement = regexp.MustCompile(`(?m)^[\p{L}_][\p{L}\p{N}_]*(\s*(:=|=|\+\+|--|\(|\.)|\s*,)`)
)

// wrapGo makes a Go fragment a file: as it is when it has a package
// clause; after "package snippet" when it starts with declarations; and
// with its statements in "func _() { … }" otherwise.
func wrapGo(text string) (string, wrapping) {
	first := firstGoToken(text)
	switch {
	case strings.HasPrefix(first, "package"):
		return text, wrapping{}
	case goDecl.MatchString(first), goVarConst.MatchString(first) && !goStatement.MatchString(text):
		return "package snippet\n\n" + text, wrapping{prefix: 2}
	}
	return "package snippet\n\nfunc _() {\n" + text + "\n}\n", wrapping{prefix: 3, wrapped: true}
}

// firstGoToken is the fragment's first line that isn't blank or a comment.
func firstGoToken(text string) string {
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		if inBlock {
			if i := strings.Index(l, "*/"); i >= 0 {
				inBlock = false
				l = strings.TrimSpace(l[i+2:])
			} else {
				continue
			}
		}
		if strings.HasPrefix(l, "/*") {
			if i := strings.Index(l, "*/"); i >= 0 {
				l = strings.TrimSpace(l[i+2:])
			} else {
				inBlock = true
				continue
			}
		}
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		return l
	}
	return ""
}

// serverText is a document's text as its server gets it, and what was
// added: a Go code block made whole; anything else as it is.
func serverText(doc *Document) (string, wrapping) {
	if doc.snippet && strings.EqualFold(filepath.Ext(doc.Path), ".go") {
		return wrapGo(doc.text)
	}
	return doc.text, wrapping{}
}

// SnippetPath is where a code block's file is said to be: a folder of its
// own (so it's a package of its own) under the workspace's root, so it
// sees the workspace's module; never written to disk. Not under a folder
// whose name starts with "." or "_": gopls gives files there no
// diagnostics.
func SnippetPath(root, key, ext string) string {
	return filepath.Join(root, "blitz-snippets", key, "snippet"+ext)
}

// shift moves an edit from the server's text to the block's, reporting
// whether it's inside the block's lines (1 to lines).
func (w wrapping) shift(e *TextEdit, lines int) bool {
	e.Start.Line -= w.prefix
	e.End.Line -= w.prefix
	if e.Start.Line < 1 || e.Start.Line > lines {
		return false
	}
	if e.End.Line > lines {
		e.End = Position{Line: lines, Column: e.End.Column}
	}
	return true
}

// unimported says whether message is gopls's "undefined: name" for a name
// text uses as a package (name.Println): a fragment's missing import.
func unimported(message, text string) bool {
	name, ok := strings.CutPrefix(message, "undefined: ")
	if !ok || !goIdent.MatchString(name) {
		return false
	}
	return regexp.MustCompile(`(^|[^\w.])` + name + `\.\w`).MatchString(text)
}
