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

// Package embed runs MCP Toolbox for Databases inside Blitz's own process
// (spec_databases_046): a pruned copy of Toolbox (third_party/mcp-toolbox)
// with only the databases Blitz offers (Postgres, AlloyDB, SQLite with
// sqlite-vec, BigQuery), its config parser (config.go, taken from
// Toolbox's cmd/internal), and Start, which serves one config's tools as
// MCP over an in-memory pipe for Blitz's MCP client.
//
// Toolbox's command line isn't used: its signal handling, its version
// check (a call home) and its global OpenTelemetry setup would act on
// Blitz's process; here none of them runs.
package embed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/log"
	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/server"
	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/telemetry"
	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools"
	"github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/util"

	// The databases Blitz offers: their sources and tools register
	// themselves (Toolbox's cmd/internal/imports.go imports every one).
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/sources/alloydbpg"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/sources/bigquery"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/sources/postgres"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/sources/sqlite"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/alloydbainl"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/bigquery/bigqueryanalyzecontribution"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/bigquery/bigqueryexecutesql"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/bigquery/bigqueryforecast"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/bigquery/bigquerygetdatasetinfo"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/bigquery/bigquerygettableinfo"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/bigquery/bigquerylistdatasetids"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/bigquery/bigquerylisttableids"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/bigquery/bigquerysearchcatalog"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/bigquery/bigquerysql"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgresdatabaseoverview"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgresexecutesql"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgresgetcolumncardinality"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistactivequeries"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistavailableextensions"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistdatabasestats"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistindexes"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistinstalledextensions"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistlocks"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistpgsettings"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistpublicationtables"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistquerystats"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistroles"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistschemas"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistsequences"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgresliststoredprocedure"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslisttables"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslisttablespaces"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslisttablestats"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslisttriggers"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslistviews"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgreslongrunningtransactions"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgresreplicationstats"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/postgres/postgressql"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/sqlite/sqliteexecutesql"
	_ "github.com/retail-cortex/blitz/third_party/mcp-toolbox/internal/tools/sqlite/sqlitesql"
)

// Version is the Toolbox release this copy was taken from.
const Version = "1.14.0"

// SourceTypes are the database sources this copy offers.
var SourceTypes = []string{"postgres", "alloydb-postgres", "sqlite", "bigquery"}

// Load reads and merges files (Toolbox's YAML: sources, tools, groups),
// filling each ${NAME} (or ${NAME:default}) from env alone, never from the
// process's environment: Blitz puts secrets there. A ${NAME} with no value
// and no default is an error, naming it.
func Load(ctx context.Context, files []string, env map[string]string) (Config, error) {
	if len(files) == 0 {
		return Config{}, errors.New("no database files")
	}
	vars := make(map[string]string, len(env))
	for k, v := range env {
		vars[k] = v
	}
	p := &ConfigParser{EnvVars: vars}
	cfg, err := p.LoadAndMergeConfigs(ctx, files)
	if err != nil {
		return cfg, err
	}
	markReads(cfg)
	return cfg, nil
}

// markReads marks the custom SQL tools whose statement only reads, and
// that don't say, read-only (readOnlyHint): Toolbox takes an unmarked one
// for a write, hiding it on a read-only source.
func markReads(cfg Config) {
	yes := true
	for name, tc := range cfg.Tools {
		rv := reflect.ValueOf(tc)
		if rv.Kind() != reflect.Struct {
			continue
		}
		stmt, ann := rv.FieldByName("Statement"), rv.FieldByName("Annotations")
		if stmt.Kind() != reflect.String || ann.Kind() != reflect.Pointer || !ann.IsNil() || !ReadsOnly(stmt.String()) {
			continue
		}
		cp := reflect.New(rv.Type()).Elem()
		cp.Set(rv)
		cp.FieldByName("Annotations").Set(reflect.ValueOf(&tools.ToolAnnotations{ReadOnlyHint: &yes}))
		cfg.Tools[name] = cp.Interface().(tools.ToolConfig)
	}
}

var (
	// readVerbs start a statement that reads.
	readVerbs = map[string]bool{"select": true, "with": true, "explain": true, "show": true, "pragma": true, "values": true, "describe": true, "table": true}
	// writeWords in a statement may make it write (WITH … DELETE).
	writeWords = regexp.MustCompile(`(?i)\b(insert|update|delete|merge|upsert|create|drop|alter|truncate|grant|revoke|copy|call|vacuum|attach|detach|reindex)\b`)
	sqlComment = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)
)

// ReadsOnly says whether a SQL statement only reads: it starts with
// SELECT, WITH, EXPLAIN, SHOW, PRAGMA, VALUES, DESCRIBE or TABLE, and has
// no word that writes. It's a hint, not a guard: a read-only source is
// what stops writes.
func ReadsOnly(statement string) bool {
	s := sqlComment.ReplaceAllString(statement, " ")
	words := strings.Fields(strings.ToLower(strings.TrimLeft(strings.TrimSpace(s), "(")))
	return len(words) > 0 && readVerbs[strings.TrimRight(words[0], ";")] && !writeWords.MatchString(s)
}

