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

package embed

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	bigqueryapi "cloud.google.com/go/bigquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bigqueryrestapi "google.golang.org/api/bigquery/v2"

	bigqueryds "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/sources/bigquery"
)

// shop is a SQLite file with typed columns.
func shop(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shop.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE orders(id INTEGER NOT NULL, customer VARCHAR(80), total NUMERIC(10,2), paid BOOLEAN, placed DATE);
INSERT INTO orders VALUES (1042, 'Ada', 12.5, 1, '2026-11-07'), (1043, 'Grace', 3, 0, '2026-11-08'), (1044, NULL, 7.25, 1, NULL)`)
	require.NoError(t, err)
	return path
}

func shopConfig(t *testing.T, readOnly bool) Config {
	t.Helper()
	yaml := "kind: source\nname: shop\ntype: sqlite\ndatabase: ${DB_PATH}\n"
	if readOnly {
		yaml += "readOnly: true\n"
	}
	cfg, err := Load(context.Background(), []string{writeConfig(t, yaml)}, map[string]string{"DB_PATH": shop(t)})
	require.NoError(t, err)
	return cfg
}

// A query's result with the driver's column metadata (spec_059_data
// DAT-10): names in order, declared types and lengths, NULLs; limits.
func TestQuery(t *testing.T) {
	ctx := context.Background()
	cfg := shopConfig(t, true)
	r, err := Query(ctx, cfg, "shop", "SELECT customer, id, total, paid, placed FROM orders ORDER BY id", QueryOptions{MaxRows: 100, MaxBytes: 0})
	require.NoError(t, err)
	var names, types []string
	for _, c := range r.Columns {
		names = append(names, c.Name)
		types = append(types, c.DBType)
	}
	assert.Equal(t, []string{"customer", "id", "total", "paid", "placed"}, names, "the query's order, not the map's")
	// SQLite's driver gives a declared type without its length.
	assert.Equal(t, []string{"varchar", "integer", "numeric", "boolean", "date"}, types)
	assert.Nil(t, r.Columns[0].Length)
	require.Len(t, r.Rows, 3)
	assert.Equal(t, "Ada", r.Rows[0][0])
	assert.Nil(t, r.Rows[2][0], "NULL")
	assert.False(t, r.Truncated)

	r, err = Query(ctx, cfg, "shop", "SELECT id FROM orders WHERE total > ? ORDER BY id", QueryOptions{Params: []any{5}, MaxRows: 1, MaxBytes: 0})
	require.NoError(t, err)
	assert.Len(t, r.Rows, 1)
	assert.True(t, r.Truncated, "a second row was there")
	r, err = Query(ctx, cfg, "shop", "SELECT customer FROM orders", QueryOptions{MaxRows: 100, MaxBytes: 10})
	require.NoError(t, err)
	assert.Len(t, r.Rows, 1, "the bytes' limit, one row at least")
	assert.True(t, r.Truncated)

	_, err = Query(ctx, cfg, "shop", "DELETE FROM orders", QueryOptions{MaxRows: 100, MaxBytes: 0})
	assert.ErrorIs(t, err, ErrWrites)
	_, err = Query(ctx, cfg, "nope", "SELECT 1", QueryOptions{MaxRows: 100, MaxBytes: 0})
	assert.ErrorContains(t, err, `no source "nope"`)
	_, err = Query(ctx, cfg, "shop", "SELECT * FROM missing", QueryOptions{MaxRows: 100, MaxBytes: 0})
	assert.Error(t, err)

	// A source that may write runs a write.
	rw := shopConfig(t, false)
	r, err = Query(ctx, rw, "shop", "UPDATE orders SET paid = 1 WHERE id = 1043 RETURNING id", QueryOptions{MaxRows: 100, MaxBytes: 0})
	require.NoError(t, err)
	assert.Len(t, r.Rows, 1)
}

func TestPostgresLength(t *testing.T) {
	n := func(p *int64) any {
		if p == nil {
			return nil
		}
		return *p
	}
	assert.EqualValues(t, 80, n(postgresLength("varchar", 84)))
	assert.EqualValues(t, 10, n(postgresLength("numeric", int32((10<<16|2)+4))))
	assert.EqualValues(t, 8, n(postgresLength("bit", 8)))
	assert.Nil(t, n(postgresLength("int4", -1)))
	assert.Nil(t, n(postgresLength("text", 40)))
}

// BigQuery's parameters, columns and write modes, without a project.
func TestBigQueryParts(t *testing.T) {
	rest, params := bigQueryParams([]any{int64(3), 2.5, true, "x", nil, time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)})
	var types, texts []string
	for _, p := range rest {
		types = append(types, p.ParameterType.Type)
		texts = append(texts, p.ParameterValue.Value)
	}
	assert.Equal(t, []string{"INT64", "FLOAT64", "BOOL", "STRING", "STRING", "TIMESTAMP"}, types)
	assert.Equal(t, []string{"3", "2.5", "true", "x", "", "2026-11-07T00:00:00Z"}, texts)
	assert.Equal(t, []string{"Value"}, rest[4].ParameterValue.NullFields)
	assert.Len(t, params, 6)
	r, p := bigQueryParams(nil)
	assert.Nil(t, r)
	assert.Nil(t, p)

	cols := bigQueryColumns(bigqueryapi.Schema{
		{Name: "id", Type: bigqueryapi.IntegerFieldType, Required: true, Description: "The order"},
		{Name: "name", Type: bigqueryapi.StringFieldType, MaxLength: 80},
		{Name: "tags", Type: bigqueryapi.StringFieldType, Repeated: true},
	})
	assert.Equal(t, "INTEGER", cols[0].DBType)
	assert.False(t, *cols[0].Nullable)
	assert.Equal(t, "The order", cols[0].Description)
	assert.EqualValues(t, 80, *cols[1].Length)
	assert.True(t, *cols[1].Nullable)
	assert.Equal(t, "ARRAY", cols[2].DBType)

	job := func(typ, dest string) *bigqueryrestapi.Job {
		j := &bigqueryrestapi.Job{Statistics: &bigqueryrestapi.JobStatistics{Query: &bigqueryrestapi.JobStatistics2{StatementType: typ}}}
		if dest != "" {
			j.Configuration = &bigqueryrestapi.JobConfiguration{Query: &bigqueryrestapi.JobConfigurationQuery{DestinationTable: &bigqueryrestapi.TableReference{DatasetId: dest}}}
		}
		return j
	}
	assert.NoError(t, bigQueryWriteMode(bigqueryds.WriteModeBlocked, job("SELECT", ""), nil))
	assert.ErrorIs(t, bigQueryWriteMode(bigqueryds.WriteModeBlocked, job("DELETE", ""), nil), ErrWrites)
	sess := &bigqueryds.Session{DatasetID: "_session"}
	assert.NoError(t, bigQueryWriteMode(bigqueryds.WriteModeProtected, job("CREATE_TABLE", "_session"), sess))
	assert.Error(t, bigQueryWriteMode(bigqueryds.WriteModeProtected, job("INSERT", "sales"), sess))
	assert.NoError(t, bigQueryWriteMode(bigqueryds.WriteModeAllowed, job("DELETE", ""), nil))
}
