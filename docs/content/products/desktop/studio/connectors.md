---
title: Connectors
weight: 85
---

**Studio › Connectors** connects your agents to your data. Point Blitz at a PostgreSQL, AlloyDB, SQLite or BigQuery database and the workspace's agents can list its tables, run queries and answer questions from it: "which products sold best last month?" with no exports pasted into the chat and no SQL written by hand. Connections are read-only unless you say otherwise, and passwords stay in your keychain.

This page walks through connecting a PostgreSQL database called `shop`, then giving the agents a query of their own.

## Open it

Choose **Studio › Connectors**. Studio's menu shows once **Show advanced** is on, in Settings' title bar. Without it, add connections for every workspace in **Settings › Connectors**, the same way; the composer's database button and `/connect` open it too.

![Studio › Connectors: two of this workspace's databases, and a global one switched off here](../../../../images/desktop/connectors-list.png)

The list shows every database this workspace can reach: its own, saved in `.agents/connectors/databases`, and your global ones from **Settings › Connectors**, which are there in every workspace. Each line says its kind, whose it is (**This workspace** or **Every workspace**), whether it's read-only, and how many tools it gives the agents. The switch turns a connection off in this workspace only, so its agents leave that data alone; the line above the list says how many tools the agents are offered now.

A connection that came with the project, one you didn't save yourself, is marked **Not trusted yet**: its tools ask before each use until you open it and save it.

## 1. Add a connection

1. Press **+** beside **Databases** (**Add a connection to Databases**).
2. Pick the kind from the tiles: **PostgreSQL**.
3. Paste a **Connection URL**, `postgres://shop_reader:secret@db.example.com:5432/shop`, and the fields fill themselves (the URL is then cleared: it holds the password). Or fill in **Host**, **Port**, **Database**, **User** and **Password** yourself.
4. The **Name** follows the database: `shop`. Type another if you like; it's kept lower case, and the tools are named after it.
5. Say **What it holds**: "The shop's orders and customers". The agents read that to know when to use it, so be specific.

![A new PostgreSQL connection: the URL read into host, port, database, user and password, the name shop, Read-only on](../../../../images/desktop/connectors-postgres.png)

| Kind | You fill in | Signs in with |
|---|---|---|
| SQLite | the file (**Choose…** opens in the workspace, and a file inside it is kept as `./data/kitchen.db`, so the connection still works when the project is cloned) | nothing: it's a file |
| PostgreSQL | a connection URL, or host, port, database, user, password | the user and password |
| BigQuery | project, location, which datasets, the most bytes a query may bill | your Google credentials |
| AlloyDB | project, region, cluster, instance, IP type, database | a user and password, or your Google credentials |

For AlloyDB and BigQuery, sign in to Google Cloud once with `gcloud auth application-default login`. The password is kept as a Blitz secret in your keychain, never in the file, so the connection's file is safe to commit for your team.

## 2. Keep it read-only

**Read-only** is on for a new connection, and the database itself enforces it: every PostgreSQL session is read-only, whatever the SQL. Tools that write aren't offered, and the rest run without asking, since they can't change anything. Turn it off only when the agents should change data; then every tool asks first. Safest of all is a database **User** that can only read, with a read-only connection on top.

## 3. Test and save

Press **Test and save**. Blitz connects with the form as it is and saves the connection only if that works. If it can't connect, it says why (a wrong host or password, a firewall, or Google credentials that need signing in again), and **Save anyway** keeps it as it is.

Once it's saved the connection stays open, now with its tools: **Ready-made tools** lists what the agents get (`execute_sql` runs any SQL the agent writes, `list_tables` lists the tables and their columns, and more; switch off the ones you don't want), and **Test the connection** has **Test** and **Read the tables** for later checks.

![Test the connection: connected, with the tools the agents would get](../../../../images/desktop/connectors-tested.png)

## 4. Write a query from a description

A query of your own is better than letting the agent write SQL each time: it's checked once, by you, and the agent calls it as a tool with just its parameters. You don't have to write the SQL either.

1. Under **Your queries**, press **Write a query**.
2. Say what you're looking for: *The 10 customers who spent the most last month*.
3. Press **Write it**. Blitz reads the database's tables and columns, and writes the query: a tool name, what it's for, the SQL, its parameters, and the assumptions it made, such as which column means "spent". The table and column names go to your model; no rows do.

![Write a query: the description and the SQL Blitz wrote, with its assumptions](../../../../images/desktop/connectors-builder-done.png)

4. Read it, then press **Use** to add it to **Your queries**, or **Back** to ask again.

**Add a query** writes one by hand instead: a **Tool name**, **What it's for**, the **SQL** with `$1`, `$2` (or `@name` for BigQuery), and a row per parameter.

## 5. Try it, then save again

Fill in a value for each parameter (`10` for `limit`) and press **Try it**: the query runs on the spot and shows what it returns.

![The query in Your queries, tried with limit 10](../../../../images/desktop/connectors-try.png)

Press **Save**. The connection is saved as `.agents/connectors/databases/shop.yaml` and the agents can use it from your next message.

## 6. Ask your agent

In the chat, ask a question the data can answer:

```text
Who were our best customers last month, and what did each spend?
```

The agent picks the tool whose description fits, here `db__shop_top_customers_last_month` with `limit: 10`, and answers from the result. On a read-only connection it doesn't need to ask you first.

## MCP servers for this workspace

Below the databases, **MCP servers** lists every server this workspace's agents use: your global ones (**Every workspace**), the workspace's own (**This workspace**) and the project's. Press **+** to add one just for this workspace: a command to run or a web address, as in Settings. It's kept in your settings folder, not the project. Switch a global one off to keep it out of this workspace alone, or **Make global** to share one of the workspace's with every workspace. [MCP servers](../../../guide/mcp.md#one-workspaces-servers)

## For every workspace

To connect a database once for all your workspaces, add it in **Settings › Connectors** instead, the same way. It's saved in `~/.blitz/connectors/databases`, and each workspace can still switch it off here. Studio never changes a global connection.

More: [Databases](../../../guide/databases.md) (every kind's settings, SQLite with vectors, the files) and [Connectors](../../../guide/connectors.md) (adding your own kinds).
