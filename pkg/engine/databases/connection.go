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

package databases

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/embed"
)

// Studio's form for a database file: one connection (a Toolbox source),
// the ready-made tools switched on and the custom SQL queries, written as
// Toolbox YAML. A file the form can't describe (more than one source,
// other tools) is still listed, as Custom, and edited as YAML.

// Field is a setting a kind of database takes.
type Field struct {
	// Key is its name in the file (host, port, project…).
	Key string
	// Secret fields (passwords) are kept as Blitz secrets, the file
	// naming them (${DB_SHOP_PASSWORD}).
	Secret bool
	// Required fields must be filled.
	Required bool
	// Default is used when it's left empty.
	Default string
	// Number fields are a whole number.
	Number bool
}

// ReadyTool is a ready-made tool a kind offers: its ID (the tool is named
// <connection>_<id>), its Toolbox type, and a statement for one made of a
// query.
type ReadyTool struct {
	ID        string
	Type      string
	Statement string
	// Writes says the tool can change the database (hidden on a
	// read-only connection).
	Writes bool
	// On is whether a new connection has it.
	On bool
}

// Kind is a kind of database Studio offers.
type Kind struct {
	// Type is its Toolbox source type.
	Type string
	// SQL is the Toolbox type of its custom queries.
	SQL    string
	Fields []Field
	Tools  []ReadyTool
}

// Kinds are the databases Studio offers, as spec_databases_046 lists them.
var Kinds = []Kind{
	{
		Type: "postgres", SQL: "postgres-sql",
		Fields: []Field{{Key: "host", Required: true, Default: "localhost"}, {Key: "port", Required: true, Default: "5432"}, {Key: "database", Required: true}, {Key: "user", Required: true}, {Key: "password", Secret: true, Required: true}},
		Tools:  postgresTools,
	},
	{
		Type: "alloydb-postgres", SQL: "postgres-sql",
		Fields: []Field{{Key: "project", Required: true}, {Key: "region", Required: true}, {Key: "cluster", Required: true}, {Key: "instance", Required: true}, {Key: "ipType", Default: "public"}, {Key: "database", Required: true}, {Key: "user"}, {Key: "password", Secret: true}},
		Tools:  postgresTools,
	},
	{
		Type: "sqlite", SQL: "sqlite-sql",
		Fields: []Field{{Key: "database", Required: true}},
		Tools: []ReadyTool{
			{ID: "execute_sql", Type: "sqlite-execute-sql", Writes: true, On: true},
			{ID: "list_tables", Type: "sqlite-sql", Statement: "SELECT name, type, sql FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY name", On: true},
		},
	},
	{
		Type: "bigquery", SQL: "bigquery-sql",
		Fields: []Field{{Key: "project", Required: true}, {Key: "location"}, {Key: "allowedDatasets"}, {Key: "maximumBytesBilled", Number: true}},
		Tools: []ReadyTool{
			{ID: "execute_sql", Type: "bigquery-execute-sql", On: true},
			{ID: "list_datasets", Type: "bigquery-list-dataset-ids", On: true},
			{ID: "list_tables", Type: "bigquery-list-table-ids", On: true},
			{ID: "table_info", Type: "bigquery-get-table-info", On: true},
			{ID: "dataset_info", Type: "bigquery-get-dataset-info"},
		},
	},
}

var postgresTools = []ReadyTool{
	{ID: "execute_sql", Type: "postgres-execute-sql", On: true},
	{ID: "list_tables", Type: "postgres-list-tables", On: true},
	{ID: "list_schemas", Type: "postgres-list-schemas"},
	{ID: "list_views", Type: "postgres-list-views"},
	{ID: "list_indexes", Type: "postgres-list-indexes"},
	{ID: "database_overview", Type: "postgres-database-overview"},
}

// KindOf is the kind with Toolbox source type t.
func KindOf(t string) (Kind, bool) {
	for _, k := range Kinds {
		if k.Type == t {
			return k, true
		}
	}
	return Kind{}, false
}

// Param is a custom query's parameter.
type Param struct {
	Name string
	// Type is string, integer, float or boolean.
	Type        string
	Description string
}

// Query is a custom SQL tool: <connection>_<name>.
type Query struct {
	Name        string
	Description string
	Statement   string
	Params      []Param
}

