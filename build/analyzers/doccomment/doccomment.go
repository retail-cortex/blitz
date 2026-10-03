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

// Package doccomment is a nogo analyzer: every package has a package
// comment, and every exported declaration a doc comment
// (spec_release_readiness_030 RR-12). A comment on a const, var or type
// group covers the names in it. Tests and generated files are exempt.
package doccomment

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer reports undocumented packages and exported declarations.
var Analyzer = &analysis.Analyzer{
	Name: "doccomment",
	Doc:  "every package has a package comment, and every exported declaration a doc comment",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	var files []*ast.File
	for _, f := range pass.Files {
		name := pass.Fset.Position(f.Package).Filename
		if strings.HasSuffix(name, "_test.go") || ast.IsGenerated(f) {
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, nil
	}
	documented := false
	for _, f := range files {
		if hasDoc(f.Doc) {
			documented = true
		}
	}
	if !documented {
		pass.Reportf(files[0].Package, "package %s has no package comment", pass.Pkg.Name())
	}
	for _, f := range files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !hasDoc(d.Doc) && exportedFunc(d) {
					pass.Reportf(d.Name.Pos(), "exported %s has no doc comment", funcName(d))
				}
			case *ast.GenDecl:
				checkGen(pass, d)
			}
		}
	}
	return nil, nil
}

// hasDoc reports whether the comment has text, not only directives such as
// //go:embed (which CommentGroup.Text leaves out, as the doc tools do).
func hasDoc(cg *ast.CommentGroup) bool {
	return cg != nil && strings.TrimSpace(cg.Text()) != ""
}

// exportedFunc reports whether d is an exported function, or an exported
// method of an exported type.
func exportedFunc(d *ast.FuncDecl) bool {
	if !d.Name.IsExported() {
		return false
	}
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return true
	}
	t := d.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
			continue
		case *ast.IndexExpr:
			t = x.X
			continue
		case *ast.IndexListExpr:
			t = x.X
			continue
		case *ast.Ident:
			return x.IsExported()
		}
		return false
	}
}

func funcName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return "func " + d.Name.Name
	}
	return "method " + d.Name.Name
}

// checkGen checks a const, var or type declaration: a doc on the group
// covers every name in it.
func checkGen(pass *analysis.Pass, d *ast.GenDecl) {
	if d.Tok == token.IMPORT || hasDoc(d.Doc) {
		return
	}
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if s.Name.IsExported() && !hasDoc(s.Doc) {
				pass.Reportf(s.Name.Pos(), "exported type %s has no doc comment", s.Name.Name)
			}
		case *ast.ValueSpec:
			if hasDoc(s.Doc) || hasDoc(s.Comment) {
				continue
			}
			for _, n := range s.Names {
				if n.IsExported() {
					pass.Reportf(n.Pos(), "exported %s %s has no doc comment", d.Tok, n.Name)
					break
				}
			}
		}
	}
}
