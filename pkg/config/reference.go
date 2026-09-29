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

package config

import (
	"embed"
	"fmt"
	"go/ast"
	"go/doc/comment"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"sync"
)

// The files that declare the settings: their doc comments are the
// reference's descriptions, so it can't drift from the code.
//
//go:embed config.go features.go modelsettings.go
var settingsSources embed.FS

// Setting is one entry of the settings reference: a table ([llm]) or a
// setting in one (llm.provider). Keys with <name> stand for any name, as
// in model_settings.<model>.temperature; [] marks an array of tables
// (hooks.pre_tool[].command).
type Setting struct {
	Key string
	// Type is table, string, integer, number, boolean or list of strings.
	Type string
	// Default is the value when unset, as TOML ("" when there's none to
	// show, as for API keys and tables).
	Default string
	// Doc says what it does, from the doc comment of its field or type.
	Doc string
}

// mapNames are the placeholders for map keys, by the map's key.
var mapNames = map[string]string{"agent_models": "<agent>", "model_settings": "<model>", "pricing": "<model>", "env": "<variable>"}

// Reference is every setting a settings file can have, in the order of
// the Config struct, with its type, default and description.
var Reference = sync.OnceValue(func() []Setting {
	docs := fieldDocs()
	def := DefaultConfig()
	home, _ := os.UserHomeDir()
	var out []Setting
	var walk func(prefix string, t reflect.Type, v reflect.Value)
	walk = func(prefix string, t reflect.Type, v reflect.Value) {
		for i := range t.NumField() {
			f := t.Field(i)
			tag, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
			if tag == "" || tag == "-" {
				continue
			}
			key := prefix + tag
			doc := docs[t.Name()+"."+f.Name]
			var fv reflect.Value
			if v.IsValid() {
				fv = v.Field(i)
			}
			ft := f.Type
			switch {
			case ft.Kind() == reflect.Struct:
				out = append(out, Setting{Key: key, Type: "table", Doc: join(doc, docs[ft.Name()])})
				walk(key+".", ft, fv)
			case ft.Kind() == reflect.Map:
				name := mapNames[tag]
				if name == "" {
					name = "<name>"
				}
				elem := ft.Elem()
				if elem.Kind() == reflect.Struct {
					out = append(out, Setting{Key: key, Type: "table", Doc: join(doc, docs[elem.Name()])})
					out = append(out, Setting{Key: key + "." + name, Type: "table", Doc: docs[elem.Name()]})
					walk(key+"."+name+".", elem, reflect.Value{})
				} else {
					out = append(out, Setting{Key: key, Type: "table", Doc: doc}, Setting{Key: key + "." + name, Type: typeName(elem), Doc: doc})
				}
			case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct:
				out = append(out, Setting{Key: key + "[]", Type: "array of tables", Doc: join(doc, docs[ft.Elem().Name()])})
				walk(key+"[].", ft.Elem(), reflect.Value{})
			default:
				s := Setting{Key: key, Type: typeName(ft), Doc: doc}
				if fv.IsValid() && !strings.HasSuffix(tag, "api_key") {
					s.Default = strings.ReplaceAll(tomlValue(fv), home, "~")
				}
				out = append(out, s)
			}
		}
	}
	walk("", reflect.TypeFor[Config](), reflect.ValueOf(*def))
	return out
})

func join(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	return a + " " + b
}

func typeName(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int64:
		return "integer"
	case reflect.Float64:
		return "number"
	case reflect.Slice:
		return "list of " + typeName(t.Elem()) + "s"
	}
	return t.String()
}

// tomlValue writes a value as TOML ("" for unset pointers).
func tomlValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return ""
		}
		return tomlValue(v.Elem())
	case reflect.String:
		return fmt.Sprintf("%q", v.String())
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = tomlValue(v.Index(i))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return fmt.Sprint(v.Interface())
}

// fieldDocs are the doc comments in settingsSources, as text: of each
// struct type ("LLMConfig") and field ("LLMConfig.Provider"), a field's
// line comment when it has no doc comment. A comment above a group of
// fields (no blank line between them) documents each in the group that has
// none of its own.
func fieldDocs() map[string]string {
	out := map[string]string{}
	fset := token.NewFileSet()
	entries, _ := settingsSources.ReadDir(".")
	for _, e := range entries {
		src, _ := settingsSources.ReadFile(e.Name())
		f, err := parser.ParseFile(fset, e.Name(), src, parser.ParseComments)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.TYPE {
				continue
			}
			for _, spec := range g.Specs {
				ts := spec.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				typeDoc := ts.Doc
				if typeDoc == nil {
					typeDoc = g.Doc
				}
				out[ts.Name.Name] = text(typeDoc)
				prevDoc, prevLine := "", 0
				for _, fld := range st.Fields.List {
					doc := text(fld.Doc)
					if doc == "" {
						doc = text(fld.Comment)
					}
					line := fset.Position(fld.Pos()).Line
					if doc == "" && fld.Doc == nil && line == prevLine+1 && !namedType(fld.Type) {
						doc = prevDoc // the group's comment
					}
					prevDoc, prevLine = doc, fset.Position(fld.End()).Line
					for _, n := range fld.Names {
						out[ts.Name.Name+"."+n.Name] = doc
					}
				}
			}
		}
	}
	return out
}

// namedType reports whether t is one of the package's types (a table),
// which a group's comment doesn't document.
func namedType(t ast.Expr) bool {
	id, ok := t.(*ast.Ident)
	return ok && ast.IsExported(id.Name)
}

// text is a comment as one paragraph of plain text.
func text(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	var p comment.Parser
	var pr comment.Printer
	pr.TextWidth = -1
	return strings.Join(strings.Fields(string(pr.Text(p.Parse(g.Text())))), " ")
}
