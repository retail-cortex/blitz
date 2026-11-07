---
title: "059 · Data in workflows and chats"
weight: 59
---

*The workspace's databases (spec_046, spec_047) become something work can start from. In a workflow, the **Data** step runs a query on a connection, a saved one, or SQL written or drafted from what you're looking for, and hands the next step a table: its columns described, its rows. Large results can go on as chunks or row by row, so the next step starts before the query ends. In a chat, the database icon picks a connection, or a connection and a query, and puts it into the conversation's context.*

| | |
|---|---|
| Status | **Draft** (2026-11-07, the owner's request; no code yet). Decisions marked **Owner** are open (§10). |
| Source (planned) | the table format and its tests in pkg/engine/datatable; the Toolbox patch in `third_party/mcp-toolbox` (DB-02's list); the Data step in `pkg/engine/flows` and the workflow runner; the chat's data context in the composer and `pkg/engine` attachments; `WorkspaceService` calls in `proto/blitz/v1/workspace.proto` |
| Depends on | [spec_043_workflows](spec_043_workflows.md) (steps, WF-13 data, WF-15 `for_each`), [spec_046_databases](spec_046_databases.md) (DB-07 read-only, DB-12 queries), [spec_047_connectors](spec_047_connectors.md) (CON-12 the query builder, CON-17 a connection off in a workspace), [spec_011_images](spec_011_images.md) (what a turn can carry), [spec_039_secrets](spec_039_secrets.md) |

## 1. Purpose

A connection lets the agents query a database when they decide to. Work often starts from the data instead: "every new order since yesterday, summarised", "these 40 customers, each written to", "what's in `orders`, explained". Today that means asking an agent to fetch it first, in words, every time. This spec makes the data a first-class input: a workflow step that fetches it the same way every run and hands it on in one shape every step can read, and a chat that can be given a database, or a query's result, as context directly.

## 2. The table: one shape for data

- **DAT-01** A query's result is a **table**:

  ```json
  {
    "columns": [
      {"name": "id", "description": "The order's number", "type": "integer", "length": null},
      {"name": "customer", "description": "Who placed it", "type": "string", "length": 80}
    ],
    "rows": [[1042, "Ada"], [1043, "Grace"]],
    "total": 2,
    "truncated": false
  }
  ```

  `rows` are arrays in the columns' order (compact, and the order a query asked for is kept; JSON objects would lose it). `total` counts the rows the query returned; `truncated` says the limit (DAT-22) cut them.
- **DAT-02** A column's **type** is portable, the same across databases: `string`, `integer`, `number` (decimals and floats), `boolean`, `date`, `time`, `timestamp`, `json`, `bytes`, or `other`; `db_type` keeps the database's own (`varchar`, `int4`, `NUMERIC(10,2)`) for whoever needs it. **Length** is the declared length where the type has one (`varchar(80)`: 80), else `null`. **Description** is the column's comment from the schema (PostgreSQL's and BigQuery's column descriptions; spec_047 CON-05 reads them) when the result's column comes from a table the schema knows, else `""`. `nullable` says whether it may be empty, when the database says. **Owner:** whether `db_type` and `nullable` stay (proposed: yes, optional; the four you named always there).
- **DAT-03** Values are JSON's: numbers as numbers, except integers beyond 2^53 and decimals with more digits than a double keeps, which are strings (so nothing is silently rounded); dates and times as ISO 8601 strings; `bytes` as base64; `json` as JSON. `null` is SQL's NULL.
- **DAT-04** The **row item**, for results that go on row by row (DAT-31), carries its columns with it, so each item stands alone:

  ```json
  {"index": 0, "row": {"id": 1042, "customer": "Ada"},
   "columns": [{"name": "id", "type": "integer", "description": "The order's number", "length": null}, …]}
  ```

  `row` is an object here (one row, its names beside its values, which is what a step reading one item wants); `index` is its place in the result. A **chunk** (DAT-31) is a table of some rows, with `index` (its first row's place) and `last` (whether more come).
- **DAT-05** The text a step hands on with a table (WF-13: every result has text and data) is the table as Markdown, its first 50 rows, then a line saying how many more there are, so an agent step reading the text sees the data, and the step reading `data` gets all of it.

## 3. Reading a query's columns

- **DAT-10** Toolbox's query tools answer with rows as JSON objects, without types and in the order Go's maps give. Blitz's copy gets a patch (the next in DB-02's list): the execute and custom-query tools return a table (DAT-01) when asked for one (a `blitz_table` argument Blitz alone sends), from the drivers' own column metadata: PostgreSQL and AlloyDB (pgx's field descriptions: the type's OID, its modifier for the length, the source table's OID for the description), SQLite (each column's declared type and its table and column of origin), BigQuery (the result's schema, its fields' descriptions and modes). The agents' tools keep answering as they do (DB-09).
- **DAT-11** A result's descriptions are matched to the schema (CON-05) by the source table and column when the driver says which they are, else by the column's name when exactly one table of the query has it, else left empty.

## 4. The Data step in a workflow

- **DAT-20** **Data** on the canvas's palette (`flows.Data`, no longer "coming soon") is a step with:
  - **Connection**: one of the workspace's connections that's on in it (CON-17); a step naming one that's off, gone or broken has that as its problem (the workflow's check, WF-02).
  - **What to run**, one of:
    - **A saved query**: one of the connection's queries (DB-12), its parameters filled from templates (`{{ input }}`, `{{ steps.pick.data.customer }}`).
    - **SQL**: a statement written in the step, its parameters (`$1`, `?`, `@name`, as the kind takes them) filled from templates. Templates go only into parameters, never into the SQL's text, so a previous step's answer can't change the query.
    - **Describe it**: what you're looking for, in words; **Draft** has the query builder (CON-12) write the SQL from the connection's schema, shown for review and kept in the step as SQL. The step runs the reviewed SQL every time, not a new draft. **Owner:** whether to also offer **Draft each run** (the SQL written from the description at every run, from `{{ input }}` too; flexible, but a different query each time) (proposed: not in the first version).
  - **Output**: **Table** (the whole result, default), **Chunks** of N rows (default 100), or **Rows** one at a time (DAT-31).
  - **Limit**: the most rows (default 1,000; at most 100,000), and the most bytes (default 8 MB).
- **DAT-21** A step's SQL is checked as it's saved and before it runs: on a read-only connection (DB-07), a statement that writes is refused (the database refuses it too); on one that isn't, a statement that writes runs only when the step says **Allow writes**, and the run asks the person first, as a tool's write would, unless the job's permissions allow it (spec_023_workers WK-04, WK-05a). `ReadsOnly` (DB-19) decides what writes.
- **DAT-22** A result past the limit is cut there: `truncated` is set, the step's text says so, and the run's record shows it (not an error; **Owner:** or an error, proposed: not).
- **DAT-23** **Try it** in the step's form runs it as set up, on the templates' sample values (the previous step's last output when the workflow has run, else ones typed), and shows the table: columns with their types and descriptions, the first rows, how many. Nothing after it runs.
- **DAT-24** Queries that cost money say so: a BigQuery query is dry-run first (bytes it will read, priced at the connection's rate, else BigQuery's on-demand $6.25 a TiB), and a run over the workspace's limit, or over the step's **Most it may cost** (unset: $1), stops with that as its problem. Studio's Try it shows the estimate before it runs.
- **DAT-25** The step's secrets are the connection's (DB-11): never in the workflow file, never in what's handed on; error messages are masked (DB-05).

## 5. Handing on: whole, in chunks, or row by row

- **DAT-30** **Table**: the next steps get one result: its data is the table (DAT-01), its text the Markdown (DAT-05).
- **DAT-31** **Chunks** and **Rows**: the step's result is a **stream** of items (chunks, DAT-04, or row items). Each step after it runs once per item, as for `for_each` (WF-15): Concurrency at a time (4 unless set), `item` and `index` in its templates, its own results kept per item. Unlike `for_each`, it **starts as the items come**: the first rows reach the next step while the query is still reading, and the query reads ahead at most Concurrency × 2 items (backpressure, so a slow step doesn't hold a whole table in memory). An **Accumulate** step after them joins what they made, in order.
- **DAT-32** **Streaming is optional**: a workflow whose runner can't stream (a step type that needs the whole input, such as Summarize or Extract, directly after) takes the stream as a whole table, saying so on the canvas. Streaming needs the driver to read rows as they come: PostgreSQL, AlloyDB and SQLite do; BigQuery pages its results, so its rows come a page at a time.
- **DAT-33** A stream's failure part-way (the database drops, a row can't be read) fails the step with what came before kept: the run's record shows the items done, and a retry (WF-15's `retry`) runs the query again from the start.
- **DAT-34** **Owner:** the default output (proposed: **Table** up to 1,000 rows; **Rows** chosen by the person, since it multiplies the steps after it by the rows).

