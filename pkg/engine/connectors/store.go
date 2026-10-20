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
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/redact"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// ConnectionDir is a category's connections folder in a scope ("" for a
// workspace scope with no workspace).
func ConnectionDir(prefixDir, workspace, scope, category string) string {
	dir := Dir(prefixDir, workspace, scope)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, category)
}

// Files are a category's connection files in a scope, sorted.
func Files(prefixDir, workspace, scope, category string) []string {
	if dir := ConnectionDir(prefixDir, workspace, scope, category); dir != "" {
		return yamlFiles(dir)
	}
	return nil
}

// kindRE is the comment that names a connection file's kind, which
// Render writes first.
var kindRE = regexp.MustCompile(`(?m)^# kind: ([a-z][a-z0-9-]*)\s*$`)

// KindComment is the line that names a file's kind.
func KindComment(kind string) string { return "# kind: " + kind + "\n" }

// backendFor is the backend of a connection file's text: its kind's, by
// its comment, else the category's.
func backendFor(cat Catalog, c Category, text string) (Backend, bool) {
	id := c.Backend
	if m := kindRE.FindStringSubmatch(text); m != nil {
		if k, ok := cat.Kind(m[1]); ok && k.Category == c.ID {
			id = k.Backend
		}
	}
	return BackendOf(id)
}

// Parse reads a category's connection file text into the form, by its
// backend.
func Parse(cat Catalog, c Category, name, text string) Connection {
	b, ok := backendFor(cat, c, text)
	if !ok {
		return Connection{Name: name, Category: c.ID, Text: text, Custom: true, Problem: fmt.Sprintf("no backend %q", c.Backend)}
	}
	conn := b.Parse(c, cat.KindsOf(c.ID, b.ID()), name, text)
	conn.Name, conn.Category, conn.Text = name, c.ID, text
	if conn.Settings == nil {
		conn.Settings = map[string]string{}
	}
	return conn
}

// List reads the workspace's connections, every category's, the global
// ones first.
func List(cat Catalog, prefixDir, workspace string) []Connection {
	var out []Connection
	for _, c := range cat.Usable() {
		for _, scope := range []string{ScopeGlobal, ScopeWorkspace} {
			for _, f := range Files(prefixDir, workspace, scope, c.ID) {
				b, err := os.ReadFile(f)
				conn := Parse(cat, c, idOf(f), string(b))
				conn.Scope, conn.Path = scope, f
				if err != nil {
					conn.Problem = err.Error()
				}
				out = append(out, conn)
			}
		}
	}
	return out
}

// SecretScope is the secrets scope of a connection's scope.
func SecretScope(prefixDir, workspace, scope string) string {
	if scope == ScopeGlobal {
		return secrets.GlobalScope
	}
	return config.SecretScope(prefixDir, workspace)
}

// HasSecret says whether a connection's secret field has a value, in its
// own scope.
func HasSecret(prefixDir, workspace string, cat Category, c Connection, field string) bool {
	_, ok := config.Vault(prefixDir).Resolve(SecretScope(prefixDir, workspace, c.Scope))(secrets.OnlyWorkspace, SecretKey(cat, c.Name, field))
	return ok
}

