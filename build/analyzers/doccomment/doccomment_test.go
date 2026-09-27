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
	"golang.org/x/tools/go/analysis"
)

// run parses and type-checks one test package and returns the analyzer's
// findings and the ones its want comments expect, as "line: message".
func run(t *testing.T, dir string) (got, want []string) {
	t.Helper()
	fset := token.NewFileSet()
	names, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	if len(names) == 0 {
		t.Fatalf("no Go files in %s", dir)
	}
	var files []*ast.File
	wantRe := regexp.MustCompile("want `([^`]*)`")
	for _, n := range names {
		f, err := parser.ParseFile(fset, n, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if m := wantRe.FindStringSubmatch(c.Text); m != nil {
					want = append(want, lineMsg(fset, c.Pos(), m[1]))
				}
			}
		}
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check(filepath.Base(dir), fset, files, info)
	if err != nil {
		t.Fatal(err)
	}
	pass := &analysis.Pass{
		Analyzer: doccomment.Analyzer, Fset: fset, Files: files, Pkg: pkg, TypesInfo: info,
		Report: func(d analysis.Diagnostic) { got = append(got, lineMsg(fset, d.Pos, d.Message)) },
	}
	if _, err := doccomment.Analyzer.Run(pass); err != nil {
		t.Fatal(err)
	}
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
		dir := filepath.Join("testdata", "src", pkg)
		if _, err := os.Stat(dir); err != nil {
			t.Fatal(err)
		}
		got, want := run(t, dir)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s:\ngot:\n  %s\nwant:\n  %s", pkg, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
		}
	}
}
