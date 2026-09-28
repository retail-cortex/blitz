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
// scripts), product names and key names.
var lintAllowed = []string{"Blitz Go (Google ADK) version", "Blitz Go", "Ctrl+C"}

// TestNoUntranslatedOutput fails when user-facing output in the REPL is a
// literal English string instead of an i18n.T lookup. Doctor, config and
// CLI errors are deliberately English and not scanned.
func TestNoUntranslatedOutput(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	files = append(files, "../../main.go")
	printers := map[string]bool{"Print": true, "Println": true, "Printf": true, "Fprint": true, "Fprintln": true, "Fprintf": true}
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
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				s, _ := strconv.Unquote(lit.Value)
				for _, a := range lintAllowed {
					s = strings.ReplaceAll(s, a, "")
				}
				assert.False(t, wordRE.MatchString(s), "%s: untranslated output %s; use i18n.T", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	}
}
