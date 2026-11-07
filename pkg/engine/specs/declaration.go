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

package specs

import (
	"path"
	"regexp"
	"strings"
)

// Symbol is a declaration a requirement's evidence sits in: the function,
// method, type or class whose uses an impact scan follows (SDD-20).
type Symbol struct {
	Name string
	Path string
	// Line and Column are the name's (1-based; the column in characters).
	Line   int
	Column int
}

// declarations match a line declaring a symbol, by language; the named
// group is the name.
var declarations = map[string][]*regexp.Regexp{
	"go": {
		regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s*)?(?P<name>[A-Za-z_]\w*)\s*[\[(]`),
		regexp.MustCompile(`^\s*type\s+(?P<name>[A-Za-z_]\w*)\s`),
	},
	"python": {
		regexp.MustCompile(`^\s*(?:async\s+)?def\s+(?P<name>[A-Za-z_]\w*)\s*\(`),
		regexp.MustCompile(`^\s*class\s+(?P<name>[A-Za-z_]\w*)\b`),
	},
	"js": {
		regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*(?P<name>[A-Za-z_$][\w$]*)\s*[<(]`),
		regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+(?P<name>[A-Za-z_$][\w$]*)\b`),
		regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+(?P<name>[A-Za-z_$][\w$]*)\s*(?::[^=]+)?=\s*(?:async\s+)?(?:\([^)]*\)|[A-Za-z_$][\w$]*)\s*(?::[^=]+)?=>`),
		regexp.MustCompile(`^\s*(?:export\s+)?(?:interface|type)\s+(?P<name>[A-Za-z_$][\w$]*)\b`),
		regexp.MustCompile(`^\s*(?:(?:public|private|protected|static|async|readonly)\s+)*(?P<name>[A-Za-z_$][\w$]*)\s*\([^)]*\)\s*(?::[^{]+)?\{\s*$`),
	},
	"rust": {
		regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?fn\s+(?P<name>[A-Za-z_]\w*)`),
		regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:struct|enum|trait)\s+(?P<name>[A-Za-z_]\w*)`),
	},
}

// languageOf is the declarations' language for p ("" for none).
func languageOf(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		return "js"
	case ".rs":
		return "rust"
	}
	return ""
}

// notNames are words the method pattern takes for names but aren't.
var notNames = map[string]bool{"if": true, "for": true, "while": true, "switch": true, "catch": true, "function": true, "return": true}

// declared is the symbol line declares in language, if it does.
func declared(language, line string) (string, int, bool) {
	for _, re := range declarations[language] {
		m := re.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		i := re.SubexpIndex("name")
		name := line[m[2*i]:m[2*i+1]]
		if notNames[name] {
			continue
		}
		return name, len([]rune(line[:m[2*i]])) + 1, true
	}
	return "", 0, false
}

// commentRE is a line that's only a comment.
var commentRE = regexp.MustCompile(`^\s*(//|#|/\*|\*|--)`)

// declarationLookback is how far up a declaration is looked for.
const declarationLookback = 200

// Declaration is the symbol line (1-based) of p (its text) belongs to: the
// declaration the comment it's in documents (a few lines below), else the
// nearest one above it. ok is false for a language it can't read, or none.
func Declaration(p, text string, line int) (Symbol, bool) {
	lang := languageOf(p)
	lines := strings.Split(text, "\n")
	if lang == "" || line < 1 || line > len(lines) {
		return Symbol{}, false
	}
	at := func(i int) (Symbol, bool) {
		if name, col, ok := declared(lang, lines[i]); ok {
			return Symbol{Name: name, Path: p, Line: i + 1, Column: col}, true
		}
		return Symbol{}, false
	}
	i := line - 1
	if s, ok := at(i); ok {
		return s, true
	}
	if commentRE.MatchString(lines[i]) {
		for j := i + 1; j < len(lines) && j <= i+6; j++ {
			if s, ok := at(j); ok {
				return s, true
			}
			if !commentRE.MatchString(lines[j]) && strings.TrimSpace(lines[j]) != "" && !strings.HasPrefix(strings.TrimSpace(lines[j]), "@") {
				break
			}
		}
	}
	for j := i - 1; j >= 0 && j >= i-declarationLookback; j-- {
		if s, ok := at(j); ok {
			return s, true
		}
	}
	return Symbol{}, false
}

// Uses are the lines of p (its text) that use name as a whole word, but
// its declaration (decl, 0 for none): the text search an impact scan
// falls back on without a language server.
func Uses(p, text, name string, decl int) []Evidence {
	re := regexp.MustCompile(`(?:^|[^\w$])` + regexp.QuoteMeta(name) + `(?:$|[^\w$])`)
	var out []Evidence
	if !strings.Contains(text, name) {
		return nil
	}
	kind := EvidenceKind(p)
	for i, line := range strings.Split(text, "\n") {
		if i+1 != decl && re.MatchString(line) {
			out = append(out, Evidence{Path: p, Line: i + 1, Kind: kind, Text: clip(strings.TrimSpace(line), 160)})
		}
	}
	return out
}
