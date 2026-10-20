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
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/databases"
	"github.com/retail-cortex/blitz/pkg/engine/runtime"
)

// The query builder (spec_connectors_047 CON-12): a connection whose kind
// has the query-builder capability reads its schema (failing when it can
// read no table), and the sql-query skill, given the schema and what the
// user is looking for, writes one of the connection's queries: its name,
// description, statement and parameters. Nothing is saved: the form
// offers it, to try and keep.

// SQLSkill is the skill that writes the queries: the built-in one, or the
// workspace's own of that name.
const SQLSkill = "sql-query"

// MaxSchemaText bounds the schema the model is given, in bytes.
var MaxSchemaText = 48 << 10

// QueryDraft is the query builder's draft: the query, the assumptions it
// made, how many tables it read, or why there's none.
type QueryDraft struct {
	Query   connectors.Query
	Notes   []string
	Tables  int
	Problem string
}

// queryReply is how the skill is asked to answer.
const queryReply = `

Reply with JSON only, no fences: {"name": "<tool name>", "description": "<what it returns and when to use it>", "statement": "<the SQL>", "params": [{"name": "<name>", "type": "string|integer|float|boolean", "description": "<what it is>"}], "notes": ["<an assumption you made, one short line each, at most five>"]}. When the schema can't answer it, reply {"problem": "<what's missing>"} instead.`

// DraftConnectorQuery has the sql-query skill write a query for c, as
// written, from what the user is looking for (request) and c's schema.
// A schema that can't be read, or holds no table, is the draft's
// problem; so is a model that fails.
func (w *Workspace) DraftConnectorQuery(ctx context.Context, c connectors.Connection, values map[string]string, request string) (QueryDraft, error) {
	if strings.TrimSpace(request) == "" {
		return QueryDraft{}, fmt.Errorf("say what you're looking for first")
	}
	_, kind, err := w.connectorKind(c, connectors.QueryBuilder)
	if err != nil {
		return QueryDraft{}, err
	}
	skill, ok := w.skills.Get(SQLSkill)
	if !ok {
		return QueryDraft{}, fmt.Errorf("the %s skill isn't there", SQLSkill)
	}
	redact := SecretRedactor(w.cfg).String
	tables, err := w.ReadConnectorSchema(ctx, c, values)
	if err != nil {
		return QueryDraft{Problem: redact(err.Error())}, nil
	}
	llm, err := w.newModel(ctx, w.cfg, cmp.Or(w.cfg.Suggestions.Model, w.cfg.Permissions.Auto.Model))
	if err != nil {
		return QueryDraft{Tables: len(tables), Problem: ModelErrorSummary(err, w.cfg)}, nil
	}
	reply, served, usage, err := runtime.Ask(ctx, llm, skill.Content+queryReply, redact(queryPrompt(kind, c, tables, request)))
	if err != nil {
		return QueryDraft{Tables: len(tables), Problem: ModelErrorSummary(err, w.cfg)}, nil
	}
	if usage != nil {
		slog.Info("query drafted", "kind", kind.ID, "tables", len(tables), "model", served, "input_tokens", usage.PromptTokenCount, "output_tokens", usage.CandidatesTokenCount)
	}
	d := parseQueryDraft(reply, kind, c)
	d.Tables = len(tables)
	return d, nil
}

// queryPrompt is what the skill is given: the connection, its dialect and
// placeholders, the names taken, the schema, and the request.
func queryPrompt(k connectors.Kind, c connectors.Connection, tables []connectors.Table, request string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Connection: %s (%s)\n", c.Name, k.Name.In("en"))
	if c.Description != "" {
		fmt.Fprintf(&b, "It holds: %s\n", c.Description)
	}
	if q := k.Queries; q != nil {
		fmt.Fprintf(&b, "Dialect: %s\n", q.Dialect)
		if q.Placeholders == connectors.Named {
			b.WriteString("Parameters: by name, @name\n")
		} else {
			b.WriteString("Parameters: by position, $1, $2… in the order of params\n")
		}
	}
	if c.ReadOnly {
		b.WriteString("The connection is read-only: the query must only read.\n")
	}
	var taken []string
	for _, t := range k.Tools {
		taken = append(taken, t.ID)
	}
	for _, q := range c.Queries {
		taken = append(taken, q.Name)
	}
	if len(taken) > 0 {
		fmt.Fprintf(&b, "Tool names taken: %s\n", strings.Join(taken, ", "))
	}
	b.WriteString("\nThe schema:\n")
	b.WriteString(schemaText(tables, MaxSchemaText))
	fmt.Fprintf(&b, "\nWhat the user is looking for: %s\n", request)
	return b.String()
}

