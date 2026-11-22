---
title: Blitz 1.3.0
weight: 1
---

The command line gets the care the desktop app has had. Blitz now greets you with its banner and finishes your commands as you type, sub-commands and values included. It answers questions about itself from the docs it carries, even offline, and sets itself up when you ask: point it at an MCP server's page and it does the rest, asking you only for the token.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade` or `brew upgrade --cask blitz`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v1.3.0).
2. Run `blitz`. Type `/` and a few letters: the rest of the command shows dimmed, → takes it, Tab finishes it or shows the choices.
3. Ask something: `? how do I add an MCP server that needs a token`.
4. Set one up: `/configure the MCP server at https://github.com/github/github-mcp-server`.

### New

- **A banner that tells you where you are.** The Blitz wordmark, then your workspace and its git branch, the agent and model, the permission mode and the sandbox, all at a glance. A narrow terminal gets one line; `ui.banner = "compact"` or `"off"`, or `-q`, keep it quiet.
- **Completion that knows every command.** Tab finishes commands at every level (`/skills registry add`, `/permissions allow … --save`) and their values: every provider's models, your agents, themes, sessions, `/config mode=`. As you type, the rest of the command shows dimmed, then what it does. A slip is forgiven: Tab turns `/pmod` into `/pin_model`, and `/permisions` asks "Did you mean /permissions?".
- **Shell completion, installed for you.** `blitz completion install` sets it up for zsh, bash or fish. Then `blitz mcp remove <Tab>` offers your servers.
- **Help that answers.** `? <question>` answers from Blitz's own docs, with the command to type and a link to the page. The chat doesn't keep it. Without a model set up, you get the matching sections and commands instead. `/docs` and `blitz docs` read the docs of your release, offline.
- **Configure by asking.** `/configure` (or `blitz configure`) hands a setup to Blitz's agent. It reads the page you name and finds what the setup needs. You see every setting before it's saved, and you type secrets yourself, hidden; they're kept as Blitz secrets. Then it checks the setup works.
- **Every setting from the terminal.** `blitz config get`, `set` and `reference` for any setting, with `-w` for this workspace alone. `blitz config edit` opens the file and checks it when you save.
- **The Reasoning view, in the terminal.** `/reasoning` draws a turn as a tree: each model call with its tokens, time and cost, the steps of reasoning it shared, its tools (✓ or ✗) and sub-agents.
- **More for scripts.** `blitz sessions list`, `blitz runs` for background runs, and `--json` on more lists.

### Good to know

- `blitz agents`, `logs`, `stop` and `attach` are now `blitz runs`, `runs logs`, `runs stop` and `runs attach`. The old names work for this release. After it, `blitz agents` lists your agents.
- `-w` on `blitz` itself still means `--worktree` for this release, then `--workspace`, as it already does on every other command. The same goes for `-a`, from `--agent` to `--all`.
- When the agent configures Blitz, it changes only this workspace and never what loosens Blitz's limits: the permission mode (except to make it stricter), where requests go, or your own profile. It tells you the command to run for those.
- `NO_COLOR` is honoured now, and `--color` sets colour either way.
