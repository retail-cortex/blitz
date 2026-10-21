// Copyright 2025 Google LLC
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

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/sources"
	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/sources/sqlcommenter"
	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/util/orderedmap"
	"go.opentelemetry.io/otel/trace"
	// Blitz: SQLite with sqlite-vec compiled in (pure Go, WebAssembly
	// through wazero), not modernc.org/sqlite, which can't load
	// extensions: every SQLite source has vec0 tables and the vec_*
	// functions.
	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	_ "github.com/ncruces/go-sqlite3/driver"
)

const SourceType string = "sqlite"

// MaxRows bounds the rows a query returns (Blitz).
var MaxRows = 1000

// validate interface
var _ sources.SourceConfig = Config{}

func init() {
	if !sources.Register(SourceType, newConfig) {
		panic(fmt.Sprintf("source type %q already registered", SourceType))
	}
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (sources.SourceConfig, error) {
	actual := Config{Name: name}
	if err := decoder.DecodeContext(ctx, &actual); err != nil {
		return nil, err
	}
	return actual, nil
}

type Config struct {
	Name         string `yaml:"name" validate:"required"`
	Type         string `yaml:"type" validate:"required"`
	Database     string `yaml:"database" validate:"required"` // Path to SQLite database file
	SQLCommenter *bool  `yaml:"sqlCommenter"`
	// ReadOnly opens the file read-only (Blitz): writes fail in SQLite
	// itself, and tools that write aren't offered.
	ReadOnly bool `yaml:"readOnly"`
}

func (r Config) SourceConfigType() string {
	return SourceType
}

func (r Config) Initialize(ctx context.Context, tracer trace.Tracer) (sources.Source, error) {
	db, err := initSQLiteConnection(ctx, tracer, r.Name, r.Database, r.ReadOnly)
	if err != nil {
		return nil, fmt.Errorf("unable to create db connection: %w", err)
	}

	err = db.PingContext(context.Background())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("unable to connect successfully: %w", err)
	}

	s := &Source{
		Config: r,
		Db:     db,
	}
	return s, nil
}

var _ sources.Source = &Source{}

type Source struct {
	Config
	Db *sql.DB
}

// Close closes the database (Blitz: Toolbox never did).
func (s *Source) Close() error {
	if s.Db == nil {
		return nil
	}
	return s.Db.Close()
}

func (s *Source) IsReadOnly() bool {
	return s.ReadOnly
}

func (s *Source) SourceType() string {
	return SourceType
}

func (s *Source) ToConfig() sources.SourceConfig {
	return s.Config
}

func (s *Source) SQLiteDB() *sql.DB {
	return s.Db
}

func (s *Source) RunSQL(ctx context.Context, statement string, params []any) (any, error) {
	// Execute the SQL query with parameters
	statement = sqlcommenter.PrependComment(ctx, statement, SourceType, s.SQLCommenter)
	rows, err := s.SQLiteDB().QueryContext(ctx, statement, params...)
	if err != nil {
		return nil, fmt.Errorf("unable to execute query: %w", err)
	}
	defer rows.Close()

	// Get column names
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("unable to get column names: %w", err)
	}

	// The sqlite driver does not support ColumnTypes, so we can't get the
	// underlying database type of the columns. We'll have to rely on the
	// generic `any` type and then handle the JSON data separately.
	rawValues := make([]any, len(cols))
	values := make([]any, len(cols))
	for i := range rawValues {
		values[i] = &rawValues[i]
	}

	// Prepare the result slice
	out := []any{}
	for rows.Next() {
		// Blitz: rows past MaxRows aren't read, so a query over a big table
		// doesn't load all of it; the last row says so.
		if len(out) == MaxRows {
			note := orderedmap.Row{}
			note.Add("note", fmt.Sprintf("only the first %d rows: narrow the query (WHERE, LIMIT) for the rest", MaxRows))
			out = append(out, note)
			break
		}
		if err := rows.Scan(values...); err != nil {
			return nil, fmt.Errorf("unable to scan row: %w", err)
		}

		// Create a map for this row
		row := orderedmap.Row{}
		for i, name := range cols {
			val := rawValues[i]
			// Handle nil values
			if val == nil {
				row.Add(name, nil)
				continue
			}
			// Handle JSON data
			if jsonString, ok := val.(string); ok {
				var unmarshaledData any
				if json.Unmarshal([]byte(jsonString), &unmarshaledData) == nil {
					row.Add(name, unmarshaledData)
					continue
				}
			}
			// Store the value in the map
			row.Add(name, val)
		}
		out = append(out, row)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return out, nil
}

func initSQLiteConnection(ctx context.Context, tracer trace.Tracer, name, dbPath string, readOnly bool) (*sql.DB, error) {
	//nolint:all // Reassigned ctx
	ctx, span := sources.InitConnectionSpan(ctx, tracer, SourceType, name)
	defer span.End()

	// Open database connection
	db, err := sql.Open("sqlite3", sqliteDSN(dbPath, readOnly))
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	// Set some reasonable defaults for SQLite
	db.SetMaxOpenConns(1) // SQLite only supports one writer at a time
	db.SetMaxIdleConns(1)

	return db, nil
}

// sqliteDSN is the driver's name for a database file (Blitz): a file: URI
// with mode=ro when read-only, which SQLite enforces as it opens it.
// ":memory:" and URIs given as such are kept.
func sqliteDSN(path string, readOnly bool) string {
	if !readOnly {
		return path
	}
	if strings.HasPrefix(path, "file:") {
		if strings.Contains(path, "?") {
			return path + "&mode=ro"
		}
		return path + "?mode=ro"
	}
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?mode=ro"
}
