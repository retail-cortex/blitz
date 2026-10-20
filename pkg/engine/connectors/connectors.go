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

// Package connectors is how Blitz's agents reach the systems a team
// works with (spec_connectors_047): databases now, email and others
// later. A connector is described, not coded: a category (Databases)
// and its kinds (PostgreSQL, SQLite…) are YAML descriptors, the built-in
// ones embedded and more read from <config>/connectors/kinds and a
// workspace's .agents/connectors/kinds. A kind names the backend that
// serves it, a Go Backend registered at start (Toolbox for databases),
// and the capabilities it offers (test, try, queries, schema,
// query-builder), which the backend must have.
//
// A connection is one file, <name>.yaml, in <config>/connectors/<category>
// (every workspace) or .agents/connectors/<category> (this one), written
// by the kind's backend; its secret settings are Blitz secrets, never in
// the file. Each category's connections are served to the agents as one
// MCP server per backend.
package connectors

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Capability is something a kind of connector can do, beyond being
// served to the agents.
type Capability string

// The capabilities.
const (
	// Test connects to a connection as written and lists its tools.
	Test Capability = "test"
	// Try runs one of a connection's tools as written.
	Try Capability = "try"
	// Queries are the user's own tools, a statement with parameters each.
	Queries Capability = "queries"
	// Schema reads what a connection holds: its tables and their columns.
	Schema Capability = "schema"
	// QueryBuilder drafts a query from what the user describes, from the
	// schema, with the sql-query skill (needs Schema and Queries).
	QueryBuilder Capability = "query-builder"
)

// Capabilities are every capability, in the order they're shown.
var Capabilities = []Capability{Test, Try, Queries, Schema, QueryBuilder}

// Text is a descriptor's text in English and other languages: written as
// a string (English) or a map of language tags to text (en, es, fr-CA).
type Text map[string]string

// UnmarshalYAML reads a string or a map.
func (t *Text) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err == nil {
		*t = Text{"en": s}
		return nil
	}
	var m map[string]string
	if err := unmarshal(&m); err != nil {
		return errors.New("text: a string, or a map of languages to strings")
	}
	*t = m
	return nil
}

// In is the text in lang (es, fr-CA, fr), else English.
func (t Text) In(lang string) string {
	if s, ok := t[lang]; ok {
		return s
	}
	if base, _, ok := strings.Cut(lang, "-"); ok {
		if s, ok := t[base]; ok {
			return s
		}
	}
	return t["en"]
}

// Category is a group of kinds of connectors (Databases, Email): Studio
// lists connections by category, and each category's are one MCP server
// per backend.
type Category struct {
	ID   string `yaml:"id"`
	Name Text   `yaml:"name"`
	Hint Text   `yaml:"hint"`
	// Icon is a Material Design icon's name, without mdi (database).
	Icon string `yaml:"icon"`
	// Server is its MCP server's name, in /mcp and mcp(<server>:…) rules.
	Server string `yaml:"server"`
	// Prefix namespaces its tools: db__shop_list_tables.
	Prefix string `yaml:"prefix"`
	// SecretPrefix starts its connections' secrets: DB_SHOP_PASSWORD.
	SecretPrefix string `yaml:"secretPrefix"`
	// Backend serves its files that name no kind (hand-written ones).
	Backend string `yaml:"backend"`

	Origin  string `yaml:"-"`
	Path    string `yaml:"-"`
	Problem string `yaml:"-"`
}

// Field is a setting a kind takes.
type Field struct {
	// Key is its name in the file (host, port, project…).
	Key   string `yaml:"key"`
	Label Text   `yaml:"label"`
	Help  Text   `yaml:"help"`
	// Type is text (the default), number (a whole number), secret (kept as
	// a Blitz secret, the file naming it), list (comma-separated) or path.
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	// Default is used when it's left empty.
	Default string `yaml:"default"`
}

// Field types.
const (
	FieldText   = "text"
	FieldNumber = "number"
	FieldSecret = "secret"
	FieldList   = "list"
	FieldPath   = "path"
)

// Secret says whether the field is kept as a Blitz secret.
func (f Field) Secret() bool { return f.Type == FieldSecret }

// ReadyTool is a ready-made tool a kind offers: named <connection>_<id>.
type ReadyTool struct {
	ID string `yaml:"id"`
	// Type is the backend's own (a Toolbox tool type).
	Type string `yaml:"type"`
	// Statement makes it a query (a backend's SQL tool).
	Statement string `yaml:"statement"`
	// About is what it does, for Studio.
	About Text `yaml:"about"`
	// Agent is what it does, for the agents; {connection} is the
	// connection's name and description.
	Agent string `yaml:"agent"`
	// Writes says it can change what it's connected to: not offered on a
	// read-only connection.
	Writes bool `yaml:"writes"`
	// On says a new connection has it.
	On bool `yaml:"on"`
}

// ReadOnly is how a kind is made read-only: Key set to Value in its
// file. A kind without it can't be.
type ReadOnly struct {
	Key   string `yaml:"key"`
	Value any    `yaml:"value"`
	// Hint says what read-only means for it.
	Hint Text `yaml:"hint"`
}

// QuerySpec is how a kind's own queries are written.
type QuerySpec struct {
	// Type is the backend's tool type for one (postgres-sql).
	Type string `yaml:"type"`
	// Dialect is the SQL they're in, for the query builder (PostgreSQL).
	Dialect string `yaml:"dialect"`
	// Placeholders is how a statement names its parameters: $1 (by
	// position) or @name.
	Placeholders string `yaml:"placeholders"`
	Hint         Text   `yaml:"hint"`
	Example      string `yaml:"example"`
}