// Connection is one database file, as Studio's form shows it.
type Connection struct {
	// Name is the file's name and its source's; its tools are named
	// <name>_<tool>.
	Name string
	// Scope is "workspace" (.agents/databases) or "global".
	Scope string
	Path  string
	// Type is the Toolbox source type (Kinds).
	Type string
	// Description is what it holds, for the agents: its tools say it, and
	// the file keeps it in a comment (# about: …).
	Description string
	// Settings are the kind's fields, a secret one naming its secret
	// (${NAME}).
	Settings map[string]string
	// ReadOnly connections can't write: the database refuses (SQLite,
	// Postgres, AlloyDB) or Toolbox does (BigQuery's writeMode blocked).
	ReadOnly bool
	// Tools are the ready-made tools switched on, by ID.
	Tools   []string
	Queries []Query
	// Custom files are more than the form describes: edited as YAML.
	Custom bool
	// Text is the file.
	Text string
	// Problem is why it can't be loaded ("" when it can).
	Problem string
}

// Scopes.
const (
	ScopeWorkspace = "workspace"
	ScopeGlobal    = "global"
)

var (
	nameRE   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	digitsRE = regexp.MustCompile(`^[0-9]{1,18}$`)
	// aboutRE is the comment that keeps a connection's description:
	// Toolbox's sources take no description field.
	aboutRE = regexp.MustCompile(`(?m)^# about: (.*)$`)
)

// ValidName says whether name can name a connection or a query: lower
// case letters, digits and _, starting with a letter, at most 40.
func ValidName(name string) bool { return nameRE.MatchString(name) }

// SecretKey is the Blitz secret a connection's secret field is kept as:
// DB_SHOP_PASSWORD.
func SecretKey(connection, field string) string {
	return "DB_" + strings.ToUpper(connection) + "_" + strings.ToUpper(field)
}

// doc is one YAML document of a database file.
type doc = map[string]any

// Parse reads a database file's text into the form; a file that's more
// than one connection with its ready-made tools and queries is Custom.
func Parse(name, text string) Connection {
	c := Connection{Name: name, Text: text, Settings: map[string]string{}}
	docs, err := decode(text)
	if err != nil {
		c.Custom, c.Problem = true, err.Error()
		return c
	}
	var source doc
	var tools []doc
	for _, d := range docs {
		switch d["kind"] {
		case "source", "sources":
			if source != nil {
				c.Custom = true
				return c
			}
			source = d
		case "tool", "tools":
			tools = append(tools, d)
		default:
			c.Custom = true
			return c
		}
	}
	if source == nil || source["name"] != name {
		c.Custom = true
		return c
	}
	c.Type, _ = source["type"].(string)
	kind, ok := KindOf(c.Type)
	if !ok {
		c.Custom = true
		return c
	}
	if m := aboutRE.FindStringSubmatch(text); m != nil {
		c.Description = strings.TrimSpace(m[1])
	}
	known := map[string]bool{"kind": true, "name": true, "type": true, "readOnly": true, "writeMode": true}
	for _, f := range kind.Fields {
		known[f.Key] = true
		if v, ok := source[f.Key]; ok && v != nil {
			c.Settings[f.Key] = scalar(v)
		}
	}
	for k := range source {
		if !known[k] {
			c.Custom = true
		}
	}
	if c.Type == "bigquery" {
		c.ReadOnly = source["writeMode"] == "blocked" || source["readOnly"] == true
	} else {
		c.ReadOnly = source["readOnly"] == true
	}
	for _, t := range tools {
		tname, _ := t["name"].(string)
		ttype, _ := t["type"].(string)
		id, ok := strings.CutPrefix(tname, name+"_")
		if !ok || t["source"] != name {
			c.Custom = true
			continue
		}
		if i := slices.IndexFunc(kind.Tools, func(r ReadyTool) bool { return r.ID == id && r.Type == ttype }); i >= 0 {
			c.Tools = append(c.Tools, id)
			continue
		}
		if ttype != kind.SQL {
			c.Custom = true
			continue
		}
		q := Query{Name: id}
		q.Description, _ = t["description"].(string)
		q.Statement, _ = t["statement"].(string)
		q.Statement = strings.TrimRight(q.Statement, "\n")
		if ps, ok := t["parameters"].([]any); ok {
			for _, p := range ps {
				pm, _ := p.(doc)
				var pp Param
				pp.Name, _ = pm["name"].(string)
				pp.Type, _ = pm["type"].(string)
				pp.Description, _ = pm["description"].(string)
				q.Params = append(q.Params, pp)
			}
		}
		c.Queries = append(c.Queries, q)
	}
	return c
}

