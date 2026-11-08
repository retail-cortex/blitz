---
name: blitz
description: Use or set up Blitz itself, the app you run in. Connect a database (a connector), change the model or settings, add an MCP server, agent, skill, rule, guardrail, slash command, workflow or job, check why something doesn't work (doctor), read the organization's policy, run evals, review a spec, or open a file, Pictures, Studio or Settings for the user
tags: [blitz, connectors, databases, settings, studio, mcp, workflows]
version: "1.0.0"
author: "Blitz"
---
# Using Blitz

You run inside Blitz. Its own features are set up and changed with the `blitz` tool, which runs the same commands as the `blitz` CLI. Call it as `{"command": "<group> <verb>", "args": {…}}`; `{"command": "help"}` lists every command, and `{"command": "help", "args": {"command": "connector save"}}` gives one command's arguments.

## What's what

- **Connections** (`connector`): databases the agents query (SQLite, PostgreSQL, AlloyDB, BigQuery). Each is a file in `.agents/connectors/<category>/`. Saved, its tools are `db__<name>_…` from the next turn, and `@db:<name>` works in a message.
- **Settings** (`settings`): the model (the conversation's or the workspace's), image and speech models, effort and the permission mode.
- **Doctor** (`doctor`): what works and what doesn't here, with what to do.
- **The organization** (`org`): what its policy allows, and its state.
- **Opening things** (`open file`, `open studio`, `open settings`, `open view`, `picture`): shows the user something in Blitz's window. A web page is `open_page`.
- **Evals, specs, jobs** (`eval`, `spec`, `job`): run an agent's evals, check or verify a spec, schedule a prompt.
- **Memory, chats, processes, approvals, secrets**: what Blitz remembers, past conversations, background processes, saved approvals, and secrets' names.
- **Studio's items** (`workflow`, `agent`, `skill`, `rule`, `guardrail`, `command`): add or change them in this workspace.
- **MCP servers** (`mcp`): servers that give the agents more tools. They run code.

## How to use it

1. Read before you change: `connector list` before `connector save` (it lists each kind's fields), `settings show` before `settings set`, `mcp list` before `mcp add`.
2. Commands that change something ask the user themselves, showing what they'll do. Don't ask first as well, and don't describe the change in place of making it: call the command.
3. Never put a secret (a password, a key, a token) in a command's arguments. The command asks the user for it in a hidden field, and keeps it as a Blitz secret. `secret set` does the same for a secret of its own.
4. Never learn Blitz's formats from its files, its folders or the `blitz` binary, and never run `blitz` in the shell for what the tool does: the tool knows them.
5. The permission mode only goes stricter through the tool. To loosen it, the user uses the status bar or `/mode`.
6. A command your organization's policy forbids says so: tell the user, with what an Admin would change.

## A new SQLite database

1. Create the database file in the workspace (with a script, or `sqlite3` in the shell), and fill it.
2. `connector list`, to see SQLite's fields.
3. `connector save` with `kind: "sqlite"`, a `name`, a `description` of what it holds (the agents read it), and `settings: {"database": "<file>.db"}`, the path relative to the workspace so it works wherever the workspace is cloned.
4. Tell the user it's there: `@db:<name>` in a message adds it to a chat, and Studio › Connectors shows it (`open studio` with `section: "connectors"`).

## Showing the user something

- A file you wrote: `open file` (or the `open_file` tool), never a browser.
- A picture to make: `picture` with the prompt opens Pictures, where the user sees the cost and makes it.
- Where something is set up: `open studio` or `open settings` at the section.
