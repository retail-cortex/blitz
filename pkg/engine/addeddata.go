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

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/datatable"
)

// Data in a chat (spec_059_data DAT-40..DAT-44): a database the user
// added to a turn is its schema and the tools that query it; a query's
// result is its SQL, its columns described and its rows.

// Bounds of what a turn's data adds (DAT-41, DAT-42).
const (
	addedSchemaColumns = 200
	addedQueryRows     = 200
)

// dbMention is @db:<connection>, or @db:<connection>/<query>, in a prompt.
var dbMention = regexp.MustCompile(`(?:^|\s)@db:([A-Za-z0-9_-]+)(?:/([A-Za-z0-9_]+))?`)

// withData is blocks with each database and query block made into its
// text, and a block for each @db: mention in text (a saved query is run,
// read-only, when it takes no parameters). What can't be read says why,
// in its block.
func (w *Workspace) withData(ctx context.Context, text string, blocks []api.ContextBlock) []api.ContextBlock {
	var conns []connectors.Connection
	connsOf := func() []connectors.Connection {
		if conns == nil {
			conns = w.Connectors()
		}
		return conns
	}
	out := make([]api.ContextBlock, 0, len(blocks))
	for _, b := range blocks {
		switch b.Kind {
		case "database":
			b.Text = w.schemaText(ctx, connsOf(), b.Connection)
		case "data_query":
			b.Text = queryText(b)
		}
		out = append(out, b)
	}
	for _, m := range dbMention.FindAllStringSubmatch(text, -1) {
		name, query := m[1], m[2]
		if query == "" {
			if !slices.ContainsFunc(out, func(b api.ContextBlock) bool { return b.Kind == "database" && b.Connection == name }) {
				out = append(out, api.ContextBlock{Kind: "database", Title: name, Connection: name, Text: w.schemaText(ctx, connsOf(), name)})
			}
			continue
		}
		out = append(out, w.savedQueryBlock(ctx, connsOf(), name, query))
	}
	return out
}

// chatConnection is the connection name a chat may use: on in the
// workspace, readable, and trusted when it's the workspace's own (DAT-44).
func chatConnection(conns []connectors.Connection, name string) (connectors.Connection, error) {
	c, ok := dataConnection(conns, name)
	switch {
	case !ok:
		return c, fmt.Errorf("there's no database connection %q", name)
	case c.Problem != "":
		return c, fmt.Errorf("the connection %s can't be read: %s", name, c.Problem)
	case !c.On:
		return c, fmt.Errorf("the connection %s is off in this workspace", name)
	case !c.Trusted:
		return c, fmt.Errorf("the connection %s isn't trusted: save it in Studio first", name)
	}
	return c, nil
}

