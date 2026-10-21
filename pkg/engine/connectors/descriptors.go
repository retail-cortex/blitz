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

package connectors

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors/builtin"
)

// Folder is the connectors' folder, in the config folder and in a
// workspace's .agents: its kinds and categories folders hold descriptors,
// and a folder per category its connections.
const Folder = config.ConnectorsFolder

// Descriptor folders, in a connectors folder.
const (
	KindsFolder      = config.ConnectorKindsFolder
	CategoriesFolder = config.ConnectorCategoriesFolder
)

// Catalog is what connectors there are: the categories and their kinds,
// built-in first, then the global descriptors and the workspace's. A
// descriptor that can't be used is listed with its problem.
type Catalog struct {
	Categories []Category
	Kinds      []Kind
}

// GlobalDir is the global connectors folder.
func GlobalDir(prefixDir string) string { return config.GlobalConnectorsDir(prefixDir) }

// WorkspaceDir is a workspace's connectors folder.
func WorkspaceDir(workspace string) string { return config.WorkspaceConnectorsDir(workspace) }

// Dir is a scope's connectors folder ("" for a workspace scope with no
// workspace).
func Dir(prefixDir, workspace, scope string) string {
	if scope == ScopeGlobal {
		return GlobalDir(prefixDir)
	}
	if workspace == "" {
		return ""
	}
	return WorkspaceDir(workspace)
}

// Load reads the catalog for a workspace ("" for the global one alone).
// A global or workspace descriptor adds a category or a kind; one with
// the ID of one there already isn't used, so a workspace can't change
// what a built-in kind means. It's read again only when a descriptor
// file changes (catalogs).
func Load(prefixDir, workspace string) Catalog {
	key := prefixDir + "\x00" + workspace
	fp := Fingerprint(descriptorFiles(prefixDir, workspace))
	catalogs.Lock()
	defer catalogs.Unlock()
	if c, ok := catalogs.m[key]; ok && c.fingerprint == fp {
		return c.cat.clone()
	}
	if len(catalogs.m) >= maxCatalogs {
		clear(catalogs.m)
	}
	cat := load(prefixDir, workspace)
	catalogs.m[key] = cachedCatalog{fingerprint: fp, cat: cat}
	return cat.clone()
}

// catalogs are the catalogs read, by folders, with the fingerprint of
// their descriptor files then; at most maxCatalogs.
var catalogs = struct {
	sync.Mutex
	m map[string]cachedCatalog
}{m: map[string]cachedCatalog{}}

const maxCatalogs = 64

type cachedCatalog struct {
	fingerprint string
	cat         Catalog
}

// clone is cat with slices of its own, so a caller appending to them
// doesn't write into the cached ones.
func (cat Catalog) clone() Catalog {
	return Catalog{Categories: slices.Clone(cat.Categories), Kinds: slices.Clone(cat.Kinds)}
}

// descriptorFiles are the global and workspace descriptor files.
func descriptorFiles(prefixDir, workspace string) []string {
	var out []string
	for _, scope := range Scopes {
		if dir := Dir(prefixDir, workspace, scope); dir != "" {
			for _, folder := range []string{CategoriesFolder, KindsFolder} {
				out = append(out, yamlFiles(filepath.Join(dir, folder))...)
			}
		}
	}
	return out
}

