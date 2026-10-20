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
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/retail-cortex/blitz/pkg/engine/connectors"
)

// catalog is the built-in catalog, its databases category and its kinds.
func catalog(t *testing.T) (connectors.Catalog, connectors.Category, []connectors.Kind) {
	t.Helper()
	cat := connectors.Load("", "")
	c, ok := cat.Category("databases")
	require.True(t, ok)
	return cat, c, cat.KindsOf("databases", ID)
}

// kindOf is the built-in kind id.
func kindOf(t *testing.T, id string) *connectors.Kind {
	t.Helper()
	cat, _, _ := catalog(t)
	k, ok := cat.Kind(id)
	require.True(t, ok, id)
	return &k
}

// What Studio's form writes, it reads back, for every built-in kind.
func TestRenderParse(t *testing.T) {
	_, cat, kinds := catalog(t)
	for _, tc := range []struct {
		name string
		c    connectors.Connection
		has  []string
	}{
		{"postgres", connectors.Connection{Name: "shop", Kind: "postgres", Description: "The shop's orders.", Settings: map[string]string{"host": "db.local", "port": "5433", "database": "shop", "user": "reader", "password": "${DB_SHOP_PASSWORD}"}, ReadOnly: true, Tools: []string{"execute_sql", "list_tables"},
			Queries: []connectors.Query{{Name: "top_customers", Description: "The best customers", Statement: "SELECT name FROM customers\nORDER BY spent DESC\nLIMIT $1", Params: []connectors.Param{{Name: "limit", Type: "integer", Description: "How many"}}}}},
			[]string{"# kind: postgres", "password: ${DB_SHOP_PASSWORD}", "readOnly: true", "# about: The shop's orders.", "name: shop_top_customers", "type: postgres-sql", `port: "5433"`, "Run a SQL statement on shop (The shop's orders)."}},
		{"alloydb", connectors.Connection{Name: "ledger", Kind: "alloydb-postgres", Settings: map[string]string{"project": "p", "region": "us-central1", "cluster": "c", "instance": "i", "database": "ledger"}, Tools: []string{"list_schemas"}},
			[]string{"ipType: public", "type: postgres-list-schemas", "type: alloydb-postgres"}},
		{"sqlite", connectors.Connection{Name: "notes", Kind: "sqlite", Settings: map[string]string{"database": "/data/notes.db"}, ReadOnly: true, Tools: []string{"list_tables"}},
			[]string{"database: /data/notes.db", "statement: SELECT name, type, sql FROM sqlite_master"}},
		{"bigquery", connectors.Connection{Name: "sales", Kind: "bigquery", Settings: map[string]string{"project": "acme", "allowedDatasets": "eu, us", "maximumBytesBilled": "1000000000"}, ReadOnly: true, Tools: []string{"execute_sql", "list_datasets"}},
			[]string{"writeMode: blocked", "allowedDatasets: [eu, us]", "maximumBytesBilled: 1000000000", "type: bigquery-list-dataset-ids"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := Backend{}.Render(cat, kindOf(t, tc.c.Kind), tc.c)
			require.NoError(t, err)
			for _, h := range tc.has {
				assert.Contains(t, text, h)
			}
			got := Backend{}.Parse(cat, kinds, tc.c.Name, text)
			assert.False(t, got.Custom, text)
			assert.Equal(t, tc.c.Kind, got.Kind)
			assert.Equal(t, tc.c.ReadOnly, got.ReadOnly)
			assert.Equal(t, tc.c.Tools, got.Tools)
			assert.Equal(t, tc.c.Queries, got.Queries)
			assert.Equal(t, strings.TrimSpace(tc.c.Description), got.Description)
			for k, v := range tc.c.Settings {
				assert.Equal(t, v, got.Settings[k], k)
			}
			_, err = loadConfig(context.Background(), []string{writeTemp(t, text)}, map[string]string{"DB_SHOP_PASSWORD": "pw"})
			assert.NoError(t, err, "Toolbox reads it")
		})
	}
}

// A hand-written file names no kind: the first kind of its source type
// is its kind; Toolbox's own readOnly makes any source read-only.
func TestParseByType(t *testing.T) {
	_, cat, kinds := catalog(t)
	c := Backend{}.Parse(cat, kinds, "ledger", "kind: source\nname: ledger\ntype: alloydb-postgres\nproject: p\nregion: r\ncluster: c\ninstance: i\ndatabase: d\n")
	assert.False(t, c.Custom)
	assert.Equal(t, "alloydb-postgres", c.Kind)
	c = Backend{}.Parse(cat, kinds, "sales", "kind: source\nname: sales\ntype: bigquery\nproject: p\nreadOnly: true\n")
	assert.False(t, c.Custom)
	assert.True(t, c.ReadOnly)
}

func writeTemp(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.yaml")
	require.NoError(t, os.WriteFile(p, []byte(text), 0o600))
	return p
}

// A file that's more than the form describes is Custom; a broken one
// says why.
func TestParseCustom(t *testing.T) {
	_, cat, kinds := catalog(t)
	for name, text := range map[string]string{
		"two sources":     "kind: source\nname: a\ntype: sqlite\ndatabase: x\n---\nkind: source\nname: b\ntype: sqlite\ndatabase: y\n",
		"another name":    "kind: source\nname: other\ntype: sqlite\ndatabase: x\n",
		"an unknown type": "kind: source\nname: a\ntype: mysql\nhost: x\n",
		"another setting": "kind: source\nname: a\ntype: postgres\nhost: x\nport: 1\ndatabase: d\nuser: u\npassword: p\nqueryParams: {sslmode: require}\n",
		"another tool":    "kind: source\nname: a\ntype: postgres\nhost: x\n---\nkind: tool\nname: a_locks\ntype: postgres-list-locks\nsource: a\ndescription: x\n",
		"a foreign tool":  "kind: source\nname: a\ntype: postgres\nhost: x\n---\nkind: tool\nname: locks\ntype: postgres-list-locks\nsource: a\ndescription: x\n",
		"a toolset":       "kind: source\nname: a\ntype: sqlite\ndatabase: x\n---\nkind: toolset\nname: all\ntools: [a_x]\n",
		"no source":       "kind: tool\nname: a_x\ntype: sqlite-sql\nsource: a\n",
	} {
		t.Run(name, func(t *testing.T) {
			c := Backend{}.Parse(cat, kinds, "a", text)
			assert.True(t, c.Custom)
			assert.Equal(t, text, c.Text)
		})
	}
	c := Backend{}.Parse(cat, kinds, "a", "kind: [")
	assert.True(t, c.Custom)
	assert.NotEmpty(t, c.Problem)
	assert.Contains(t, strings.Join(Backend{}.Check(nil, c), " "), "YAML")
	text, err := Backend{}.Render(cat, nil, connectors.Connection{Custom: true, Text: "kind: source\n"})
	require.NoError(t, err)
	assert.Equal(t, "kind: source\n", text, "a custom file is written as it is")
}

// The backend says what it can do; it's registered with the connectors.
func TestBackend(t *testing.T) {
	b, ok := connectors.BackendOf(ID)
	require.True(t, ok)
	assert.Equal(t, ID, b.ID())
	assert.Equal(t, connectors.Capabilities, b.Capabilities())
	assert.True(t, ReadsOnly("SELECT 1"))
	assert.False(t, ReadsOnly("DELETE FROM x"))
}
