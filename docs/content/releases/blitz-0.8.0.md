---
title: Blitz 0.8.0
weight: 7
---

**Your agents can now work with your data.** Blitz 0.8.0 connects your agents to the databases you already use. Point Blitz at a PostgreSQL, AlloyDB, SQLite or BigQuery database and your agents can explore its tables, run queries and answer questions from your real data. Read-only by default and with passwords kept in your keychain, so you stay in control of what they can see and change.

This release also makes the desktop app feel faster and steadier with many workspaces open, lets you test your skills before you rely on them, and puts everything that's running one click away in your menu bar.

### Get started in three steps

1. Update Blitz (the app updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.8.0).
2. Open **Studio › Connectors** and press **+** next to **Databases**.
3. Pick your database, fill in a few details and press **Test**. Then ask your agent a question about your data.

Full guide: [connect a database](../guide/databases.md). More on [connectors](../guide/connectors.md).

### What's new

**Ask your data anything.** Once a database is connected, your agents can list its tables, read them and answer questions like "which products sold best last month?" No copying exports into the chat, no hand-written SQL.

**Safe by default.** Every new connection is read-only, and the database itself enforces it, so an agent can explore freely and never change a row. Passwords go into your system keychain, never into a file, so a project's connections can be shared with your whole team.

**Write a query by describing it.** Say what you're looking for, like "the 10 customers who spent the most last month", and Blitz reads your tables and writes the query for you. Keep the ones you trust and they become tools your agents can reuse, with a **Try it** button right next to each.

**You decide what each project can reach.** Keep a database to one project, or share it with all of them from **Settings › Connectors**. Switch any connection off in a project with one click, in Studio or straight from the menu bar, and that project's agents leave that data alone.

**Built to grow.** Databases are the first of Blitz's **Connectors**, with more kinds of systems to come. Adding a new kind of database is a short settings file, not new code.

**Everything running, one click away.** The Blitz icon in your menu bar now pulses while a job or workflow runs, lists what's running with **Open** and **Stop**, and gives every workspace its own menu of jobs, workflows and connections.

**Skills you can test.** Give a skill a few example requests, try it with a prompt and see straight away whether your agent picked it up. Keep the good tries as tests and run them all in the background, or in CI.

**Evals that keep going.** Start an eval, close Studio and get on with your day. It keeps running, shows its progress, and moves past a case that gets stuck.

**No more frozen window.** With several workspaces open and a conversation running, the app could stop responding as if Blitz had crashed. That's fixed: open as many workspaces, chats and canvases as you like. The connection between the window and Blitz is now private and encrypted, even on your own machine.

**Studio, one menu away.** Jump to Agents, Evals, Skills, Connectors or any other part of Studio straight from the menu bar.

### Good to know

- **Workers are now called jobs**, in the app, the menu bar and the command line (`blitz jobs`; `blitz workers` still works). Your existing jobs carry on unchanged.
- **On Windows, install the app** with `Blitz_0.8.0_windows_x64_setup.exe`. The old Edge-based window is gone, and **Open Blitz** in the tray now opens the app.
- **BigQuery and AlloyDB** sign in with your Google Cloud credentials (`gcloud auth application-default login`). They're newer in Blitz than PostgreSQL and SQLite and have seen less real-world use, so we'd love to hear how they work for you.
- **SQLite answers up to 1,000 rows per query.** Add `WHERE` or `LIMIT` to narrow larger results.
- **Includes the latest security fix** for how Blitz checks certificates on secure connections (Go 1.27.3).
- Your settings, workspaces and conversations carry over from 0.7.1.
