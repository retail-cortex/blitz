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
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/model"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// The query builder reads the connection's schema and gives it, with what
// the user is looking for, to the sql-query skill; its reply is the
// draft, nothing saved.
func TestDraftConnectorQuery(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	ctx := context.Background()
	w := openTest(t)
	llm := &promptLLM{reply: `{"name": "Quick Recipes", "description": "Recipes ready in under some minutes", "statement": "SELECT name FROM recipes WHERE minutes < $1 ORDER BY minutes LIMIT 10", "params": [{"name": "minutes", "type": "integer", "description": "At most"}], "notes": ["minutes is the cooking time"]}`}
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }
	c := kitchen(t)
	c.Description = "Our recipes"
	d, err := w.DraftConnectorQuery(ctx, c, nil, "recipes that are quick to make")
	require.NoError(t, err)
	require.Empty(t, d.Problem)
	assert.Equal(t, 1, d.Tables)
	assert.Equal(t, "quick_recipes", d.Query.Name)
	assert.Equal(t, "SELECT name FROM recipes WHERE minutes < $1 ORDER BY minutes LIMIT 10", d.Query.Statement)
	assert.Equal(t, []connectors.Param{{Name: "minutes", Type: "integer", Description: "At most"}}, d.Query.Params)
	assert.Equal(t, []string{"minutes is the cooking time"}, d.Notes)
	require.Len(t, llm.prompts, 1)
	for _, want := range []string{"kitchen (SQLite)", "Our recipes", "Dialect: SQLite", "$1, $2", "read-only", "Tool names taken: execute_sql, list_tables, count", "recipes\n  name TEXT\n  minutes INTEGER", "quick to make"} {
		assert.Contains(t, llm.prompts[0], want)
	}

	llm.err = errors.New("quota exceeded")
	d, err = w.DraftConnectorQuery(ctx, c, nil, "x")
	require.NoError(t, err)
	assert.Contains(t, d.Problem, "quota exceeded")
	assert.Equal(t, 1, d.Tables)

	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return nil, errors.New("no key") }
	d, err = w.DraftConnectorQuery(ctx, c, nil, "x")
	require.NoError(t, err)
	assert.Contains(t, d.Problem, "no key")
}

// No table read is the draft's problem, before any model is asked; a
// connection that can't have a query builder is refused.
func TestDraftConnectorQueryRefuses(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	ctx := context.Background()
	w := openTest(t)
	llm := &promptLLM{reply: "{}"}
	w.newModel = func(context.Context, *config.Config, string) (model.LLM, error) { return llm, nil }

	empty := filepath.Join(t.TempDir(), "empty.db")
	db, err := sql.Open("sqlite3", empty)
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA user_version = 1")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	c := kitchen(t)
	c.Settings = map[string]string{"database": empty}
	d, err := w.DraftConnectorQuery(ctx, c, nil, "anything")
	require.NoError(t, err)
	assert.Contains(t, d.Problem, "no tables")
	assert.Empty(t, llm.prompts, "the model isn't asked")

	_, err = w.DraftConnectorQuery(ctx, c, nil, " ")
	assert.ErrorContains(t, err, "say what")
	_, err = w.DraftConnectorQuery(ctx, connectors.Connection{Name: "raw", Category: "databases", Custom: true}, nil, "x")
	assert.ErrorIs(t, err, ErrNoQueryBuilder)
	_, err = w.DraftConnectorQuery(ctx, connectors.Connection{Name: "x", Category: "email"}, nil, "x")
	assert.ErrorIs(t, err, connectors.ErrUnknown)
}

