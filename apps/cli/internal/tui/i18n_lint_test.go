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

package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// English words in a printed literal mean text that bypasses the catalogs.
var wordRE = regexp.MustCompile(`[A-Za-z]{3,}`)

// Literals allowed to stay as they are: --version output (parsed by
// scripts), product names, key names, a rule kind and an AppleScript
// command (not shown).
var lintAllowed = []string{"Blitz Go (Google ADK) version", "Blitz Go", "Ctrl+C", "mcp:", "display notification %s with title %s"}

// English sources: doctor's report (English by policy, spec_020 CLI-30)
// and headless.go's stream-json protocol, which programs read.
var lintEnglish = map[string]bool{"doctor.go": true, "headless.go": true}

// TestNoUntranslatedOutput fails when user-facing output in the REPL or a
// subcommand (spec_020 CLI-30b) is a literal English string instead of an
// i18n.T lookup: an argument to a print or a Sprintf, or a piece of one
// joined with +. Doctor, --help, errors and stream-json are deliberately
// English: errors and --help aren't printed with fmt, so aren't scanned.
func TestNoUntranslatedOutput(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	cli, _ := filepath.Glob(filepath.Join("..", "..", "*.go"))
	require.Greater(t, len(cli), 20, "the CLI's sources aren't in the test's data")
	for _, f := range cli {
		if !lintEnglish[filepath.Base(f)] {
			files = append(files, f)
		}
	}
	printers := map[string]bool{"Print": true, "Println": true, "Printf": true, "Fprint": true, "Fprintln": true, "Fprintf": true, "Sprintf": true}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		require.NoError(t, err)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "fmt" || !printers[sel.Sel.Name] {
				return true
			}
			for _, arg := range call.Args {
				for _, lit := range literals(arg) {
					s, _ := strconv.Unquote(lit.Value)
					for _, a := range lintAllowed {
						s = strings.ReplaceAll(s, a, "")
					}
					assert.False(t, wordRE.MatchString(s), "%s: untranslated output %s; use i18n.T", fset.Position(lit.Pos()), lit.Value)
				}
			}
			return true
		})
	}
}

// literals are the string literals an argument is, or is joined from with +.
func literals(e ast.Expr) []*ast.BasicLit {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			return []*ast.BasicLit{e}
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return append(literals(e.X), literals(e.Y)...)
		}
	case *ast.ParenExpr:
		return literals(e.X)
	}
	return nil
}