// scalar is a YAML scalar as text; a list, its items with commas.
func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any: // allowedDatasets
		parts := make([]string, len(x))
		for i, p := range x {
			parts[i] = scalar(p)
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

// decode reads a file's YAML documents.
func decode(text string) ([]doc, error) {
	dec := yaml.NewDecoder(strings.NewReader(text))
	var out []doc
	for {
		var d doc
		err := dec.Decode(&d)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if d != nil {
			out = append(out, d)
		}
	}
}

// Check lists what keeps c from being saved; none when it can be.
func Check(c Connection) []string {
	var problems []string
	if !ValidName(c.Name) {
		problems = append(problems, fmt.Sprintf("name %q: lower case letters, digits and _, starting with a letter", c.Name))
	}
	if c.Custom {
		if _, err := decode(c.Text); err != nil {
			problems = append(problems, "the YAML: "+err.Error())
		}
		return problems
	}
	kind, ok := KindOf(c.Type)
	if !ok {
		return append(problems, fmt.Sprintf("type %q: one of postgres, alloydb-postgres, sqlite, bigquery", c.Type))
	}
	for _, f := range kind.Fields {
		if v := strings.TrimSpace(c.Settings[f.Key]); f.Number && v != "" && !digitsRE.MatchString(v) {
			problems = append(problems, f.Key+" is a whole number")
		}
		// A secret one comes as a value to keep, not a setting (Save).
		if f.Required && !f.Secret && f.Default == "" && strings.TrimSpace(c.Settings[f.Key]) == "" {
			problems = append(problems, f.Key+" is required")
		}
	}
	for _, id := range c.Tools {
		if !slices.ContainsFunc(kind.Tools, func(r ReadyTool) bool { return r.ID == id }) {
			problems = append(problems, fmt.Sprintf("no ready-made tool %q", id))
		}
	}
	seen := map[string]bool{}
	for _, r := range kind.Tools {
		seen[r.ID] = true
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
			problems = append(problems, fmt.Sprintf("query %q: the SQL is empty", q.Name))
		}
		if strings.TrimSpace(q.Description) == "" {
			problems = append(problems, fmt.Sprintf("query %q: say what it's for (the agents read it)", q.Name))
		}
		for _, p := range q.Params {
			if !ValidName(p.Name) {
				problems = append(problems, fmt.Sprintf("query %q: parameter %q isn't a name", q.Name, p.Name))
			}
			if !slices.Contains([]string{"string", "integer", "float", "boolean"}, p.Type) {
				problems = append(problems, fmt.Sprintf("query %q: parameter %q: type is string, integer, float or boolean", q.Name, p.Name))
			}
		}
	}
	return problems
}