// Placeholder styles.
const (
	Positional = "$1"
	Named      = "@name"
)

// SchemaSpec is how a kind's schema is read: a statement (run as one of
// its queries) whose rows are the tables, or a reader the backend has.
type SchemaSpec struct {
	Statement string `yaml:"statement"`
	Reader    string `yaml:"reader"`
}

// Kind is a kind of connector: a descriptor.
type Kind struct {
	ID       string `yaml:"id"`
	Category string `yaml:"category"`
	Name     Text   `yaml:"name"`
	Hint     Text   `yaml:"hint"`
	// Backend serves it (toolbox).
	Backend string `yaml:"backend"`
	// Source is the backend's own type for it (a Toolbox source type).
	Source       string       `yaml:"source"`
	Fields       []Field      `yaml:"fields"`
	ReadOnly     *ReadOnly    `yaml:"readOnly"`
	Tools        []ReadyTool  `yaml:"tools"`
	Queries      *QuerySpec   `yaml:"queries"`
	Schema       *SchemaSpec  `yaml:"schema"`
	Capabilities []Capability `yaml:"capabilities"`

	// Origin is builtin, global or workspace; Path its file ("" built in).
	Origin  string `yaml:"-"`
	Path    string `yaml:"-"`
	Problem string `yaml:"-"`
}

// Can says whether the kind has capability c.
func (k Kind) Can(c Capability) bool { return slices.Contains(k.Capabilities, c) }

// Field is the kind's field key.
func (k Kind) Field(key string) (Field, bool) {
	for _, f := range k.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// Tool is the kind's ready-made tool id.
func (k Kind) Tool(id string) (ReadyTool, bool) {
	for _, t := range k.Tools {
		if t.ID == id {
			return t, true
		}
	}
	return ReadyTool{}, false
}

// Param is a query's parameter.
type Param struct {
	Name string
	// Type is string, integer, float or boolean.
	Type        string
	Description string
}

// ParamTypes are the types a parameter may have.
var ParamTypes = []string{"string", "integer", "float", "boolean"}

// Query is a connection's own tool: <connection>_<name>.
type Query struct {
	Name        string
	Description string
	Statement   string
	Params      []Param
}

// Connection is one connection file, as Studio's form shows it.
type Connection struct {
	// Name is the file's name; its tools are named <name>_<tool>.
	Name     string
	Category string
	// Scope is workspace (.agents/connectors/<category>) or global.
	Scope string
	Path  string
	// Kind is its kind's ID ("" for a custom file the backend can't say).
	Kind string
	// Description is what it holds, for the agents.
	Description string
	// Settings are the kind's fields; a secret one names its secret.
	Settings map[string]string
	// ReadOnly connections can't change what they're connected to.
	ReadOnly bool
	// Tools are the ready-made tools switched on, by ID.
	Tools   []string
	Queries []Query
	// Custom files are more than the form describes: edited as text.
	Custom bool
	// Text is the file.
	Text string
	// Problem is why it can't be read ("" when it can).
	Problem string
	// Trusted says the person trusted it (Serving.Trusted): a global one,
	// or a workspace one saved in Studio or with trusted project settings.
	Trusted bool
	// On says it's served in this workspace: the person can turn any
	// connection off there, a global one too (CON-17).
	On bool
}

// Key names a connection in a workspace's choices: <scope>/<category>/<name>.
func Key(scope, category, name string) string {
	return scope + "/" + category + "/" + name
}

// Key is c's key (Key).
func (c Connection) Key() string { return Key(c.Scope, c.Category, c.Name) }

// Scopes.
const (
	ScopeWorkspace = "workspace"
	ScopeGlobal    = "global"
)

// Origins of descriptors.
const (
	OriginBuiltin   = "builtin"
	OriginGlobal    = ScopeGlobal
	OriginWorkspace = ScopeWorkspace
)

// ToolInfo is a tool a connection offers.
type ToolInfo struct {
	Name        string
	Description string
	// ReadOnly tools run without asking.
	ReadOnly bool
}

// Table is a table (or view) a connection holds, as its schema says.
type Table struct {
	// Name is as a query names it (schema.table, dataset.table).
	Name string
	// About is its comment or description.
	About   string
	Columns []Column
}

// Column is a table's column.
type Column struct {
	Name  string
	Type  string
	About string
}

// ErrUnknown is a connection that isn't there.
var ErrUnknown = errors.New("no such connection")

// ErrNoTables is a schema read that found no table it could read.
var ErrNoTables = errors.New("no tables")

// TestTimeout bounds connecting to a connection to test, try or read it.
var TestTimeout = 20 * time.Second

var (
	nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	idRE   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
)

// ValidName says whether name can name a connection or a query: lower
// case letters, digits and _, starting with a letter, at most 40.
func ValidName(name string) bool { return nameRE.MatchString(name) }

// SecretKey is the Blitz secret a connection's secret field is kept as:
// DB_SHOP_PASSWORD (the category's SecretPrefix, the name, the field).
func SecretKey(cat Category, connection, field string) string {
	return cat.SecretPrefix + "_" + strings.ToUpper(connection) + "_" + strings.ToUpper(field)
}