// load reads the catalog (Load).
func load(prefixDir, workspace string) Catalog {
	var cat Catalog
	add := func(origin, file string, data []byte) {
		if filepath.Base(filepath.Dir(file)) == CategoriesFolder {
			c := Category{Origin: origin, Path: pathOf(origin, file)}
			if err := strict(data, &c); err != nil {
				c.ID, c.Problem = idOf(file), err.Error()
			}
			cat.Categories = append(cat.Categories, c)
			return
		}
		k := Kind{Origin: origin, Path: pathOf(origin, file)}
		if err := strict(data, &k); err != nil {
			k.ID, k.Problem = idOf(file), err.Error()
		}
		cat.Kinds = append(cat.Kinds, k)
	}
	for _, folder := range []string{CategoriesFolder, KindsFolder} {
		entries, _ := fs.ReadDir(builtin.FS, folder)
		for _, e := range entries {
			name := path.Join(folder, e.Name())
			if data, err := fs.ReadFile(builtin.FS, name); err == nil {
				add(OriginBuiltin, name, data)
			}
		}
	}
	for _, scope := range Scopes {
		dir := Dir(prefixDir, workspace, scope)
		if dir == "" {
			continue
		}
		for _, folder := range []string{CategoriesFolder, KindsFolder} {
			for _, f := range yamlFiles(filepath.Join(dir, folder)) {
				if data, err := os.ReadFile(f); err == nil {
					add(scope, f, data)
				}
			}
		}
	}
	cat.check()
	return cat
}

// pathOf is a descriptor's path as listed: "" for a built-in one.
func pathOf(origin, file string) string {
	if origin == OriginBuiltin {
		return ""
	}
	return file
}

// idOf is a descriptor's ID, or a connection's name, by its file.
func idOf(file string) string { return config.ConnectorFileName(file) }

// strict decodes a descriptor, refusing fields it doesn't know.
func strict(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return dec.Decode(v)
}

// yamlFiles are a folder's YAML files, sorted.
func yamlFiles(dir string) []string { return config.ConnectorFiles(dir) }

// reserved are names a category can't take: its folder would be a
// descriptors folder.
var reserved = []string{KindsFolder, CategoriesFolder}