## 6. Data in a chat

- **DAT-40** The chat box's database icon (today: Settings › Connectors) opens a menu of the workspace's connections that are on, each with its kind and its saved queries, then **Write SQL…**, **Describe what you need…**, and at the end **Connect a database…** (Settings › Connectors, as now). `/data` opens the same menu, and `@db:shop` (a connection) and `@db:shop/top_customers` (one of its queries) typed in the box do what choosing them does.
- **DAT-41** Choosing a **connection** adds it as a chip (its icon and name) to the next message: the turn's context gets the connection's tables and columns (its schema, as CON-05 reads it, at most 200 columns, described), and a line saying its tools are the agent's to query (`db__shop_execute_sql`…). Nothing is queried yet; the agent queries it as it needs to, as tools on a read-only connection run without asking (DB-07).
- **DAT-42** Choosing a **query** (a saved one with its parameters asked for, SQL written, or SQL drafted from a description and shown for review) runs it at once, read-only only, and adds its result as a chip ("shop · top_customers · 40 rows"): the turn's context gets the SQL and the table (DAT-01) as the model reads it best, the columns described, then the rows as Markdown up to 200 rows or 32,000 tokens, the rest said to be cut (and the agent can query for more). A click on the chip shows the table (the CSV viewer's grid, spec_029).
- **DAT-43** A result added to a chat is kept with the turn (so the conversation can be reopened and re-read), not re-run; a chip says when it was read.
- **DAT-44** In an untrusted workspace (CON-09), a workspace connection that isn't trusted isn't offered.
- **DAT-45** **CLI**: `@db:shop` and `@db:shop/top_customers` in a prompt (the REPL completes them), and `/data` lists the connections and queries; `blitz data query <connection> "<sql>" [--json|--csv]` prints a table (DAT-01) for scripts.

