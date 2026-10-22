---
title: Connectors
weight: 85
---

**Studio › Connectors** connects your agents to your data. Point Blitz at a PostgreSQL, AlloyDB, SQLite or BigQuery database and the workspace's agents can list its tables, run queries and answer questions from it: "which products sold best last month?" with no exports pasted into the chat and no SQL written by hand. Connections are read-only unless you say otherwise, and passwords stay in your keychain.

This page walks through connecting a PostgreSQL database called `shop`, then giving the agents a query of their own.

## Open it

Choose **Studio › Connectors**. Studio's menu shows once **Show advanced** is on, in Settings' title bar.

![Studio › Connectors: two of this workspace's databases, and a global one switched off here](../../../../images/desktop/connectors-list.png)

The list shows every database this workspace can reach: its own, saved in `.agents/connectors/databases`, and your global ones from **Settings › Connectors**, which are there in every workspace. Each line says its kind, whose it is (**This workspace** or **Every workspace**), whether it's read-only, and how many tools it gives the agents. The switch turns a connection off in this workspace only, so its agents leave that data alone; the line above the list says how many tools the agents are offered now.

A connection that came with the project, one you didn't save yourself, is marked **Not trusted yet**: its tools ask before each use until you open it and save it.

## 1. Add a connection

1. Press **+** beside **Databases** (**Add a connection to Databases**).
2. Give it a **Name**, lower case: `shop`. Its tools are named after it, `db__shop_…`.
3. Say **What it holds**: "The shop's orders and customers". The agents read that to know when to use it, so be specific.
4. Choose the **Kind** and fill in its settings. For PostgreSQL: **Host**, **Port**, **Database**, **User** and **Password**.

![A new PostgreSQL connection: name, what it holds, host, port, database, user and password, Read-only on](../../../../images/desktop/connectors-postgres.png)

| Kind | You fill in | Signs in with |
|---|---|---|
| PostgreSQL | host, port, database, user, password | the user and password |
| AlloyDB | project, region, cluster, instance, IP type, database | a user and password, or your Google credentials |
| SQLite | the file's path (`./data/kitchen.db` starts at the workspace's folder) | nothing: it's a file |
| BigQuery | project, location, which datasets, the most bytes a query may bill | your Google credentials |

For AlloyDB and BigQuery, sign in to Google Cloud once with `gcloud auth application-default login`. The password is kept as a Blitz secret in your keychain, never in the file, so the connection's file is safe to commit for your team.

## 2. Keep it read-only

**Read-only** is on for a new connection, and the database itself enforces it: every PostgreSQL session is read-only, whatever the SQL. Tools that write aren't offered, and the rest run without asking, since they can't change anything. Turn it off only when the agents should change data; then every tool asks first. Safest of all is a database **User** that can only read, with a read-only connection on top.

**Ready-made tools** lists what the agents get: `execute_sql` runs any SQL the agent writes, `list_tables` lists the tables and their columns, and more. Switch off the ones you don't want.

## 3. Test it

Under **Test the connection**, press **Test**. Blitz connects with the form as it is, saved or not, and lists the tools the agents would get. **Read the tables** shows the tables and columns it can see.

![Test the connection: connected, with the tools the agents would get](../../../../images/desktop/connectors-tested.png)

If it can't connect, it says why: a wrong host or password, a firewall, or Google credentials that need signing in again.

## 4. Write a query from a description

A query of your own is better than letting the agent write SQL each time: it's checked once, by you, and the agent calls it as a tool with just its parameters. You don't have to write the SQL either.

1. Under **Your queries**, press **Write a query**.
2. Say what you're looking for: *The 10 customers who spent the most last month*.
3. Press **Write it**. Blitz reads the database's tables and columns, and writes the query: a tool name, what it's for, the SQL, its parameters, and the assumptions it made, such as which column means "spent". The table and column names go to your model; no rows do.

![Write a query: the description and the SQL Blitz wrote, with its assumptions](../../../../images/desktop/connectors-builder-done.png)

4. Read it, then press **Use** to add it to **Your queries**, or **Back** to ask again.

**Add a query** writes one by hand instead: a **Tool name**, **What it's for**, the **SQL** with `$1`, `$2` (or `@name` for BigQuery), and a row per parameter.

## 5. Try it, then save

Fill in a value for each parameter (`10` for `limit`) and press **Try it**: the query runs on the spot and shows what it returns.

![The query in Your queries, tried with limit 10](../../../../images/desktop/connectors-try.png)

Press **Save**. The connection is saved as `.agents/connectors/databases/shop.yaml` and the agents can use it from your next message.

## 6. Ask your agent

In the chat, ask a question the data can answer:

```text
Who were our best customers last month, and what did each spend?
```

The agent picks the tool whose description fits, here `db__shop_top_customers_last_month` with `limit: 10`, and answers from the result. On a read-only connection it doesn't need to ask you first.

## For every workspace

To connect a database once for all your workspaces, add it in **Settings › Connectors** instead, the same way. It's saved in `~/.blitz/connectors/databases`, and each workspace can still switch it off here. Studio never changes a global connection.

More: [Databases](../../../guide/databases.md) (every kind's settings, SQLite with vectors, the files) and [Connectors](../../../guide/connectors.md) (adding your own kinds).
