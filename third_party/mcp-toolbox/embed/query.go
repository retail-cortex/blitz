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
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace/noop"
)

// Column is a query result's column as its database describes it: its
// name, the database's type, its declared length (nil when the type has
// none), whether it may be NULL (nil when the database doesn't say), and
// its comment in the schema when it comes from a table that has one.
type Column struct {
	Name        string
	DBType      string
	Length      *int64
	Nullable    *bool
	Description string
}

// Result is a query's columns and rows (each row's values in the columns'
// order, as the driver read them), and whether a limit cut it.
type Result struct {
	Columns   []Column
	Rows      [][]any
	Truncated bool
}

// ErrWrites means a statement that writes, on a read-only source.
var ErrWrites = errors.New("the connection is read-only: this statement writes")

// ErrNoQuery means a source Query can't run statements on (BigQuery, whose
// queries are billed: the caller estimates first).
var ErrNoQuery = errors.New("this kind of source can't be queried here")

// Query runs statement (its parameters params) on cfg's source, read-only
// when the source is, and reads its result with the driver's column
// metadata (spec_059_data DAT-10): at most maxRows rows and about maxBytes
// of values (the rest cut: Truncated). The source is opened for the query
// and closed after it.
func Query(ctx context.Context, cfg Config, source, statement string, params []any, maxRows, maxBytes int) (Result, error) {
	sc, ok := cfg.Sources[source]
	if !ok {
		return Result{}, fmt.Errorf("no source %q", source)
	}
	if sourceReadOnly(sc) && !ReadsOnly(statement) {
		return Result{}, ErrWrites
	}
	src, err := sc.Initialize(ctx, noop.NewTracerProvider().Tracer("blitz"))
	if err != nil {
		return Result{}, err
	}
	if c, ok := src.(interface{ Close() error }); ok {
		defer c.Close()
	}
	switch s := src.(type) {
	case interface{ PostgresPool() *pgxpool.Pool }:
		return queryPostgres(ctx, s.PostgresPool(), src.IsReadOnly(), statement, params, maxRows, maxBytes)
	case interface{ SQLiteDB() *sql.DB }:
		return querySQL(ctx, s.SQLiteDB(), statement, params, maxRows, maxBytes)
	}
	return Result{}, ErrNoQuery
}

// budget counts the rows and bytes read against the limits.
type budget struct {
	rows, bytes   int
	maxRows, maxB int
}

// fits reports whether one more row fits, counting its bytes as JSON, and
// counts it when it does.
func (b *budget) fits(row []any) bool {
	if b.rows >= b.maxRows {
		return false
	}
	n := 0
	if data, err := json.Marshal(row); err == nil {
		n = len(data)
	}
	if b.maxB > 0 && b.rows > 0 && b.bytes+n > b.maxB {
		return false
	}
	b.rows++
	b.bytes += n
	return true
}

// queryPostgres runs statement on pool (in a read-only transaction for a
// read-only source, rolled back after), each column described from
// pg_catalog: its type's name, its length from the type modifier, its
// table's comment.
func queryPostgres(ctx context.Context, pool *pgxpool.Pool, readOnly bool, statement string, params []any, maxRows, maxBytes int) (Result, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	var q interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
		QueryRow(context.Context, string, ...any) pgx.Row
	} = conn
	if readOnly {
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			return Result{}, err
		}
		defer tx.Rollback(ctx) //nolint:errcheck // read-only: nothing to keep
		q = tx
	}
	rows, err := q.Query(ctx, statement, params...)
	if err != nil {
		return Result{}, err
	}
	fields := rows.FieldDescriptions()
	var out Result
	tm := conn.Conn().TypeMap()
	for _, f := range fields {
		col := Column{Name: f.Name}
		if t, ok := tm.TypeForOID(f.DataTypeOID); ok {
			col.DBType = t.Name
		} else {
			col.DBType = strconv.FormatUint(uint64(f.DataTypeOID), 10)
		}
		col.Length = postgresLength(col.DBType, f.TypeModifier)
		out.Columns = append(out.Columns, col)
	}
	b := budget{maxRows: maxRows, maxB: maxBytes}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			rows.Close()
			return Result{}, err
		}
		if !b.fits(vals) {
			out.Truncated = true
			break
		}
		out.Rows = append(out.Rows, vals)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	// Descriptions and nullability, for columns that come from a table.
	for i, f := range fields {
		if f.TableOID == 0 || f.TableAttributeNumber == 0 {
			continue
		}
		var desc *string
		var notNull bool
		err := q.QueryRow(ctx, "SELECT col_description($1::oid, $2::int), a.attnotnull FROM pg_attribute a WHERE a.attrelid = $1::oid AND a.attnum = $2::int", f.TableOID, int(f.TableAttributeNumber)).Scan(&desc, &notNull)
		if err != nil {
			continue
		}
		if desc != nil {
			out.Columns[i].Description = *desc
		}
		nullable := !notNull
		out.Columns[i].Nullable = &nullable
	}
	return out, nil
}

// postgresLength is a column's declared length from its type modifier:
// character types' length, numeric's precision; nil for others.
func postgresLength(typ string, mod int32) *int64 {
	if mod < 4 {
		return nil
	}
	var n int64
	switch typ {
	case "varchar", "bpchar", "char":
		n = int64(mod - 4)
	case "numeric":
		n = int64((mod - 4) >> 16 & 0xffff)
	case "bit", "varbit":
		n = int64(mod)
	default:
		return nil
	}
	return &n
}

// declaredLength is a declared type's length: VARCHAR(80) is 80.
var declaredLength = regexp.MustCompile(`\(\s*(\d+)`)

// querySQL runs statement on db (SQLite: a read-only source's file is
// opened read-only), each column described from its declared type (its
// length when the driver keeps it: SQLite's doesn't).
func querySQL(ctx context.Context, db *sql.DB, statement string, params []any, maxRows, maxBytes int) (Result, error) {
	rows, err := db.QueryContext(ctx, statement, params...)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return Result{}, err
	}
	var out Result
	for _, ct := range types {
		col := Column{Name: ct.Name(), DBType: strings.ToLower(ct.DatabaseTypeName())}
		if m := declaredLength.FindStringSubmatch(ct.DatabaseTypeName()); m != nil {
			if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				col.Length = &n
			}
		}
		if nullable, ok := ct.Nullable(); ok {
			col.Nullable = &nullable
		}
		out.Columns = append(out.Columns, col)
	}
	b := budget{maxRows: maxRows, maxB: maxBytes}
	for rows.Next() {
		vals := make([]any, len(types))
		ptrs := make([]any, len(types))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return Result{}, err
		}
		if !b.fits(vals) {
			out.Truncated = true
			break
		}
		out.Rows = append(out.Rows, vals)
	}
	return out, rows.Err()
}
