---
title: Databases
weight: 46
---

Connect a database and your agents can answer questions from your data: how many orders failed last night, which tables a migration touches, what a customer bought. Blitz runs [MCP Toolbox for Databases](https://github.com/googleapis/mcp-toolbox) inside itself, so there's nothing else to install, and every database starts **read-only**: the database itself refuses any change, whatever the agent asks.

Databases are the first of Blitz's [connectors](connectors.md). Spec: [databases](../about/specs/spec_databases_046.md).

## Connect one

In the desktop app, open **Studio › Connectors** for one this workspace has (saved in `.agents/connectors/databases`, so you can commit it for your team), or **Settings › Connectors** for one every workspace has (`~/.blitz/connectors/databases`), and press **+** by **Databases**. Any workspace can switch a global one off ([what each workspace can reach](connectors.md#choose-what-each-workspace-can-reach)).

1. Give it a **name** (`shop`) and say **what it holds** ("The shop's orders and customers"). The agents read that to know when to use it.
2. Choose the **kind** and fill in its settings:

   | Kind | Settings | Signs in with |
   |---|---|---|
   | PostgreSQL | host, port, database, user, password | the user and password |
   | AlloyDB | project, region, cluster, instance, IP type, database | a user and password, or your Google credentials |
   | SQLite | the file's path | nothing: it's a file |
   | BigQuery | project, location, which datasets, the most bytes a query may bill | your Google credentials |

   For Google Cloud, sign in once with `gcloud auth application-default login`.
3. Leave **Read-only** on unless the agents should change data.
4. Press **Test** to connect and see the tools the agents will get, then **Save**.

The password goes into your system's keychain as a [secret](../about/specs/spec_secrets_039.md) (`DB_SHOP_PASSWORD`), never into the file, so a workspace's database file is safe to commit.

## What the agents get

Each database offers **ready-made tools**, named `db__<database>_<tool>`: `db__shop_list_tables`, `db__shop_execute_sql`, and for BigQuery `list_datasets` and `table_info`. Switch off the ones you don't want.

**Your queries** are better still: SQL you write once, with parameters, offered as a tool of its own. An agent asking for "the top 5 customers" calls `db__shop_top_customers` with `limit: 5` instead of writing SQL. Write the SQL, and Blitz drafts its parameters from `$1`, `$2` (PostgreSQL, SQLite) or `@name` (BigQuery); **Try it** runs it on the spot.

### Write a query from a description

Don't want to write the SQL? Press **Write a query** and say what you're looking for: *the 10 customers who spent the most last month*. Blitz reads the database's tables and columns (with their comments), and the built-in **sql-query** skill writes the query: a tool name, what it's for, the SQL in the database's dialect, and its parameters, with the assumptions it made (which column means "spent"). **Use** adds it to Your queries; try it, then save.

- It needs to read the tables: if it can't connect, or the connection can see no table, it says so and writes nothing.
- The table and column names go to your model; no rows do.
- Write a skill of your own named `sql-query` in the workspace to change how queries are written (your naming rules, a house style).

## Read-only, and what asks first

| | Read-only | Can write |
|---|---|---|
| PostgreSQL, AlloyDB | every session is read-only, and `execute_sql` runs in a read-only transaction: no SQL gets a write through | writes work |
| SQLite | the file is opened read-only | writes work |
| BigQuery | writes are blocked (`writeMode: blocked`) | writes work |
| Tools that write | not offered | offered |
| Asking first | tools run without asking: they can't change anything | every tool asks first, like any MCP tool |

Safest of all is a database user that can only read, and a read-only connection on top.

## SQLite with vectors

Every SQLite database has [sqlite-vec](https://github.com/asg017/sqlite-vec) built in, so an agent can keep embeddings in a `vec0` table and find the nearest ones:

```sql
CREATE VIRTUAL TABLE notes_vec USING vec0(embedding float[768]);
SELECT rowid, distance FROM notes_vec WHERE embedding MATCH ? AND k = 5 ORDER BY distance;
```

(Writing to it needs a connection that can write.)

## The files

A connection is a [Toolbox YAML](https://googleapis.github.io/genai-toolbox/resources/) file. Studio writes it, and you can write or edit one yourself. Blitz picks up a change within a couple of seconds:

```yaml
# kind: postgres
# about: The shop's orders and customers
kind: source
name: shop
type: postgres
host: db.internal
port: "5432"
database: shop
user: reader
password: ${DB_SHOP_PASSWORD}
readOnly: true
---
kind: tool
name: shop_top_customers
type: postgres-sql
source: shop
description: The customers who spent the most
statement: |
  SELECT name, sum(total) AS spent FROM orders JOIN customers USING (customer_id)
  GROUP BY name ORDER BY spent DESC LIMIT $1
parameters:
  - name: limit
    type: integer
    description: How many
```

`${NAME}` is the Blitz secret `NAME` (this workspace's, else the global one), never an environment variable. A file that came with the workspace (one you didn't save in Studio) gets only this workspace's secrets, and its tools ask first, until you open it and press **Save and trust**. A file with more than Studio's form describes, such as several sources or other Toolbox tools, still works: Studio lists it as **Custom YAML** and lets you edit the text.

All the files together are one MCP server, **databases**, which `/mcp` lists with its state. It connects the first time an agent needs it; if a database can't be reached, `/mcp` and Studio say why.