## 7. API

- **DAT-50** `WorkspaceService.RunConnectorQuery` (connection, a saved query's name or SQL, parameters, limit, and stream): the table, or with stream the row items or chunks as a server stream; the cost estimate first for a kind that bills (`estimate`). `TryConnectorTool`'s text answer stays for the agents' tools.

## 8. Tests

The table from each driver (types, lengths, descriptions, big integers and decimals as strings, NULLs, dates) against a real SQLite and PostgreSQL in tests, BigQuery's against its schema fixtures; the Toolbox patch's output beside its unpatched one; the Data step's forms (saved query, SQL with template parameters, drafted), its check (read-only, writes asked for, a connection off), its limits and truncation; streaming: items before the query ends, backpressure, Accumulate's order, a failure part-way; the chat's chips: a connection's schema, a query's result, the caps, `@db:` completion; the costs (a dry-run's estimate stops a run over its limit).

## 9. Phases

1. The table (DAT-01–DAT-05), the Toolbox patch (DAT-10, DAT-11) and `RunConnectorQuery` (DAT-50, without stream).
2. The Data step with **Table** output, Try it, limits and costs (DAT-20–DAT-25, DAT-30).
3. The chat: the menu, connection and query chips, `@db:` (DAT-40–DAT-44).
4. Streaming: **Chunks** and **Rows** (DAT-31–DAT-33), the API's stream.
5. The CLI (DAT-45).

## 10. Open decisions (Owner)

1. `db_type` and `nullable` beside your four column fields (proposed: yes, optional).
2. Rows as arrays in the table and as objects in a row item (proposed, DAT-01, DAT-04), or objects everywhere.
3. **Draft each run** from a description (proposed: not at first; a reviewed SQL runs every time).
4. A result past its limit: cut and said (proposed), or an error.
5. The default output: **Table** up to 1,000 rows (proposed); **Rows** chosen.
6. The default most a BigQuery step may cost: $1 (proposed).