// schemaText is a connection as a turn reads it (DAT-41): what it holds,
// its tables and columns (at most addedSchemaColumns), and the tools that
// query it. Nothing is queried.
func (w *Workspace) schemaText(ctx context.Context, conns []connectors.Connection, name string) string {
	c, err := chatConnection(conns, name)
	if err != nil {
		return err.Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Database connection %s (%s", c.Name, c.Kind)
	if c.ReadOnly {
		b.WriteString(", read-only")
	}
	b.WriteString(")")
	if c.Description != "" {
		b.WriteString(": " + c.Description)
	}
	b.WriteString("\n")
	tables, err := w.ReadConnectorSchema(ctx, c, nil)
	if err != nil {
		fmt.Fprintf(&b, "Its schema couldn't be read: %s\n", w.secretRedactor().String(err.Error()))
	} else {
		b.WriteString("Tables:\n")
		shown, all := 0, 0
		for _, t := range tables {
			all += len(t.Columns)
		}
		for _, t := range tables {
			if shown >= addedSchemaColumns {
				break
			}
			fmt.Fprintf(&b, "- %s", t.Name)
			if t.About != "" {
				b.WriteString(": " + t.About)
			}
			b.WriteString("\n")
			for _, col := range t.Columns {
				if shown >= addedSchemaColumns {
					break
				}
				fmt.Fprintf(&b, "  - %s %s", col.Name, col.Type)
				if col.About != "" {
					b.WriteString(": " + col.About)
				}
				b.WriteString("\n")
				shown++
			}
		}
		if all > shown {
			fmt.Fprintf(&b, "(%d more columns left out: list them with its tools)\n", all-shown)
		}
	}
	if tools := connectionTools(w.ConnectorCatalog(), c); len(tools) > 0 {
		fmt.Fprintf(&b, "Query it as you need to with its tools: %s.\n", strings.Join(tools, ", "))
	}
	return b.String()
}

// connectionTools are the agents' names for c's tools and queries.
func connectionTools(cat connectors.Catalog, c connectors.Connection) []string {
	category, ok := cat.Category(dataCategory)
	if !ok {
		return nil
	}
	var out []string
	for _, t := range c.Tools {
		out = append(out, category.Prefix+"__"+c.Name+"_"+t)
	}
	for _, q := range c.Queries {
		out = append(out, category.Prefix+"__"+c.Name+"_"+q.Name)
	}
	return out
}

// savedQueryBlock is one of a connection's saved queries, run now and
// read-only, as a turn's block (an @db: mention).
func (w *Workspace) savedQueryBlock(ctx context.Context, conns []connectors.Connection, name, query string) api.ContextBlock {
	b := api.ContextBlock{Kind: "data_query", Title: name + " · " + query, Connection: name}
	c, err := chatConnection(conns, name)
	if err != nil {
		b.Text = err.Error()
		return b
	}
	i := slices.IndexFunc(c.Queries, func(q connectors.Query) bool { return q.Name == query })
	switch {
	case i < 0:
		b.Text = fmt.Sprintf("%s has no query %q", name, query)
		return b
	case len(c.Queries[i].Params) > 0:
		b.Text = fmt.Sprintf("%s takes parameters: add it from the chat box's database menu", query)
		return b
	}
	b.Statement = c.Queries[i].Statement
	t, err := w.RunConnectorQuery(ctx, c, nil, connectors.QueryRun{Query: query, MaxRows: addedQueryRows, ReadOnly: true})
	if err != nil {
		b.Text = "The query failed: " + w.secretRedactor().String(err.Error())
		return b
	}
	data, _ := json.Marshal(t)
	b.Text = string(data)
	b.Text = queryText(b)
	return b
}

// queryText is a query's result as a turn reads it (DAT-42): its
// connection and SQL, its columns described, then its rows as Markdown
// (at most addedQueryRows), saying what was cut. Text that isn't a table
// (why it failed) is kept as it is.
func queryText(b api.ContextBlock) string {
	var t datatable.Table
	if err := json.Unmarshal([]byte(b.Text), &t); err != nil || t.Columns == nil {
		return b.Text
	}
	var s strings.Builder
	fmt.Fprintf(&s, "A query's result, on the database connection %s.\n", b.Connection)
	if b.Statement != "" {
		fmt.Fprintf(&s, "SQL:\n```sql\n%s\n```\n", strings.TrimSpace(b.Statement))
	}
	s.WriteString("Columns:\n")
	for _, c := range t.Columns {
		fmt.Fprintf(&s, "- %s (%s", c.Name, c.Type)
		if c.Length != nil {
			fmt.Fprintf(&s, ", %d", *c.Length)
		}
		s.WriteString(")")
		if c.Description != "" {
			s.WriteString(": " + c.Description)
		}
		s.WriteString("\n")
	}
	fmt.Fprintf(&s, "Rows (%d):\n", t.Total)
	md := t.Markdown(addedQueryRows)
	if len(md) > addedMaxData-s.Len()-200 {
		md = md[:max(0, addedMaxData-s.Len()-200)] + "\n… (the rest is cut: query the connection for more)\n"
	}
	s.WriteString(md)
	if t.Truncated {
		s.WriteString("The query returned more rows than these: query the connection for the rest.\n")
	}
	return s.String()
}
