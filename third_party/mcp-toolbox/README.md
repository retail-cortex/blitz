# MCP Toolbox for Databases, in-process

A pruned copy of [MCP Toolbox for Databases](https://github.com/googleapis/mcp-toolbox)
(Apache-2.0, Google LLC; the version is in `VERSION`), which `blitzd` runs
inside its own process to give agents database tools over MCP
(spec_databases_046). Only Toolbox's core and the databases Blitz offers are
here: Postgres, AlloyDB for PostgreSQL, SQLite (with sqlite-vec) and
BigQuery. Toolbox's tests, command line, documentation and the other
databases are left out.

- `internal/`: Toolbox's code as released, its import path moved under
  Blitz's module, with `patches/` applied. Kept as its authors wrote it:
  nogo's checks don't run on it.
- `embed/`: Blitz's facade. `config.go` is Toolbox's config parser (from
  `cmd/internal`), patched to fill `${NAME}` only from the values given;
  `embed.go` loads a configuration and serves its tools as MCP over an
  in-memory pipe, without Toolbox's signal handling, version check or
  global telemetry.
- `patches/`: Blitz's changes, applied in order:
  1. SQLite through ncruces' driver with sqlite-vec compiled in, and a
     `readOnly` source setting (the file opened read-only);
  2. a `readOnly` Postgres setting (read-only sessions), and read-only,
     rolled-back transactions for read-only Postgres and AlloyDB sources;
  3. the stored-procedures tool no longer names the Cloud SQL source,
     which isn't copied;
  4. the config parser's `${NAME}` from the given values alone, and its doc
     comments;
  5. the MCP stdio session no longer panics when it's closed while
     reading (in-process, a panic would stop `blitzd`);
  6. the server closes its sources (their pools, files and clients) when
     it's closed, and sources can be closed;
  7. a start that fails once a source is open (a later source, a tool, the
     server itself) closes the sources open so far, and BigQuery closes
     its client and caches on its own early errors;
  8. a SQLite query returns at most `MaxRows` (1000) rows, the last one
     saying so, rather than loading a whole table into memory.

To move to another release: `tools/update_toolbox.sh <version>`, then
`bazel run //tools:tidy` and the tests. A patch that no longer applies is
reported, to be redone against the new code.