// Check lists what keeps c, of kind k (nil for a custom one), from being
// written: its name, its kind's required and number settings, and what
// its backend says.
func Check(b Backend, k *Kind, c Connection) []string {
	var problems []string
	if !ValidName(c.Name) {
		problems = append(problems, fmt.Sprintf("name %q: lower case letters, digits and _, starting with a letter", c.Name))
	}
	if k != nil && !c.Custom {
		for _, f := range k.Fields {
			v := strings.TrimSpace(c.Settings[f.Key])
			if f.Type == FieldNumber && v != "" && !digitsRE.MatchString(v) {
				problems = append(problems, f.Key+" is a whole number")
			}
			// A secret one comes as a value to keep, not a setting (Save).
			if f.Required && !f.Secret() && f.Default == "" && v == "" {
				problems = append(problems, f.Key+" is required")
			}
		}
		if c.ReadOnly && k.ReadOnly == nil {
			problems = append(problems, k.ID+" can't be read-only")
		}
		for _, id := range c.Tools {
			if _, ok := k.Tool(id); !ok {
				problems = append(problems, fmt.Sprintf("no ready-made tool %q", id))
			}
		}
		if len(c.Queries) > 0 && !k.Can(Queries) {
			problems = append(problems, k.ID+" takes no queries of your own")
		}
		seen := map[string]bool{}
		for _, t := range k.Tools {
			seen[t.ID] = true
		}
		for _, q := range c.Queries {
			switch {
			case !ValidName(q.Name):
				problems = append(problems, fmt.Sprintf("query %q: lower case letters, digits and _, starting with a letter", q.Name))
			case seen[q.Name]:
				problems = append(problems, fmt.Sprintf("query %q: the name is taken", q.Name))
			}
			seen[q.Name] = true
			if strings.TrimSpace(q.Statement) == "" {
				problems = append(problems, fmt.Sprintf("query %q: the statement is empty", q.Name))
			}
			if strings.TrimSpace(q.Description) == "" {
				problems = append(problems, fmt.Sprintf("query %q: say what it's for (the agents read it)", q.Name))
			}
			for _, p := range q.Params {
				if !ValidName(p.Name) {
					problems = append(problems, fmt.Sprintf("query %q: parameter %q isn't a name", q.Name, p.Name))
				}
				if !slices.Contains(ParamTypes, p.Type) {
					problems = append(problems, fmt.Sprintf("query %q: parameter %q: type is string, integer, float or boolean", q.Name, p.Name))
				}
			}
		}
	}
	return append(problems, b.Check(k, c)...)
}

var digitsRE = regexp.MustCompile(`^[0-9]{1,18}$`)

// resolve is the backend and kind of a connection from the form: its
// kind's, or the category's backend for a custom one.
func resolve(cat Catalog, c Category, conn Connection) (Backend, *Kind, error) {
	if conn.Custom {
		b, ok := backendFor(cat, c, conn.Text)
		if !ok {
			return nil, nil, fmt.Errorf("no backend %q", c.Backend)
		}
		return b, nil, nil
	}
	k, ok := cat.Kind(conn.Kind)
	if !ok || k.Category != c.ID {
		var ids []string
		for _, k := range cat.KindsOf(c.ID, "") {
			ids = append(ids, k.ID)
		}
		return nil, nil, fmt.Errorf("kind %q: one of %s", conn.Kind, strings.Join(ids, ", "))
	}
	b, ok := BackendOf(k.Backend)
	if !ok {
		return nil, nil, fmt.Errorf("no backend %q", k.Backend)
	}
	return b, &k, nil
}

