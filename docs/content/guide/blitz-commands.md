---
title: Asking Blitz to set itself up
weight: 47
---

Ask the agent to change Blitz itself, in words: "create a SQLite database of my recipes and add a connector for it", "switch this chat to Claude", "why isn't Ollama working?", "add the GitHub MCP server", "open the notes you just wrote". The agent runs Blitz's own commands with its `blitz` tool, the same commands as `blitz do` in the terminal, and anything that changes something is put to you first.

## What it can do

| Group | Commands |
|---|---|
| `connector` | `list`, `save` (a database connection, its file shown before it's written), `enable`, `disable` |
| `settings` | `show`, `set` (the model, image or speech model, effort, or the permission mode, only ever stricter) |
| `doctor` | what works here and what doesn't: the model, the providers, Ollama, the mode, the sandbox, MCP servers, connections, language servers |
| `org` | `policy` (what your organization allows and locks), `status` |
| `open` | `file` (in the editor), `studio`, `settings`, `view` (changes, processes, Pictures); a web page is the agent's `open_page` |
| `picture` | opens Pictures with the prompt, where you see the cost and make it |
| `eval`, `spec`, `job` | run an agent's evals (asked first: they cost), check, verify and plan a spec, list and create jobs |
| `memory`, `chat`, `process`, `approval`, `secret` | what Blitz remembers, new and past chats, background processes, saved approvals, secrets' names |
| `workflow`, `agent`, `skill`, `rule`, `guardrail`, `command` | list them, and add or change this workspace's |
| `mcp`, `hook`, `plugin` | list them; add an MCP server, or turn one on or off |

`blitz do help` lists them all, and `blitz do help connector save` gives one command's arguments.

## What it asks you

- **Changes are approved first**, in the chat's approval card: what will change, and the file it writes. In auto mode the reviewer decides, as for any change, and you can overrule it.
- **Secrets are yours to type.** A connection's password, or `secret set`, asks you in a hidden field after you approve. The value goes straight to Blitz's secret store; the agent and the conversation never see it.
- **What runs code is always asked:** adding an MCP server or turning one on shows exactly what it runs, even in auto mode or where a rule would allow it.
- **The permission mode only goes stricter** through the agent. Loosening it is yours: the status bar's mode, or `/mode`.
- **Your organization's policy applies.** Where it doesn't allow your own connections or MCP servers, the command says so.
- **A connection approved in the card is trusted**, as **Save and trust** in Studio would, and turned on in this workspace.

Plan mode runs only the commands that read or open something. A job runs a command only when its permissions name it: `blitz:connector list`, or `blitz:connector *` for the group.

## In the terminal

`blitz do <command> [name=value…]` runs the same commands:

```bash
blitz do doctor
blitz do connector list --json
blitz do connector save kind=sqlite name=recipes description="Recipes and ingredients" settings='{"database":"recipes.db"}'
blitz do settings set key=model value=gemini/gemini-4-carbon
```

A value is read as JSON when it is (`true`, `3`, `{"a":1}`), else as text, and `--args` takes them all as one JSON object. A command that changes something shows what it'll do and asks `[y/N]` (`--yes` doesn't), then asks for any secret it needs, hidden. `--json` prints the result for scripts.
