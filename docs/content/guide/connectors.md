---
title: Connectors
weight: 45
---

**Connectors** are the systems your agents reach, set up in **Studio › Connectors** (a workspace's) and **Settings › Connectors** (every workspace's) rather than installed: [databases](databases.md) today, more categories to come. Every connection that's on in a workspace has its tools offered to the agents there, named after its category (`db__shop_top_customers`), with the same approvals as any tool.

Spec: [connectors](../about/specs/spec_connectors_047.md).

## Where connections live

| | This workspace | Every workspace |
|---|---|---|
| Set up in | **Studio › Connectors** | **Settings › Connectors** |
| Connections | `.agents/connectors/<category>/<name>.yaml` | `~/.blitz/connectors/<category>/<name>.yaml` |
| Your own kinds | `.agents/connectors/kinds/<id>.yaml` | `~/.blitz/connectors/kinds/<id>.yaml` |

A workspace's Studio only changes that workspace's connections. Your global ones are listed there too, so you can see what's in scope and switch each on or off, but you change them in Settings. To share one of the workspace's with every workspace, open it in Studio and press **Make global**: it moves to `~/.blitz/connectors`, its password with it, and leaves the workspace's `.agents` folder (if that's committed, your teammates lose it with your next commit). A kind you described in the workspace's own `.agents/connectors/kinds` can't go global until you add its descriptor to `~/.blitz/connectors/kinds`.

Passwords and other secret settings go into your system's keychain as [secrets](../about/specs/spec_secrets_039.md), never into the file, so a workspace's connections are safe to commit.

**A connection that came with the workspace**, one you didn't save yourself, is served, but it can use only this workspace's secrets, and every tool asks before it runs. Open it in Studio and press **Save and trust** once you've read it; trusting the project's settings trusts it too. That way a cloned repository can't point one of your passwords at a server of its own.

## Choose what each workspace can reach

Every connection, your global ones too, has a switch in Studio › Connectors. Turn one **off** and the agents in this workspace no longer get its tools, so they can't draw on that data here; other workspaces aren't changed. That's how you keep a project's answers to the sources that belong to it: your sales warehouse on for the analysis workspace, off for the website.

The tray has the same switches: each workspace's submenu ends with **Connectors**, every connection it reaches ticked while it's on there, so you can put a data source in or out of scope without opening the app.

A connection that's off keeps its file and its password, and you can still test it, try its queries and read its tables in Studio. The switch is yours, kept in your settings folder rather than the project, so cloning a repository never turns one of your connections on.

## Add a kind of your own

Each kind is a short YAML **descriptor**: its settings, its ready-made tools, how it's made read-only, and what it can do. A kind that an existing backend can serve needs no code. A Supabase database, for instance, is PostgreSQL with other defaults:

```yaml
# .agents/connectors/kinds/supabase.yaml
id: supabase
category: databases
name: Supabase
hint: A Supabase project's Postgres, through its pooler.
backend: toolbox
source: postgres
capabilities: [test, try, queries, schema, query-builder]
fields:
  - {key: host, required: true, help: "aws-0-us-east-1.pooler.supabase.com"}
  - {key: port, default: "6543"}
  - {key: database, default: postgres}
  - {key: user, required: true}
  - {key: password, type: secret, required: true}
readOnly: {key: readOnly, value: true}
tools:
  - {id: execute_sql, type: postgres-execute-sql, on: true, agent: "Run a SQL statement on {connection}."}
  - {id: list_tables, type: postgres-list-tables, on: true, agent: "List the tables in {connection}."}
queries: {type: postgres-sql, dialect: PostgreSQL, placeholders: "$1"}
schema:
  statement: |
    SELECT table_schema || '.' || table_name AS "table", NULL AS about,
      string_agg(column_name || ' ' || data_type, ', ' ORDER BY ordinal_position) AS columns
    FROM information_schema.columns
    WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
    GROUP BY table_schema, table_name
```

It appears under **Databases** at once. Text can be one string, or a map of languages (`{en: Host, es: Servidor, fr-CA: Hôte}`). The built-in kinds, in [`pkg/engine/connectors/builtin`](https://github.com/retail-cortex/blitz/tree/main/pkg/engine/connectors/builtin), show every field.

A descriptor that can't be used (an unknown backend, a capability the backend lacks, a misspelt field) is listed under **Descriptors that can't be used** with why. Yours can add kinds but never change a built-in one: a kind whose ID is taken is ignored.

## Capabilities

| Capability | What Studio offers |
|---|---|
| `test` | **Test**: connect as the form is and list the tools the agents would get |
| `try` | **Try it** on a query |
| `queries` | **Your queries**: SQL with parameters, each a tool of its own |
| `schema` | **Read the tables**: the tables and their columns |
| `query-builder` | **Write a query**: describe it, and the sql-query skill writes it from the schema (needs `schema` and `queries`) |

A kind lists the ones it has, and its backend must be able to: the **toolbox** backend (MCP Toolbox for Databases, inside Blitz) can do them all.