// Save writes conn in its scope's folder, in place of previous (its old
// name, "" for a new one), keeping the secret values given (by field) as
// Blitz secrets in that scope; problems, with nothing written, when it
// can't be saved.
func Save(cat Catalog, prefixDir, workspace string, conn Connection, values map[string]string, previous string) (Connection, []string, error) {
	c, ok := cat.Category(conn.Category)
	if !ok {
		return Connection{}, []string{fmt.Sprintf("category %q isn't there", conn.Category)}, nil
	}
	if conn.Scope == "" {
		conn.Scope = ScopeWorkspace
	}
	b, k, err := resolve(cat, c, conn)
	if err != nil {
		return Connection{}, []string{err.Error()}, nil
	}
	problems := Check(b, k, conn)
	if conn.Scope != ScopeGlobal && conn.Scope != ScopeWorkspace {
		problems = append(problems, fmt.Sprintf("scope %q: workspace or global", conn.Scope))
	}
	dir := ConnectionDir(prefixDir, workspace, conn.Scope, c.ID)
	if dir == "" {
		return Connection{}, append(problems, "no workspace for a workspace connection"), nil
	}
	if previous != conn.Name && file(dir, conn.Name) != "" {
		problems = append(problems, fmt.Sprintf("there's a connection named %q already", conn.Name))
	}
	if previous != "" && file(dir, previous) == "" {
		return Connection{}, nil, fmt.Errorf("%w: %q", ErrUnknown, previous)
	}
	renamed := previous != "" && previous != conn.Name
	var secretFields []Field
	if k != nil && !conn.Custom {
		for _, f := range k.Fields {
			if f.Secret() {
				secretFields = append(secretFields, f)
			}
		}
	}
	for _, f := range secretFields {
		given := values[f.Key] != ""
		kept := HasSecret(prefixDir, workspace, c, conn, f.Key) || (renamed && HasSecret(prefixDir, workspace, c, Connection{Name: previous, Scope: conn.Scope}, f.Key))
		if f.Required && !given && !kept {
			problems = append(problems, f.Key+" is required")
		}
		conn.Settings = maps.Clone(conn.Settings)
		if conn.Settings == nil {
			conn.Settings = map[string]string{}
		}
		delete(conn.Settings, f.Key)
		if given || kept {
			conn.Settings[f.Key] = "${" + SecretKey(c, conn.Name, f.Key) + "}"
		}
	}
	if len(problems) > 0 {
		return Connection{}, problems, nil
	}
	text, err := b.Render(c, k, conn)
	if err != nil {
		return Connection{}, nil, err
	}
	vault := config.Vault(prefixDir)
	scope := SecretScope(prefixDir, workspace, conn.Scope)
	for _, f := range secretFields {
		key := SecretKey(c, conn.Name, f.Key)
		if v := values[f.Key]; v != "" {
			if err := vault.Set(scope, key, v); err != nil {
				return Connection{}, nil, err
			}
			redact.Remember(v)
		} else if renamed { // its secret moves with it
			if v, ok := vault.Resolve(scope)(secrets.OnlyWorkspace, SecretKey(c, previous, f.Key)); ok {
				if err := vault.Set(scope, key, v); err != nil {
					return Connection{}, nil, err
				}
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Connection{}, nil, err
	}
	path := filepath.Join(dir, conn.Name+".yaml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return Connection{}, nil, err
	}
	if renamed {
		if err := Delete(cat, prefixDir, workspace, c.ID, conn.Scope, previous); err != nil {
			return Connection{}, nil, err
		}
	}
	saved := Parse(cat, c, conn.Name, text)
	saved.Scope, saved.Path = conn.Scope, path
	return saved, nil, nil
}

// file is the file of the connection name in dir; "" when there's none.
func file(dir, name string) string {
	if !ValidName(name) {
		return ""
	}
	for _, ext := range []string{".yaml", ".yml"} {
		p := filepath.Join(dir, name+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Delete removes a category's connection name from its scope, with its
// secrets.
func Delete(cat Catalog, prefixDir, workspace, category, scope, name string) error {
	c, ok := cat.Category(category)
	if !ok {
		return fmt.Errorf("%w: category %q", ErrUnknown, category)
	}
	p := file(ConnectionDir(prefixDir, workspace, scope, category), name)
	if p == "" {
		return fmt.Errorf("%w: %q", ErrUnknown, name)
	}
	b, _ := os.ReadFile(p)
	conn := Parse(cat, c, name, string(b))
	if err := os.Remove(p); err != nil {
		return err
	}
	if k, ok := cat.Kind(conn.Kind); ok {
		vault := config.Vault(prefixDir)
		for _, f := range k.Fields {
			if f.Secret() {
				_ = vault.Delete(SecretScope(prefixDir, workspace, scope), SecretKey(c, name, f.Key))
			}
		}
	}
	return nil
}

// Prepare is c as the backend writes it for a test, try or schema read:
// its kind and backend, and its secret settings naming their secrets when
// a value is typed or kept.
func Prepare(cat Catalog, env Env, conn Connection) (Backend, *Kind, Connection, error) {
	c, ok := cat.Category(conn.Category)
	if !ok {
		return nil, nil, conn, fmt.Errorf("category %q isn't there", conn.Category)
	}
	b, k, err := resolve(cat, c, conn)
	if err != nil {
		return nil, nil, conn, err
	}
	if problems := Check(b, k, conn); len(problems) > 0 {
		return nil, nil, conn, fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	if k != nil && !conn.Custom {
		conn.Settings = maps.Clone(conn.Settings)
		if conn.Settings == nil {
			conn.Settings = map[string]string{}
		}
		for _, f := range k.Fields {
			if !f.Secret() {
				continue
			}
			delete(conn.Settings, f.Key)
			if env.Values[f.Key] != "" || HasSecret(env.PrefixDir, env.Workspace, c, conn, f.Key) {
				conn.Settings[f.Key] = "${" + SecretKey(c, conn.Name, f.Key) + "}"
			}
		}
	}
	return b, k, conn, nil
}
