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
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/api"
	"github.com/retail-cortex/blitz/pkg/engine/connectors"
	"github.com/retail-cortex/blitz/pkg/engine/datatable"
	"github.com/retail-cortex/blitz/pkg/engine/session"
	"github.com/retail-cortex/blitz/pkg/secrets"
)

// A database added to a turn is its schema and its tools; a query's
// result is its SQL, columns and rows; @db: mentions add either; what
// can't be used says why (spec_059_data DAT-41..DAT-44).
func TestWithData(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w := openTest(t)
	ctx := context.Background()
	c := kitchen(t)
	c.Tools = []string{"list_tables"}
	c.Queries = append(c.Queries, connectors.Query{Name: "longer", Description: "Longer than", Statement: "SELECT name FROM recipes WHERE minutes > ?", Params: []connectors.Param{{Name: "m", Type: "integer", Description: "Minutes"}}})
	saved, probs, err := w.SaveConnector(ctx, c, nil, "")
	require.NoError(t, err)
	require.Empty(t, probs)

	got := w.withData(ctx, "", []api.ContextBlock{{Kind: "database", Title: "kitchen", Connection: "kitchen"}})
	require.Len(t, got, 1)
	for _, want := range []string{"Database connection kitchen (sqlite, read-only)", "- recipes", "  - name TEXT", "  - minutes INTEGER", "db__kitchen_list_tables, db__kitchen_count"} {
		assert.Contains(t, got[0].Text, want)
	}

	n := int64(80)
	table, err := json.Marshal(datatable.New([]datatable.Column{{Name: "name", DBType: "varchar", Length: &n, Description: "The dish"}}, [][]any{{"soup"}, {"bread"}}, true))
	require.NoError(t, err)
	got = w.withData(ctx, "", []api.ContextBlock{{Kind: "data_query", Title: "kitchen · SQL", Connection: "kitchen", Statement: "SELECT name FROM recipes", Text: string(table)}})
	for _, want := range []string{"on the database connection kitchen", "```sql\nSELECT name FROM recipes\n```", "- name (string, 80): The dish", "Rows (2):", "| soup |", "query the connection for the rest"} {
		assert.Contains(t, got[0].Text, want)
	}
	assert.Equal(t, "why it failed", queryText(api.ContextBlock{Text: "why it failed"}), "not a table: as it is")

	got = w.withData(ctx, "how many? @db:kitchen/count and @db:kitchen, @db:kitchen/longer @db:kitchen/nope @db:gone", nil)
	require.Len(t, got, 5)
	assert.Contains(t, got[0].Text, "| 2 |", "a saved query, run")
	assert.Equal(t, "SELECT count(*) AS n FROM recipes", got[0].Statement)
	assert.Contains(t, got[1].Text, "- recipes")
	assert.Contains(t, got[2].Text, "takes parameters")
	assert.Contains(t, got[3].Text, `no query "nope"`)
	assert.Contains(t, got[4].Text, `no database connection "gone"`)
	got = w.withData(ctx, "@db:kitchen", []api.ContextBlock{{Kind: "database", Connection: "kitchen"}})
	assert.Len(t, got, 1, "a mention of a database added already adds nothing")
	assert.Equal(t, "\n[added: database: kitchen, kitchen · count]", AddedNote([]api.ContextBlock{{Kind: "database", Title: "kitchen"}, {Kind: "data_query", Title: "kitchen · count"}}))

	// A clone's file isn't trusted: it isn't read (DAT-44).
	text, err := os.ReadFile(saved.Path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(saved.Path, append(text, '\n'), 0o600))
	got = w.withData(ctx, "@db:kitchen @db:kitchen/count", nil)
	assert.Contains(t, got[0].Text, "isn't trusted")
	assert.Contains(t, got[1].Text, "isn't trusted")
	_, _, err = w.SaveConnector(ctx, w.Connectors()[0], nil, "kitchen")
	require.NoError(t, err)
	require.NoError(t, w.SetConnectorOn(ctx, "databases", connectors.ScopeWorkspace, "kitchen", false))
	assert.Contains(t, w.withData(ctx, "@db:kitchen", nil)[0].Text, "is off")
	assert.Contains(t, chatConnectionText(t, []connectors.Connection{{Name: "b", Category: "databases", Problem: "unreadable"}}), "can't be read")
}

func chatConnectionText(t *testing.T, conns []connectors.Connection) string {
	t.Helper()
	_, err := chatConnection(conns, "b")
	require.Error(t, err)
	return err.Error()
}

// A turn records what was added and gives the agent the database's text.
func TestTurnWithData(t *testing.T) {
	secrets.SetDefault(&secrets.Memory{})
	w, _ := openTestWith(t, nil, text("two recipes"))
	ctx := context.Background()
	_, _, err := w.SaveConnector(ctx, kitchen(t), nil, "")
	require.NoError(t, err)
	s := newSession(t, w)
	_, err = w.Run(ctx, s.ID, api.Turn{Text: "how many? @db:kitchen/count"}, func(api.Event) {})
	require.NoError(t, err)
	msgs := w.storage.Active().Messages
	i := slices.IndexFunc(msgs, func(m session.Message) bool { return m.Role == "user" })
	require.GreaterOrEqual(t, i, 0)
	assert.True(t, strings.HasSuffix(msgs[i].Content, "[added: kitchen · count]"), msgs[i].Content)
}