// check notes each descriptor's problems: unknown backends, capabilities
// the backend hasn't, duplicate IDs.
func (cat *Catalog) check() {
	seen := map[string]bool{}
	for i := range cat.Categories {
		c := &cat.Categories[i]
		if c.Problem != "" {
			continue
		}
		var problems []string
		switch {
		case !idRE.MatchString(c.ID) || slices.Contains(reserved, c.ID):
			problems = append(problems, fmt.Sprintf("id %q: lower case letters, digits and -, not kinds or categories", c.ID))
		case seen[c.ID]:
			problems = append(problems, fmt.Sprintf("there's a category %q already", c.ID))
		}
		if !ValidName(c.Server) {
			problems = append(problems, fmt.Sprintf("server %q: lower case letters, digits and _", c.Server))
		}
		if !ValidName(c.Prefix) {
			problems = append(problems, fmt.Sprintf("prefix %q: lower case letters, digits and _", c.Prefix))
		}
		if c.SecretPrefix == "" || strings.ToUpper(c.SecretPrefix) != c.SecretPrefix {
			problems = append(problems, "secretPrefix: upper case, such as DB")
		}
		if _, ok := BackendOf(c.Backend); !ok {
			problems = append(problems, fmt.Sprintf("backend %q: one of %s", c.Backend, strings.Join(Backends(), ", ")))
		}
		for _, o := range cat.Categories[:i] {
			if o.Problem == "" && o.ID != c.ID && (o.Server == c.Server || o.Prefix == c.Prefix) {
				problems = append(problems, fmt.Sprintf("category %q has its server or prefix", o.ID))
			}
		}
		c.Problem = strings.Join(problems, "; ")
		seen[c.ID] = seen[c.ID] || c.Problem == ""
	}
	seenKind := map[string]bool{}
	for i := range cat.Kinds {
		k := &cat.Kinds[i]
		if k.Problem != "" {
			continue
		}
		var problems []string
		if !idRE.MatchString(k.ID) {
			problems = append(problems, fmt.Sprintf("id %q: lower case letters, digits and -", k.ID))
		} else if seenKind[k.ID] {
			problems = append(problems, fmt.Sprintf("there's a kind %q already", k.ID))
		}
		if _, ok := cat.Category(k.Category); !ok {
			problems = append(problems, fmt.Sprintf("category %q isn't there", k.Category))
		}
		b, ok := BackendOf(k.Backend)
		if !ok {
			problems = append(problems, fmt.Sprintf("backend %q: one of %s", k.Backend, strings.Join(Backends(), ", ")))
		}
		if k.Name.In("en") == "" {
			problems = append(problems, "name: say what it's called")
		}
		keys := map[string]bool{}
		for _, f := range k.Fields {
			if f.Key == "" || keys[f.Key] {
				problems = append(problems, fmt.Sprintf("field %q: every field has a key of its own", f.Key))
			}
			keys[f.Key] = true
			if f.Type != "" && !slices.Contains(FieldTypes, f.Type) {
				problems = append(problems, fmt.Sprintf("field %q: type is one of %s", f.Key, strings.Join(FieldTypes, ", ")))
			}
		}
		ids := map[string]bool{}
		for _, t := range k.Tools {
			if !ValidName(t.ID) || ids[t.ID] {
				problems = append(problems, fmt.Sprintf("tool %q: every tool has an id of its own (lower case, digits and _)", t.ID))
			}
			ids[t.ID] = true
		}
		for _, c := range k.Capabilities {
			switch {
			case !slices.Contains(Capabilities, c):
				problems = append(problems, fmt.Sprintf("capability %q: one of %s", c, capabilityList()))
			case ok && !supports(b, c):
				problems = append(problems, fmt.Sprintf("capability %q: backend %s can't", c, k.Backend))
			case c == Queries && (k.Queries == nil || k.Queries.Type == ""):
				problems = append(problems, "capability queries: say how they're written (queries: type)")
			case c == Schema && (k.Schema == nil || k.Schema.Statement == "" && k.Schema.Reader == ""):
				problems = append(problems, "capability schema: say how it's read (schema: statement or reader)")
			case c == Schema && k.Schema.Statement != "" && (k.Queries == nil || k.Queries.Type == ""):
				problems = append(problems, "capability schema: a statement runs as a query (queries: type)")
			case c == QueryBuilder && (!k.Can(Schema) || !k.Can(Queries)):
				problems = append(problems, "capability query-builder: needs schema and queries")
			}
		}
		if q := k.Queries; q != nil && q.Placeholders != "" && q.Placeholders != Positional && q.Placeholders != Named {
			problems = append(problems, fmt.Sprintf("queries: placeholders is %s or %s", Positional, Named))
		}
		k.Problem = strings.Join(problems, "; ")
		seenKind[k.ID] = seenKind[k.ID] || k.Problem == ""
	}
}

// Category is the usable category id.
func (cat Catalog) Category(id string) (Category, bool) {
	for _, c := range cat.Categories {
		if c.ID == id && c.Problem == "" {
			return c, true
		}
	}
	return Category{}, false
}

// Kind is the usable kind id.
func (cat Catalog) Kind(id string) (Kind, bool) {
	for _, k := range cat.Kinds {
		if k.ID == id && k.Problem == "" {
			return k, true
		}
	}
	return Kind{}, false
}

// KindsOf are a category's usable kinds, of backend ("" for any).
func (cat Catalog) KindsOf(category, backend string) []Kind {
	var out []Kind
	for _, k := range cat.Kinds {
		if k.Category == category && k.Problem == "" && (backend == "" || k.Backend == backend) {
			out = append(out, k)
		}
	}
	return out
}

// Usable are the categories that can be used.
func (cat Catalog) Usable() []Category {
	var out []Category
	for _, c := range cat.Categories {
		if c.Problem == "" {
			out = append(out, c)
		}
	}
	return out
}

// capabilityList is the capabilities' names, for a problem.
func capabilityList() string {
	names := make([]string, len(Capabilities))
	for i, c := range Capabilities {
		names[i] = string(c)
	}
	return strings.Join(names, ", ")
}