// schemaText is the tables as text, a table a line with its columns, cut
// at limit bytes.
func schemaText(tables []connectors.Table, limit int) string {
	var b strings.Builder
	for i, t := range tables {
		var line strings.Builder
		line.WriteString(t.Name)
		if t.About != "" {
			fmt.Fprintf(&line, " -- %s", t.About)
		}
		line.WriteString("\n")
		for _, c := range t.Columns {
			fmt.Fprintf(&line, "  %s %s", c.Name, c.Type)
			if c.About != "" {
				fmt.Fprintf(&line, " -- %s", c.About)
			}
			line.WriteString("\n")
		}
		if b.Len()+line.Len() > limit {
			fmt.Fprintf(&b, "(and %d more tables)\n", len(tables)-i)
			break
		}
		b.WriteString(line.String())
	}
	return b.String()
}

var (
	positionalRE = regexp.MustCompile(`\$(\d+)`)
	namedRE      = regexp.MustCompile(`@([A-Za-z_]\w*)`)
	notNameRE    = regexp.MustCompile(`[^a-z0-9_]+`)
)

// parseQueryDraft reads the skill's JSON reply (fences and text around it
// tolerated) into a query the form can take: a valid name not taken,
// parameters typed and matching the statement's placeholders, and a note
// when it may write.
func parseQueryDraft(reply string, k connectors.Kind, c connectors.Connection) QueryDraft {
	s := strings.TrimSpace(reply)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	var got struct {
		Name        string             `json:"name"`
		Description string             `json:"description"`
		Statement   string             `json:"statement"`
		Params      []connectors.Param `json:"params"`
		Notes       []string           `json:"notes"`
		Problem     string             `json:"problem"`
	}
	if err := json.Unmarshal([]byte(s), &got); err != nil {
		return QueryDraft{Problem: "the model's reply wasn't a query; try again"}
	}
	if p := strings.TrimSpace(got.Problem); p != "" && strings.TrimSpace(got.Statement) == "" {
		return QueryDraft{Problem: p}
	}
	if strings.TrimSpace(got.Statement) == "" {
		return QueryDraft{Problem: "the model's reply wasn't a query; try again"}
	}
	q := connectors.Query{
		Name:        freeName(got.Name, k, c),
		Description: strings.TrimSpace(got.Description),
		Statement:   strings.TrimSpace(got.Statement),
	}
	d := QueryDraft{}
	for _, n := range got.Notes {
		if n = strings.TrimSpace(n); n != "" && len(d.Notes) < 5 {
			d.Notes = append(d.Notes, n)
		}
	}
	seen := map[string]bool{}
	for _, p := range got.Params {
		p.Name = strings.Trim(notNameRE.ReplaceAllString(strings.ToLower(p.Name), "_"), "_")
		if !connectors.ValidName(p.Name) || seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		if !slices.Contains(connectors.ParamTypes, p.Type) {
			p.Type = "string"
		}
		q.Params = append(q.Params, p)
	}
	q.Params, d.Notes = matchPlaceholders(k, q.Statement, q.Params, d.Notes)
	if !databases.ReadsOnly(q.Statement) {
		d.Notes = append(d.Notes, "It may write: a read-only connection refuses it, and it asks before it runs.")
	}
	d.Query = q
	return d
}

// matchPlaceholders makes the parameters the statement's: by position,
// one for each $N (a missing one added as a string, extra ones dropped);
// by name, one for each @name.
func matchPlaceholders(k connectors.Kind, statement string, params []connectors.Param, notes []string) ([]connectors.Param, []string) {
	if k.Queries == nil {
		return params, notes
	}
	if k.Queries.Placeholders == connectors.Named {
		var out []connectors.Param
		for _, m := range namedRE.FindAllStringSubmatch(statement, -1) {
			name := strings.ToLower(m[1])
			if slices.ContainsFunc(out, func(p connectors.Param) bool { return p.Name == name }) {
				continue
			}
			if i := slices.IndexFunc(params, func(p connectors.Param) bool { return p.Name == name }); i >= 0 {
				out = append(out, params[i])
			} else {
				out = append(out, connectors.Param{Name: name, Type: "string"})
				notes = append(notes, fmt.Sprintf("@%s had no parameter: added as text", name))
			}
		}
		return out, notes
	}
	most := 0
	for _, m := range positionalRE.FindAllStringSubmatch(statement, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > most {
			most = n
		}
	}
	for len(params) < most {
		n := len(params) + 1
		params = append(params, connectors.Param{Name: fmt.Sprintf("p%d", n), Type: "string"})
		notes = append(notes, fmt.Sprintf("$%d had no parameter: added as text", n))
	}
	if most == 0 {
		return nil, notes
	}
	return params[:most], notes
}

// freeName is the skill's name for the query, made valid and not taken
// by the kind's tools or the connection's queries.
func freeName(name string, k connectors.Kind, c connectors.Connection) string {
	name = strings.Trim(notNameRE.ReplaceAllString(strings.ToLower(name), "_"), "_")
	if len(name) > 36 {
		name = strings.TrimRight(name[:36], "_")
	}
	if !connectors.ValidName(name) {
		name = "query"
	}
	taken := func(n string) bool {
		_, tool := k.Tool(n)
		return tool || slices.ContainsFunc(c.Queries, func(q connectors.Query) bool { return q.Name == n })
	}
	out := name
	for i := 2; taken(out); i++ {
		out = fmt.Sprintf("%s_%d", name, i)
	}
	return out
}
