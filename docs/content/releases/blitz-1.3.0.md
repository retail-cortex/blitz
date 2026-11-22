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
- **Providers in one place.** `blitz providers add anthropic` asks for the key and checks it works before saving it. `list`, `use`, `check` and `remove` do the rest.
- **The service's log, from the terminal.** `blitz service logs` shows what the service has been doing, filtered by level or words, and `-f` keeps watching. It reads the log files itself, so it works even when the service won't start.
- **Removing asks first.** Removing an MCP server, plugin, skill, note, job or Pictures session asks on a terminal (`-y` skips it). Scripts go ahead as before.
- **More for scripts.** `blitz sessions list`, `blitz runs` for background runs, and `--json` on more lists.

### Fixed

- **"The workspace is open elsewhere" after a restart.** When the service restarted, mostly on Windows, a workspace could be refused while the old service was still closing it. Blitz now waits a moment for a workspace that's on its way out, and still says at once when one really is open somewhere else.

- **Voice picks a model you can use.** Settings › Voice used to list every live model Google lists, including ones your key or project can't reach, and picked the default by its name rather than its version, so a conversation could stop with "failed to receive setup complete". Blitz now checks each model with the settings a conversation uses and offers only the ones that answer, native audio first (it sounds the most natural), or says why when none does. Newer live models also hear you now: your voice reaches them in the form they read.

- **Goodbyes are said whole.** When you end a voice conversation by saying goodbye, Blitz's reply plays to the end instead of being cut off.

### Good to know

- `blitz agents`, `logs`, `stop` and `attach` are now `blitz runs`, `runs logs`, `runs stop` and `runs attach`. The old names work for this release. After it, `blitz agents` lists your agents.
- `-w` on `blitz` itself still means `--worktree` for this release, then `--workspace`, as it already does on every other command. The same goes for `-a`, from `--agent` to `--all`.
- When the agent configures Blitz, it changes only this workspace and never what loosens Blitz's limits: the permission mode (except to make it stricter), where requests go, or your own profile. It tells you the command to run for those.
- `NO_COLOR` is honoured now, and `--color` sets colour either way.