// Render writes c as Toolbox YAML: its source, then its ready-made tools
// and queries. A secret field always names its secret.
func Render(c Connection) (string, error) {
	if c.Custom {
		return c.Text, nil
	}
	kind, ok := KindOf(c.Type)
	if !ok {
		return "", fmt.Errorf("no database type %q", c.Type)
	}
	label := c.Name
	if c.Description != "" {
		label = c.Name + " (" + strings.TrimRight(c.Description, ".") + ")"
	}
	var docs []*yaml.Node
	src := mapping("kind", "source", "name", c.Name, "type", c.Type)
	for _, f := range kind.Fields {
		v := strings.TrimSpace(c.Settings[f.Key])
		switch {
		case f.Secret:
			if v == "" && !f.Required {
				continue
			}
			v = "${" + SecretKey(c.Name, f.Key) + "}"
		case v == "":
			if v = f.Default; v == "" {
				continue
			}
		}
		if f.Key == "allowedDatasets" {
			var list []string
			for _, d := range strings.Split(v, ",") {
				if d = strings.TrimSpace(d); d != "" {
					list = append(list, d)
				}
			}
			addList(src, f.Key, list)
			continue
		}
		if f.Number {
			n := &yaml.Node{Kind: yaml.ScalarNode, Value: v, Tag: "!!int"}
			src.Content = append(src.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: f.Key}, n)
			continue
		}
		add(src, f.Key, v)
	}
	switch {
	case c.Type == "bigquery" && c.ReadOnly:
		add(src, "writeMode", "blocked")
	case c.ReadOnly:
		addBool(src, "readOnly", true)
	}
	docs = append(docs, src)
	for _, r := range kind.Tools {
		if !slices.Contains(c.Tools, r.ID) {
			continue
		}
		t := mapping("kind", "tool", "name", c.Name+"_"+r.ID, "type", r.Type, "source", c.Name, "description", readyDescription(r, label))
		if r.Statement != "" {
			add(t, "statement", r.Statement)
		}
		docs = append(docs, t)
	}
	for _, q := range c.Queries {
		t := mapping("kind", "tool", "name", c.Name+"_"+q.Name, "type", kind.SQL, "source", c.Name, "description", q.Description)
		stmt := &yaml.Node{Kind: yaml.ScalarNode, Value: strings.TrimSpace(q.Statement) + "\n", Style: yaml.LiteralStyle}
		t.Content = append(t.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "statement"}, stmt)
		if len(q.Params) > 0 {
			seq := &yaml.Node{Kind: yaml.SequenceNode}
			for _, p := range q.Params {
				pm := mapping("name", p.Name, "type", p.Type, "description", p.Description)
				seq.Content = append(seq.Content, pm)
			}
			t.Content = append(t.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "parameters"}, seq)
		}
		docs = append(docs, t)
	}
	var b bytes.Buffer
	b.WriteString("# A database for Blitz's agents (spec_databases_046), written by Studio.\n# ${NAME} is the Blitz secret NAME.\n")
	if d := strings.Join(strings.Fields(c.Description), " "); d != "" {
		b.WriteString("# about: " + d + "\n")
	}
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			return "", err
		}
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return b.String(), nil
}

// readyDescription is what a ready-made tool says it does, for the
// agents.
func readyDescription(r ReadyTool, label string) string {
	switch r.ID {
	case "execute_sql":
		return "Run a SQL statement on " + label + "."
	case "list_tables":
		return "List the tables in " + label + ", with their columns."
	case "list_schemas":
		return "List the schemas in " + label + "."
	case "list_views":
		return "List the views in " + label + "."
	case "list_indexes":
		return "List the indexes in " + label + "."
	case "database_overview":
		return "An overview of " + label + ": its version, size and connections."
	case "list_datasets":
		return "List the datasets in " + label + "."
	case "table_info":
		return "A table's schema and details in " + label + "."
	case "dataset_info":
		return "A dataset's details in " + label + "."
	}
	return r.ID + " on " + label + "."
}

func mapping(kv ...string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for i := 0; i+1 < len(kv); i += 2 {
		add(n, kv[i], kv[i+1])
	}
	return n
}

func add(n *yaml.Node, k, v string) {
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &yaml.Node{Kind: yaml.ScalarNode, Value: v, Tag: "!!str"})
}

func addBool(n *yaml.Node, k string, v bool) {
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprint(v), Tag: "!!bool"})
}

func addList(n *yaml.Node, k string, vs []string) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, v := range vs {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: v, Tag: "!!str"})
	}
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, seq)
}

// Dir is a scope's folder for a workspace.
func Dir(prefixDir, workspace, scope string) string {
	if scope == ScopeGlobal {
		return GlobalDir(prefixDir)
	}
	return WorkspaceDir(workspace)
}

// List reads the workspace's database files, the global ones first.
func List(prefixDir, workspace string) []Connection {
	var out []Connection
	for _, scope := range []string{ScopeGlobal, ScopeWorkspace} {
		if scope == ScopeWorkspace && workspace == "" {
			continue
		}
		for _, f := range embed.Files(Dir(prefixDir, workspace, scope)) {
			b, err := os.ReadFile(f)
			name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(f), ".yaml"), ".yml")
			c := Parse(name, string(b))
			c.Scope, c.Path = scope, f
			if err != nil {
				c.Problem = err.Error()
			}
			out = append(out, c)
		}
	}
	return out
}
