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
	"errors"
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

// Dirs is where connections are: the settings folder (Prefix, "" for
// ~/.blitz) for the global ones and a workspace ("" for none) for its own.
type Dirs struct {
	Prefix    string
	Workspace string
}

// Dir is a scope's connectors folder ("" for the workspace's with no
// workspace).
func (d Dirs) Dir(scope string) string { return Dir(d.Prefix, d.Workspace, scope) }

// ConnectionDir is a category's connections folder in a scope ("" for
// the workspace's with no workspace).
func (d Dirs) ConnectionDir(scope, category string) string {
	if dir := d.Dir(scope); dir != "" {
		return filepath.Join(dir, category)
	}
	return ""
}

// Files are a category's connection files in a scope, sorted.
func (d Dirs) Files(scope, category string) []string {
	if dir := d.ConnectionDir(scope, category); dir != "" {
		return yamlFiles(dir)
	}
	return nil
}

// SecretScope is the secrets scope of a connection scope's.
func (d Dirs) SecretScope(scope string) string {
	if scope == ScopeGlobal {
		return secrets.GlobalScope
	}
	return config.SecretScope(d.Prefix, d.Workspace)
}

// Global is d without its workspace: the global connections alone.
func (d Dirs) Global() Dirs { return Dirs{Prefix: d.Prefix} }

// Scopes are the connection scopes, the global one first: what Dirs
// lists in order.
var Scopes = []string{ScopeGlobal, ScopeWorkspace}

// Ref names a connection: its scope, category and name.
type Ref struct {
	Scope    string
	Category string
	Name     string
}

// Key is r's key in a workspace's choices (Key).
func (r Ref) Key() string { return Key(r.Scope, r.Category, r.Name) }

// Ref is c's reference.
func (c Connection) Ref() Ref { return Ref{Scope: c.Scope, Category: c.Category, Name: c.Name} }

// kindRE is the comment that names a connection file's kind, which
// Render writes first (KindComment).
var kindRE = regexp.MustCompile(`(?m)^# kind: ([a-z][a-z0-9-]*)\s*$`)