// The skill's reply becomes a query the form can take.
func TestParseQueryDraft(t *testing.T) {
	cat := connectors.Load("", "")
	pg, _ := cat.Kind("postgres")
	bq, _ := cat.Kind("bigquery")
	conn := connectors.Connection{Name: "shop", Queries: []connectors.Query{{Name: "orders"}}}
	for _, tc := range []struct {
		name    string
		kind    connectors.Kind
		reply   string
		want    connectors.Query
		notes   []string
		problem string
	}{
		{name: "fenced", kind: pg, reply: "```json\n{\"name\": \"top\", \"description\": \"d\", \"statement\": \"SELECT 1\"}\n```", want: connectors.Query{Name: "top", Description: "d", Statement: "SELECT 1"}},
		{name: "a name taken, a tool's or a query's", kind: pg, reply: `{"name": "orders", "statement": "SELECT 1"}`, want: connectors.Query{Name: "orders_2", Statement: "SELECT 1"}},
		{name: "a ready-made tool's name", kind: pg, reply: `{"name": "list_tables", "statement": "SELECT 1"}`, want: connectors.Query{Name: "list_tables_2", Statement: "SELECT 1"}},
		{name: "no name", kind: pg, reply: `{"name": "42!", "statement": "SELECT 1"}`, want: connectors.Query{Name: "query", Statement: "SELECT 1"}},
		{name: "a long name", kind: pg, reply: `{"name": "` + strings.Repeat("ab", 30) + `", "statement": "SELECT 1"}`, want: connectors.Query{Name: strings.Repeat("ab", 18), Statement: "SELECT 1"}},
		{name: "positional parameters filled and trimmed", kind: pg, reply: `{"name": "q", "statement": "SELECT $1, $2", "params": [{"name": "A", "type": "date"}]}`,
			want: connectors.Query{Name: "q", Statement: "SELECT $1, $2", Params: []connectors.Param{{Name: "a", Type: "string"}, {Name: "p2", Type: "string"}}}, notes: []string{"$2 had no parameter: added as text"}},
		{name: "extra positional ones dropped", kind: pg, reply: `{"name": "q", "statement": "SELECT 1", "params": [{"name": "a", "type": "integer"}, {"name": "a", "type": "integer"}]}`,
			want: connectors.Query{Name: "q", Statement: "SELECT 1"}},
		{name: "named parameters", kind: bq, reply: `{"name": "q", "statement": "SELECT * FROM t WHERE d = @day AND s = @Shop AND e = @day", "params": [{"name": "day", "type": "string", "description": "When"}]}`,
			want: connectors.Query{Name: "q", Statement: "SELECT * FROM t WHERE d = @day AND s = @Shop AND e = @day", Params: []connectors.Param{{Name: "day", Type: "string", Description: "When"}, {Name: "shop", Type: "string"}}}, notes: []string{"@shop had no parameter: added as text"}},
		{name: "a write", kind: pg, reply: `{"name": "wipe", "statement": "DELETE FROM orders"}`, want: connectors.Query{Name: "wipe", Statement: "DELETE FROM orders"}, notes: []string{"It may write: a read-only connection refuses it, and it asks before it runs."}},
		{name: "the skill can't", kind: pg, reply: `{"problem": "no table holds refunds"}`, problem: "no table holds refunds"},
		{name: "not JSON", kind: pg, reply: "Here you go: SELECT 1", problem: "wasn't a query"},
		{name: "no statement", kind: pg, reply: `{"name": "q"}`, problem: "wasn't a query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := parseQueryDraft(tc.reply, tc.kind, conn)
			if tc.problem != "" {
				assert.Contains(t, d.Problem, tc.problem)
				return
			}
			assert.Empty(t, d.Problem)
			assert.Equal(t, tc.want, d.Query)
			assert.Equal(t, tc.notes, d.Notes)
		})
	}
}

// The schema is a table a line, its columns under it, cut when it's too
// long.
func TestSchemaText(t *testing.T) {
	tables := []connectors.Table{
		{Name: "public.orders", About: "Every order", Columns: []connectors.Column{{Name: "id", Type: "integer"}, {Name: "total", Type: "numeric", About: "In dollars"}}},
		{Name: "public.customers", Columns: []connectors.Column{{Name: "id", Type: "integer"}}},
	}
	assert.Equal(t, "public.orders -- Every order\n  id integer\n  total numeric -- In dollars\npublic.customers\n  id integer\n", schemaText(tables, 1000))
	assert.Equal(t, "public.orders -- Every order\n  id integer\n  total numeric -- In dollars\n(and 1 more tables)\n", schemaText(tables, 80))
}
