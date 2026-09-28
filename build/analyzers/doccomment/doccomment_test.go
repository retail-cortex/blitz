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

package doccomment_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/retail-cortex/blitz/build/analyzers/doccomment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis"
)

// run parses and type-checks one test package and returns the analyzer's
// findings and the ones its want comments expect, as "line: message".
func run(t *testing.T, dir string) (got, want []string) {
	t.Helper()
	fset := token.NewFileSet()
	names, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	require.NotEqual(t, 0, len(names), "no Go files in %s", dir)
	var files []*ast.File
	wantRe := regexp.MustCompile("want `([^`]*)`")
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			f, err := parser.ParseFile(fset, n, nil, parser.ParseComments)
			require.NoError(t, err)
			files = append(files, f)
			for _, cg := range f.Comments {
				for _, c := range cg.List {
					if m := wantRe.FindStringSubmatch(c.Text); m != nil {
						want = append(want, lineMsg(fset, c.Pos(), m[1]))
					}
				}
			}
		})
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check(filepath.Base(dir), fset, files, info)
	require.NoError(t, err)
	pass := &analysis.Pass{
		Analyzer: doccomment.Analyzer, Fset: fset, Files: files, Pkg: pkg, TypesInfo: info,
		Report: func(d analysis.Diagnostic) { got = append(got, lineMsg(fset, d.Pos, d.Message)) },
	}
	_, err = doccomment.Analyzer.Run(pass)
	require.NoError(t, err)
	sort.Strings(got)
	sort.Strings(want)
	return got, want
}

func lineMsg(fset *token.FileSet, pos token.Pos, msg string) string {
	p := fset.Position(pos)
	return filepath.Base(p.Filename) + ":" + strconv.Itoa(p.Line) + ": " + msg
}

func TestAnalyzer(t *testing.T) {
	for _, pkg := range []string{"a", "b"} {
		t.Run(pkg, func(t *testing.T) {
			dir := filepath.Join("testdata", "src", pkg)
			_, err := os.Stat(dir)
			require.NoError(t, err)
			got, want := run(t, dir)
			assert.Equal(t, strings.Join(want, "\n"), strings.Join(got, "\n"), "%s:\ngot:\n  %s\nwant:\n  %s", pkg, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
		})
	}
}