// KindOf is the kind a connection file's text names in its comment ("":
// none).
func KindOf(text string) string {
	if m := kindRE.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// KindComment is the line that names a file's kind.
func KindComment(kind string) string { return "# kind: " + kind + "\n" }

// backendFor is the backend of a connection file's text: its kind's, by
// its comment, else the category's.
func backendFor(cat Catalog, c Category, text string) (Backend, bool) {
	id := c.Backend
	if k, ok := cat.Kind(KindOf(text)); ok && k.Category == c.ID {
		id = k.Backend
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

// List reads the connections, every category's, the global ones first.
func List(cat Catalog, d Dirs) []Connection {
	var out []Connection
	for _, c := range cat.Usable() {
		for _, scope := range Scopes {
			for _, f := range d.Files(scope, c.ID) {
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

// HasSecret says whether a connection's secret field has a value, in its
// own scope.
func HasSecret(d Dirs, cat Category, c Connection, field string) bool {
	_, ok := config.Vault(d.Prefix).Resolve(d.SecretScope(c.Scope))(secrets.OnlyWorkspace, SecretKey(cat, c.Name, field))
	return ok
}

// SecretRef is the placeholder a connection file names a secret field's
// value by: ${DB_SHOP_PASSWORD}.
func SecretRef(cat Category, connection, field string) string {
	return "${" + SecretKey(cat, connection, field) + "}"
}

// withSecretRefs is conn with each of k's secret fields naming its secret
// when has says it has a value, and naming nothing otherwise: what Save
// writes and Prepare tests.
func withSecretRefs(cat Category, k *Kind, conn Connection, has func(Field) bool) Connection {
	if k == nil || conn.Custom {
		return conn
	}
	conn.Settings = maps.Clone(conn.Settings)
	if conn.Settings == nil {
		conn.Settings = map[string]string{}
	}
	for _, f := range k.SecretFields() {
		delete(conn.Settings, f.Key)
		if has(f) {
			conn.Settings[f.Key] = SecretRef(cat, conn.Name, f.Key)
		}
	}
	return conn
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
func Save(cat Catalog, d Dirs, conn Connection, values map[string]string, previous string) (Connection, []string, error) {
	p, problems, err := planSave(cat, d, conn, values, previous)
	if err != nil || len(problems) > 0 {
		return Connection{}, problems, err
	}
	if err := p.writeSecrets(d, values); err != nil {
		return Connection{}, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return Connection{}, nil, err
	}
	if err := os.WriteFile(p.path, []byte(p.text), 0o600); err != nil {
		return Connection{}, nil, err
	}
	if p.renamed() {
		if err := Delete(cat, d, Ref{Scope: p.conn.Scope, Category: p.cat.ID, Name: previous}); err != nil {
			return Connection{}, nil, err
		}
	}
	saved := Parse(cat, p.cat, p.conn.Name, p.text)
	saved.Scope, saved.Path = p.conn.Scope, p.path
	return saved, nil, nil
}

// saving is a save worked out: the connection as written, its file and
// text, and its secret fields.
type saving struct {
	cat      Category
	conn     Connection
	previous string
	path     string
	text     string
	secrets  []Field
}

func (p saving) renamed() bool { return p.previous != "" && p.previous != p.conn.Name }

// planSave checks conn and works out what Save writes, writing nothing.
func planSave(cat Catalog, d Dirs, conn Connection, values map[string]string, previous string) (saving, []string, error) {
	c, ok := cat.Category(conn.Category)
	if !ok {
		return saving{}, []string{fmt.Sprintf("category %q isn't there", conn.Category)}, nil
	}
	if conn.Scope == "" {
		conn.Scope = ScopeWorkspace
	}
	b, k, err := resolve(cat, c, conn)
	if err != nil {
		return saving{}, []string{err.Error()}, nil
	}
	problems := Check(b, k, conn)
	if conn.Scope != ScopeGlobal && conn.Scope != ScopeWorkspace {
		problems = append(problems, fmt.Sprintf("scope %q: workspace or global", conn.Scope))
	}
	dir := d.ConnectionDir(conn.Scope, c.ID)
	if dir == "" {
		return saving{}, append(problems, "no workspace for a workspace connection"), nil
	}
	p := saving{cat: c, previous: previous, path: filepath.Join(dir, conn.Name+".yaml")}
	switch existing := file(dir, conn.Name); {
	case previous != conn.Name && existing != "":
		problems = append(problems, fmt.Sprintf("there's a connection named %q already", conn.Name))
	case existing != "":
		p.path = existing // written where it is: a .yml stays one file
	}
	if previous != "" && file(dir, previous) == "" {
		return saving{}, nil, fmt.Errorf("%w: %q", ErrUnknown, previous)
	}
	if k != nil && !conn.Custom {
		p.secrets = k.SecretFields()
	}
	p.conn = conn
	kept := func(f Field) bool {
		return HasSecret(d, c, conn, f.Key) || (p.renamed() && HasSecret(d, c, Connection{Name: previous, Scope: conn.Scope}, f.Key))
	}
	for _, f := range p.secrets {
		if f.Required && values[f.Key] == "" && !kept(f) {
			problems = append(problems, f.Key+" is required")
		}
	}
	if len(problems) > 0 {
		return saving{}, problems, nil
	}
	p.conn = withSecretRefs(c, k, conn, func(f Field) bool { return values[f.Key] != "" || kept(f) })
	if p.text, err = b.Render(c, k, p.conn); err != nil {
		return saving{}, nil, err
	}
	return p, nil, nil
}

// writeSecrets keeps the secret values given; a renamed connection's
// kept ones move to its new name.
func (p saving) writeSecrets(d Dirs, values map[string]string) error {
	vault := config.Vault(d.Prefix)
	scope := d.SecretScope(p.conn.Scope)
	for _, f := range p.secrets {
		key := SecretKey(p.cat, p.conn.Name, f.Key)
		v := values[f.Key]
		if v == "" && p.renamed() {
			v, _ = vault.Resolve(scope)(secrets.OnlyWorkspace, SecretKey(p.cat, p.previous, f.Key))
		}
		if v == "" {
			continue
		}
		if err := vault.Set(scope, key, v); err != nil {
			return err
		}
		redact.Remember(v)
	}
	return nil
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

// Delete removes a connection and its secrets: every secret its
// category's kinds could have given it, so none is left behind even when
// its file can't be read.
func Delete(cat Catalog, d Dirs, r Ref) error {
	c, ok := cat.Category(r.Category)
	if !ok {
		return fmt.Errorf("%w: category %q", ErrUnknown, r.Category)
	}
	p := file(d.ConnectionDir(r.Scope, r.Category), r.Name)
	if p == "" {
		return fmt.Errorf("%w: %q", ErrUnknown, r.Name)
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	vault := config.Vault(d.Prefix)
	scope := d.SecretScope(r.Scope)
	var errs []error
	seen := map[string]bool{}
	for _, k := range cat.KindsOf(c.ID, "") {
		for _, f := range k.SecretFields() {
			if key := SecretKey(c, r.Name, f.Key); !seen[key] {
				seen[key] = true
				errs = append(errs, vault.Delete(scope, key))
			}
		}
	}
	return errors.Join(errs...)
}

// ResolvePath is p, a path setting's value (a field of type path), as it
// opens: a relative one against base (the workspace's folder for a
// workspace connection, the home folder for a global one), ~/ as the home
// folder; a value that isn't a file's path (SQLite's :memory:, a file:
// URI, a ${NAME} filled later) is left as it is (false).
func ResolvePath(p, base string) (string, bool) {
	switch {
	case p == "" || strings.HasPrefix(p, ":") || strings.HasPrefix(p, "file:") || strings.Contains(p, "${"):
		return p, false
	case p == "~" || strings.HasPrefix(p, "~/"):
		return config.ExpandHome(p), true
	case filepath.IsAbs(p):
		return p, true
	}
	return filepath.Join(base, p), true
}

// MakeGlobal moves the workspace's connection name, of category, to the
// global folder with its secrets (CON-19), so every workspace has it:
// problems, with nothing moved, when its kind or category is the
// workspace's own descriptor, it's a custom file, or a global one has its
// name. ErrUnknown when it isn't there.
func MakeGlobal(d Dirs, category, name string) (Connection, []string, error) {
	wcat := Load(d.Prefix, d.Workspace)
	c, ok := wcat.Category(category)
	if !ok {
		return Connection{}, nil, fmt.Errorf("%w: category %q", ErrUnknown, category)
	}
	p := file(d.ConnectionDir(ScopeWorkspace, category), name)
	if p == "" {
		return Connection{}, nil, fmt.Errorf("%w: %q", ErrUnknown, name)
	}
	text, err := os.ReadFile(p)
	if err != nil {
		return Connection{}, nil, err
	}
	conn := Parse(wcat, c, name, string(text))
	conn.Scope, conn.Path = ScopeWorkspace, p
	global := d.Global()
	gcat := Load(global.Prefix, "")
	_, ok = gcat.Category(category)
	switch {
	case conn.Problem != "":
		return Connection{}, []string{conn.Problem}, nil
	case conn.Custom:
		return Connection{}, []string{fmt.Sprintf("%s is more than the form describes: move %s to %s by hand", name, p, global.ConnectionDir(ScopeGlobal, category))}, nil
	case !ok:
		return Connection{}, []string{fmt.Sprintf("the %s category is this workspace's own: add its descriptor to %s first", category, GlobalDir(d.Prefix))}, nil
	}
	k, ok := gcat.Kind(conn.Kind)
	if !ok {
		return Connection{}, []string{fmt.Sprintf("the %s kind is this workspace's own: add its descriptor to %s first", conn.Kind, filepath.Join(GlobalDir(d.Prefix), KindsFolder))}, nil
	}
	if file(global.ConnectionDir(ScopeGlobal, category), name) != "" {
		return Connection{}, []string{fmt.Sprintf("there's a global connection named %q already", name)}, nil
	}
	values := map[string]string{}
	resolveSecret := config.Vault(d.Prefix).Resolve(d.SecretScope(ScopeWorkspace))
	conn.Settings = maps.Clone(conn.Settings)
	for _, f := range k.Fields {
		switch v := conn.Settings[f.Key]; {
		case f.Secret():
			if v, ok := resolveSecret(secrets.OnlyWorkspace, SecretKey(c, name, f.Key)); ok {
				values[f.Key] = v
			}
		// A relative path meant the workspace's folder: it still names that
		// file. ~/ means the home folder anywhere, so it stays as written.
		case f.Type == FieldPath && v != "" && !strings.HasPrefix(v, "~"):
			if p, isFile := ResolvePath(v, d.Workspace); isFile {
				conn.Settings[f.Key] = p
			}
		}
	}
	conn.Scope = ScopeGlobal
	saved, problems, err := Save(gcat, global, conn, values, "")
	if err != nil || len(problems) > 0 {
		return saved, problems, err
	}
	if err := Delete(wcat, d, Ref{Scope: ScopeWorkspace, Category: category, Name: name}); err != nil {
		return saved, nil, err
	}
	return saved, nil, nil
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
	conn = withSecretRefs(c, k, conn, func(f Field) bool {
		return env.Values[f.Key] != "" || HasSecret(env.Dirs, c, conn, f.Key)
	})
	return b, k, conn, nil
}