// ReadOnlySources are cfg's sources that can't write, by name: SQLite,
// Postgres and AlloyDB with readOnly (the database refuses a write), and
// BigQuery with writeMode blocked (or readOnly).
func ReadOnlySources(cfg Config) []string {
	var out []string
	for name, sc := range cfg.Sources {
		if sourceReadOnly(sc) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// ReadOnlyTools are cfg's tools on a read-only source (ReadOnlySources),
// by name: whatever they say they do, they can't change the database.
func ReadOnlyTools(cfg Config) []string {
	ro := map[string]bool{}
	for _, s := range ReadOnlySources(cfg) {
		ro[s] = true
	}
	var out []string
	for name, tc := range cfg.Tools {
		if src := field(tc, "Source"); src.Kind() == reflect.String && ro[src.String()] {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// sourceReadOnly says whether a source's settings make it read-only.
func sourceReadOnly(sc any) bool {
	if wm := field(sc, "WriteMode"); wm.Kind() == reflect.String { // BigQuery
		switch wm.String() {
		case "blocked":
			return true
		case "":
			ro := field(sc, "ReadOnly")
			return ro.Kind() == reflect.Pointer && !ro.IsNil() && ro.Elem().Bool()
		}
		return false
	}
	ro := field(sc, "ReadOnly")
	return ro.Kind() == reflect.Bool && ro.Bool()
}

// field is a config struct's field by name; the zero Value when it has
// none.
func field(v any, name string) reflect.Value {
	rv := reflect.Indirect(reflect.ValueOf(v))
	if rv.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return rv.FieldByName(name)
}

// Files are dir's YAML files (*.yaml, *.yml), by name; none when dir isn't
// there.
func Files(dir string) []string {
	var out []string
	for _, pattern := range []string{"*.yaml", "*.yml"} {
		m, _ := filepath.Glob(filepath.Join(dir, pattern))
		out = append(out, m...)
	}
	slices.Sort(out)
	return out
}

// Session is a running Toolbox server's MCP connection: what the client
// writes goes to the server, and what the server answers is read. Close
// stops the server and closes its database connections.
type Session struct {
	r     *io.PipeReader // the server's answers
	w     *io.PipeWriter // the client's requests
	stop  context.CancelFunc
	srv   *server.Server
	done  chan struct{}
	err   error
	close sync.Once
}

// Read reads the server's MCP messages.
func (s *Session) Read(p []byte) (int, error) { return s.r.Read(p) }

// Write sends the server MCP messages.
func (s *Session) Write(p []byte) (int, error) { return s.w.Write(p) }

// Close stops the server: its MCP session ends and its sources'
// connections close.
func (s *Session) Close() error {
	s.close.Do(func() {
		_ = s.w.Close()
		s.stop()
		_ = s.srv.Shutdown(context.Background())
		<-s.done
		s.srv.Close()
		_ = s.r.Close()
	})
	if errors.Is(s.err, context.Canceled) || errors.Is(s.err, io.EOF) {
		return nil
	}
	return s.err
}

// Start initializes cfg's sources (connecting to each: a database that
// can't be reached is the error) and tools, and serves them as MCP over
// the returned Session, until Close or ctx is done. Toolbox's log goes to
// logOut (nil: discarded).
func Start(ctx context.Context, cfg Config, logOut io.Writer) (*Session, error) {
	if logOut == nil {
		logOut = io.Discard
	}
	logger, err := log.NewLogger("standard", "warn", logOut, logOut)
	if err != nil {
		return nil, fmt.Errorf("toolbox's logger: %w", err)
	}
	ctx, stop := context.WithCancel(ctx)
	ctx = util.WithLogger(ctx, logger)
	ctx = util.WithUserAgent(ctx, "blitz (mcp-toolbox "+Version+")")
	inst, err := telemetry.CreateTelemetryInstrumentation(Version)
	if err != nil {
		stop()
		return nil, fmt.Errorf("toolbox's instrumentation: %w", err)
	}
	ctx = util.WithInstrumentation(ctx, inst)
	srv, err := server.NewServer(ctx, server.ServerConfig{
		Version:                 Version,
		SourceConfigs:           cfg.Sources,
		AuthServiceConfigs:      cfg.AuthServices,
		EmbeddingModelConfigs:   cfg.EmbeddingModels,
		ToolConfigs:             cfg.Tools,
		PromptConfigs:           cfg.Prompts,
		ResourceConfigs:         cfg.Resources,
		ResourceTemplateConfigs: cfg.ResourceTemplates,
		GroupConfigs:            cfg.Groups,
		Stdio:                   true,
	})
	if err != nil {
		stop()
		return nil, err
	}
	clientR, clientW := io.Pipe() // client → server
	serverR, serverW := io.Pipe() // server → client
	s := &Session{r: serverR, w: clientW, stop: stop, srv: srv, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer func() {
			if p := recover(); p != nil {
				s.err = fmt.Errorf("toolbox stopped: %v", p)
			}
			_ = serverW.CloseWithError(io.EOF)
		}()
		s.err = srv.ServeStdio(ctx, clientR, serverW)
	}()
	return s, nil
}
