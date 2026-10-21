---
name: sql-query
description: Write one correct, safe SQL query for a database connector from its schema and what the user is looking for, with parameters for the values that change
tags: [sql, databases, connectors, query]
version: "1.0.0"
author: "Blitz"
---
# Writing a SQL query from a schema

Use this when someone describes data they want from a database the agents are connected to (a Blitz connector under Connectors › Databases), and the query should become a tool of its own, or be run once.

## Before you write

1. **Read the schema first.** Use only the tables and columns it lists, spelled exactly as listed, with the schema or dataset prefix it gives (`public.orders`, `sales.orders`). Never guess at a column: if what the user wants isn't in the schema, say so.
2. **Find the joins.** Join on columns that name each other (`orders.customer_id` and `customers.customer_id`, or `customers.id`). Don't join on a guess.
3. **Pick the dialect's functions.** PostgreSQL and AlloyDB: `date_trunc`, `now()`, `ILIKE`, `::date`. SQLite: `date()`, `strftime`, `LIKE` (no `ILIKE`); a `vec0` table is queried with `WHERE embedding MATCH ? AND k = 5`. BigQuery (GoogleSQL): `DATE_TRUNC(d, MONTH)`, `CURRENT_DATE()`, `SAFE_DIVIDE`, and tables named `dataset.table`.

## Writing it

- **Read, don't write.** A SELECT (or WITH … SELECT). Never INSERT, UPDATE, DELETE, DDL or anything that changes data, unless the user asks for exactly that.
- **Parameters for what changes.** Anything the user might vary between runs (a customer, a date, a status, how many) is a parameter, not a literal:
  - PostgreSQL, AlloyDB and SQLite take them by position: `$1`, `$2`… in the order they're listed.
  - BigQuery takes them by name: `@day`, `@customer`.
  - Each has a name (lower case, digits and _), a type (`string`, `integer`, `float` or `boolean`) and a description the agents read. Dates are strings in ISO form (`2026-10-20`); cast them in the SQL when needed.
- **Bound it.** Add `ORDER BY` for a stable answer and a `LIMIT` (a `limit` parameter when the user says "top N").
- **Name what comes back.** Alias computed columns (`AS revenue`), and select the columns the answer needs, not `*`.
- **Cheap on BigQuery.** Select only the columns needed and filter on the partition column when the schema shows one: BigQuery bills by bytes read.

## Handing it back

- A **name** for the tool: lower case, digits and _, a verb or noun phrase (`top_customers`, `orders_by_day`).
- A **description** saying what it returns and when to use it, for the agents choosing a tool.
- The **SQL**, and its **parameters**.
- **Notes**: the assumptions you made (which column means "revenue", which join), in a line each.

When you can't write it from the schema — a table or column it would need isn't there — say what's missing instead of inventing it.
