---
title: "067 · CLI parity"
weight: 67
---

*The command line as a first-class Blitz: a banner that says whose it is, completion that knows every command, sub-command and value, help that answers questions, configuration by asking the agent, and every setting the desktop app can change.*

| | |
|---|---|
| Status | **Phases 1–4 built**: CLP-01–04 (the banner, colour rules, first-run hints; the update notice waits for spec_054's check to be cached) and CLP-10–14 (the command tree, completion at every level, the hint and fuzzy Tab, shell completion; ranking by use isn't built) and CLP-20–24 (help from the tree, the docs in the binary, ask Blitz from them, the offline answer, errors; `help.model` and `--debug` aren't built) and CLP-30–32, 35 (`/configure` and `blitz configure`, MCP servers from a page with env, headers and secrets, the settings the agent may set; providers, CLP-33, and the deletes, CLP-34, go to phase 5); phase 5 in part: CLP-40 (`config get|set|edit|check|reference`), CLP-47 (`/reasoning`, `blitz sessions reasoning`), CLP-48 (`sessions list`), CLP-34 (`connector remove|test`, `rule|guardrail|secret|workflow|skill remove` through the tool and `blitz do`, rule and guardrail saves diffed against the old file; agent and job removes are for later); About you and `[voice]` are `blitz config set profile.*|voice.*` for now; phase 6 in part: CLP-50, CLP-51 (`blitz runs`; the old names and the root's `-w`/`-a` for a release), CLP-53 in part; the rest planned (2026-11-22, the owner's ask: "ensure [the CLI] has the features required to access and edit the config and features now available in the desktop app … a Blitz banner on startup, powered by Retail Cortex. A crisp autocomplete, that includes sub commands. A better help system that can be agent driven when a provider is present … configure via the agent such as: look at this site X and configure the MCP server here"). Branch `cli-parity`. |
| Source | `apps/cli/` (cobra commands, `helpgroups.go`, `configcmd.go`, `mcp.go`, `do.go`); `apps/cli/internal/tui/` (`repl.go`, `render.go`, `terminal.go`, `commands.go`, `commands_extra.go`, `status.go`, `theme.go`); `pkg/api/blitzcommands.go` (the catalog), `pkg/engine/blitzcommands.go` (its handlers), `pkg/engine/tools/blitz.go`; `pkg/config/mcpedit.go`, `providers.go` (`SetValue`); `pkg/i18n/locales/` |
| Tests | Listed with each phase (§4). Every phase ends with the CLI's tests, `tools/check_format.sh`, and the journey tests of §4.8 |
| Depends on | [spec_019_tui](spec_019_tui.md), [spec_020_cli](spec_020_cli.md), [spec_060_blitz_tools](spec_060_blitz_tools.md) (the catalog, approvals, `blitz do`), [spec_009_mcp](spec_009_mcp.md), [spec_004_i18n](spec_004_i18n.md), [spec_008_web](spec_008_web.md) |

## 1. Purpose

The desktop app has had the attention, and the CLI shows it. Today (from a survey of `c8e251ba`):

- **Start-up** prints one plain line (`Blitz 1.2.0  agent … · model …`) and two hints that both say "Type /help". There is no brand, no colour control (`NO_COLOR` is ignored), and the status shows only after the first prompt.
- **Completion** knows slash-command names from a hand-kept list (`main.go:651`) and one level of arguments. Readline prints the candidates as a bare list on Tab. It doesn't complete `/model` names, themes, styles, personalities, config keys, `/cd` paths or aliases. `blitz completion` is cobra's default and has no dynamic values.
- **Help** comes from three hand-kept places that disagree: `/help`'s table (`commands.go:191`), the completer, and the dispatcher. `/help` leaves out `/resume`, `/skills install`, `/model --refresh` and the aliases. No help links to the docs. Cobra's text isn't localised, and the docs aren't in the binary (only the desktop page bundles them).
- **Configuration through the agent** works only part of the way. `blitz mcp add` through the agent can't set env vars or headers, so an MCP server that needs a token can't be set up. `settings set` takes five keys, and there is no provider set-up, no deletes and no `mcp remove`.
- **Parity.** About 200 of the service's ~420 RPCs have no CLI route. Desktop-only today: personality editing, About you, browser profiles, logs, Reasoning, workspace health, workflow runs, job editing, eval and skill authoring, connector test, schema and delete, secret delete, rule and guardrail delete, language-server status, and the settings file check.
- **Rough edges.**
  - The same word means different things: `blitz agents` lists background runs, while `/agents` lists personas.
  - Short flags clash: `-w` is the worktree in one place and the workspace in another; `-a` and `-f` likewise.
  - Deletes don't ask, and most lists have no `--json`.
  - Errors are raw: `Error: rpc error: …`.
  - Unknown sub-commands of `/skills` and `/session` do nothing.

This spec fixes that in phases small enough to land one at a time, each with its tests.

## 2. Principles

- **One catalog, four readers.** Every command, sub-command, argument and value source is declared once. The dispatcher, `/help`, the completer and shell completion all read that declaration, so they can't drift. A test fails when a handled command isn't declared.
- **Same nouns as the desktop.** A thing has the same name in the desktop, the CLI and the `blitz` tool: agents, skills, rules, guardrails, workflows, jobs, connectors, MCP servers, secrets, personalities and browser profiles. The form is `blitz <noun> <verb>`, plus `/<noun> <verb>` in the REPL.
- **Through the service.** Settings changes go through the service's RPCs (or `RunBlitzCommand`) when attached, so the desktop sees them at once. They fall back in-process when no service is running.
- **Asks like the desktop does.** A change the agent proposes goes through the same approval, with a diff, as the desktop's form (spec_060). Secrets are asked as hidden input and never echoed or logged.
- **Plain terminals stay plain.**
  - `NO_COLOR`, `TERM=dumb`, a non-TTY output and `--json` all mean no colour, no banner art, no spinner and no menus.
  - Every list has `--json`.
  - Exit codes stay those of spec_020.

## 3. Requirements

### 3.1 The banner (phase 1)

- **CLP-01** **The banner.**
  - The interactive CLI opens with a **Blitz wordmark**: block letters about 5 lines tall in the brand's lightning yellow-to-orange gradient, truecolor where the terminal says so (`COLORTERM`), 256 colours otherwise.
  - Beside or under the wordmark: **powered by Retail Cortex**, the version, and the update notice when a newer release is out (from spec_054's check, never a network call of its own).
  - Below it, one **status block**:
    - the workspace (its folder, with a git branch when there is one)
    - agent · model (or "no model yet: run `/setup models`")
    - the permission mode, in red when unsafe
    - the sandbox in one word
    - the organization, when connected
  - One hint line: `/help · Tab completes · ? asks Blitz · Ctrl+C exits`.
- **CLP-02** **Width and quiet.**
  - Under 60 columns the wordmark gives way to a one-line `⚡ Blitz` mark.
  - `ui.banner = "full" | "compact" | "off"` (default `full`) is settable from `/config` and Settings.
  - `--quiet`/`-q` and one-shot runs (`blitz "prompt"`, `exec`) print no banner.
  - A resumed session shows the compact banner, then its recap.
- **CLP-03** **Colour rules** apply to every CLI output, not only the banner:
  - `NO_COLOR` (any value) turns colour off.
  - `CLICOLOR_FORCE` / `FORCE_COLOR` turn it on.
  - `TERM=dumb` or non-TTY output turns it off.
  - `--color=auto|always|never` overrides all three.
  - One helper (`tui.Style`) replaces the raw ANSI constants in `render.go:28-39`, and `warnFn` uses it too.
- **CLP-04** The duplicate hints go (`banner.hint` and `repl.hint` say the same thing). The sandbox, steer and key hints show once per install (counted in the CLI's state file) or with `/help keys`.

### 3.2 Completion (phase 2)

- **CLP-10** **One command tree.** `tui.Commands` declares each slash command once. A declaration holds:
  - its name, aliases, group and one-line help (an i18n key)
  - its sub-commands, recursively
  - its arguments, each with a **value source**: static choices, agents, models, sessions, themes, styles, personalities, locales, config keys, a config key's values, workspace paths, directories, skills, MCP servers, connectors, secrets' names, jobs, tasks or processes
  - its flags

  The dispatcher looks commands up in the tree. `/help`, the completer and the desktop's parity test (`slashParity.test.ts`, now checked both ways) read it. The hand-kept lists at `main.go:648-745` and `commands.go:191-262` go.
- **CLP-11** **Completion at every level:**
  - any depth of sub-command (`/skills registry add`, `/permissions allow`, `/session load <id>`)
  - flags (`--save`, `--force`)
  - `key=value` for `/config` and `/set`: the keys from the settings reference (`GetSettingsReference`), then each key's allowed values (enums, booleans, models, themes)
  - aliases
  - custom commands as they are now, not as they were at start-up
  - `@` files and `@db:` connections, as today
- **CLP-12** **A completion menu and a hint.**
  - Tab on one candidate completes in place. On several, it completes as far as they agree, then opens readline's menu under the line: Tab and the arrows move, Enter takes.
  - As a slash command is typed, a dim **hint** after the cursor shows the rest of the word the candidates agree on (→ takes it). Once the command's name is typed, the hint is its usage and what it does: `/permissions  [allow|ask|deny|remove <rule> [--save]] — Rules for what runs…`.
  - No hint without colour (CLP-03), while Ctrl+R searches, or when it wouldn't fit on the line.
  - The hint uses only values already known (a source's answer is kept 10 s), so typing never waits on the service.
  - ergochat/readline's menu has no room for descriptions, so they're in the hint and in `/help <command>` instead.
- **CLP-13** **Fuzzy, not only prefix.** A candidate matches by prefix first, then by words (`/perm a` → `/permissions allow`), then by subsequence. (Ranking ties by use in this install is for later.) Tab takes a fuzzy match only when it is the one candidate and none starts with what's typed.
- **CLP-14** **Shell completion from the same tree.**
  - The cobra commands get `ValidArgsFunction` and flag completion from one table: agents, models, MCP servers, connectors, jobs, specs, skills, plugins, sessions, workspaces and config keys. Values come from the settings files and the catalog, never the network or the service (agents, models, jobs, specs and sessions are for later).
  - `blitz completion install` writes the script for the user's shell (zsh, bash or fish; PowerShell prints it), and `doctor` says when completion isn't installed (spec_020 CLI-42).

### 3.3 Help (phase 3)

- **CLP-20** **`/help` from the tree.** `/help` lists the groups with one line per command. `/help <command>` shows its usage, sub-commands, flags, two or three examples and a link to its page on the docs site. `/help keys` shows the key bindings, and `/help topics` the guide pages. `blitz help <command>` and `--help` do the same for the cobra commands: Examples on every command, and a "Learn more:" link.
- **CLP-21** **The docs in the binary.**
  - The user docs (getting started, guide, tutorials, products and releases; no pictures) are embedded in the Go binaries as Markdown, by `//docs` (`docs/embed.go`), from the same files the desktop page bundles. They add about 0.6 MB.
  - `pkg/userdocs` reads them once, on first use: each page split into sections at its headings, searched by BM25, with words in a page's title or the heading counting more and release notes counting less.
  - `blitz docs [page|words]` and `/docs …` list the pages, show one (rendered with glamour, its links pointing at the site), or the best matches with their headings and addresses.
- **CLP-22** **Ask Blitz.** `? <question>` in the REPL, `/help <question>` (words that name no command and aren't a slip of one), and `blitz help "<question>"` ask Blitz.
  - The question goes to the session's model as an aside: recorded nowhere, its tools read-only. Its prompt carries the docs' sections that best match the question (at most 14,000 characters), and asks for the command to type and a link to the page used, with no invented commands.
  - The docs travel in the prompt rather than through a docs tool. Each built-in agent lists its tools and only `blitz` has the `blitz` tool, so this way any agent and any model, a small local one too, can answer.
  - `blitz help "<question>"` runs in a session kept nowhere.
  - `help.model`, a model of its own for help, is for later; for now it's the session's model (the owner's choice).
- **CLP-23** **Offline fallback.** When no model can be built (`ModelErr`) or the call fails, the question is answered without a model:
  - the best matching docs sections (CLP-21) and commands (CLP-10, a word in the name counting more than one in the description), each with its first lines
  - how to set up a model to ask in your own words

  A help question never fails just because there is no model.
- **CLP-24** **Errors that help.**
  - An error shows the service's own message, without the RPC's code (`not_found: …`, `rpc error: code = … desc = …`). `BLITZ_DEBUG=1` prints it whole; a `--debug` flag is for later.
  - An unknown command suggests the nearest ("Did you mean /permissions?"; a swap of two letters counts as one edit). So does an unknown sub-command, which used to do nothing.
  - Turning every raw `%v` in the REPL into one sentence of what to do is phase 6's.

### 3.4 Configuring through the agent (phase 4)

- **CLP-30** **Ask to configure.** `/configure <what>` (a bundled command run by the `blitz` agent, which has `web_fetch` and the `blitz` tool) and `blitz configure <what>` set Blitz up as asked. `/setup` was already the project's setup, hence the name. For "the MCP server described at https://…":
  1. It reads the page with `web_fetch`, which asks for the host as usual.
  2. It finds the server's command or URL and what it needs: env vars or headers, and which are secrets.
  3. It calls `mcp add` with all of it and the page as `source`. The person sees one approval: the server's `[[mcp.servers]]` entry as a diff, where it came from, and the secrets it'll ask for.
  4. On yes, it asks each secret not yet kept, hidden, saves them as the workspace's Blitz secrets and the server, reloads MCP, lists the server's tools (`mcp test`), and says when it signs in with OAuth (`blitz mcp login <name>`).
- **CLP-31** **`mcp` through the tool, completed.**
  - `mcp add` and `mcp edit` take `env`, `headers` (an address's only), `prefix`, `agents`, `tools`, `timeout_seconds` and `source`.
  - A value with `${NAME}` (or `{secrets:NAME}`) names a secret. A literal value of a secret-looking name (a token, key, secret, password, `Authorization`) is dropped and asked, named from the server and the variable (`LINEAR_API_TOKEN`, `Bearer ` kept). Either way the settings hold `{secrets:NAME}`, and the agent never sees the value.
  - `mcp edit` diffs the old entry against the new. `mcp remove` shows what goes. `mcp test` connects and lists the tools (`MCPManager.ToolNames`).
  - Workspace scope only, as the tool's changes are (DRV-06): the person moves one to every workspace (`blitz mcp make-global`, Settings).
- **CLP-32** **Settings through the tool.**
  - `settings set` takes, besides its five, the workspace's settings an agent may change: `ui.*`, `search.*`, the models (`llm.*.model`, `llm.*.models`, `blitz.default_model`, `images.model`, `audio.model`, `health.model`), `audio.voice`, `llm.provider`, `llm.providers` and `blitz.default_agent`.
  - It never takes what loosens limits (the mode beyond tightening, agency, jobs' policy), what redirects requests (base URLs, auth, projects and locations), what writes elsewhere (folders), or the person's own (`profile.*`). Those are refused with the `blitz config set` command the person can run.
  - The mode still only tightens, and unsafe is never reachable (spec_060, 63d9fb62).
  - `settings reference [key|prefix]` lists the sections, or each key's type, default, what it does, and whether the agent may set it.
- **CLP-33** **Providers through the tool.**
  - `provider list`, `provider add <name>` (the provider, its base URL, project or location; the key asked as hidden input and stored like `config set-key`), `provider check <name>` (`CheckApiKey`), and `provider remove`.
  - `provider add` is always asked, and it's refused while an organization provides the models (org policy).
- **CLP-34** **The rest of the catalog's gaps.**
  - Delete for connectors, rules, guardrails, secrets, agents, skills, workflows and jobs. Each is always asked and shows what goes.
  - `connector test`.
  - `rule save`/`guardrail save` diff against the old file (today the old side is empty).
  - `job create` through the tool (rejected today, `blitzcommands.go:1091`).
- **CLP-35** **Safety unchanged.**
  - Every change keeps spec_060's rules: `askChange` with a diff, `MustAsk` for what brings code (MCP servers, hooks, plugins), `ErrConfirmWhileAgentWorks`, `BLITZ_AGENT` refusing `--yes`, and the organization's policy.
  - A fetched page's text is data: the agent shows where each value came from (the page's URL) in the approval's detail.

### 3.5 Parity (phase 5)

The desktop's settings and Studio, reachable from the CLI. Each item is a `blitz <noun> <verb>` command with `--json`, plus the REPL's `/<noun>` where it's used in a session. They go through the service when attached.

- **CLP-40** **Settings.**
  - `blitz config get <key>` and `blitz config set <key> <value> [--workspace]` cover every reference key. `/config` takes `key value` as well as `key=value`, and `--save` writes either scope (today only global, and only seven keys).
  - `blitz config edit` opens the settings file in `$EDITOR` and checks it on save (`CheckConfigFile`), offering to reopen on an error.
  - `blitz config check` checks it without opening.
- **CLP-41** **Providers and models.**
  - `blitz providers list|add|check|remove|use`. `use` is the default provider: `SetProvider`.
  - `config set-key` validates with `CheckApiKey` before saving.
  - `/model` completes every provider's models (spec_061).
- **CLP-42** **Personality and About you.**
  - `blitz personality list|show|use|new|edit|try|remove`. `new` and `edit` open `$EDITOR` on the Markdown.
  - `blitz profile show|set name|pronouns|…` (About you).
  - `/personality` keeps picking.
- **CLP-43** **Agents, skills, rules, guardrails, workflows, evals, jobs.** For each:
  - `list`, `show`, `new`/`edit` (in `$EDITOR`, saved through the service with its validation), `remove`, and `enable`/`disable` where the desktop has them.
  - Rules: `reorder` and `try`. Guardrails: `try`.
  - Skills: `try`, `files`, `trust`.
  - Workflows: `run`, `runs`, `watch`, `stop`.
  - Jobs: `new`, `edit`, `remove`, `schedule` (preview).
  - Evals: `new`, `draft` (DraftEvalCases), `stop`.
  - Human tasks: `blitz tasks list|answer`, and `/tasks` answers them.
- **CLP-44** **Connectors and secrets.**
  - `blitz connectors list|show|add|edit|test|schema|remove|enable|disable|make-global`.
  - `add` asks each field in turn, from the connector's descriptor, with secrets hidden.
  - `blitz secrets list|set|remove`.
- **CLP-45** **Browser profiles.** `blitz browser profiles list|new|remove|clear|open|sites`, and `blitz browser use <profile>` for the workspace.
- **CLP-46** **Health, logs, language servers.**
  - `blitz health [--watch]`, the Studio overview's checks.
  - `blitz logs` is the service's log days, with `--day` and `--follow`. Background runs' output moves to `blitz runs logs` (CLP-50).
  - `blitz lsp status|restart`.
- **CLP-47** **Reasoning in the terminal.**
  - `/reasoning [turn]` draws a turn's graph (`GetReasoning`) as an indented tree: calls, steps of reasoning, tools with ✓ or ✗, sub-agents, and tokens and cost per call.
  - It folds to a summary per call, and `/reasoning <n> --full` prints every node.
  - `blitz sessions reasoning <id> [--turn n] [--json]`.
- **CLP-48** **Sessions.** `blitz sessions list|show|export|usage|remove`. `list` is missing today.
- **CLP-49** **Voice.** The settings only: `blitz voice show|set` for the `[voice]` keys. Talking stays in the desktop app (spec_063).

### 3.6 Consistency (phase 6)

- **CLP-50** **One word, one meaning.**
  - `blitz runs` (`list`, `logs`, `stop`, `attach`) is the background runs. *Built.*
  - `blitz attach`, `logs` and `stop` work for a release, hidden, each saying its new name.
  - `blitz agents` lists the runs for a release, saying it moves. It can't mean both at once; from the next release it lists the agents, as `/agents` does.
- **CLP-51** **Flags mean one thing.** *Built in part.*
  - The root's `-w` (`--worktree`) and `-a` (`--agent`) work for a release, saying they'll mean `--workspace` and `--all` as they do elsewhere (pflag's deprecated shorthands).
  - `-f` keeps its usual meanings, follow for `logs` and force elsewhere, as `tail`, `docker` and `git` use them.
  - `-y` is `--yes` where a command asks.
  - A test that walks the command tree for short flags with two meanings is for later.
- **CLP-52** **Removal is `remove`.** Every noun uses `remove`, with `rm` and `delete` (and `uninstall` for plugins and skills) as aliases. A remove asks on a terminal unless `-y` is given. In a script it does as it did, so scripts keep working. *For later.*
- **CLP-53** **`--json` on every list and show,** with the same field names as the RPC's JSON. *Built for* `runs`, `sessions list`, `mcp list` (names, targets, scope and state, never env or header values), `memory list`, `config get` and `config reference`, beside `do`, `data`, `eval`, `search`, `pkg validate` and `picture show`. The rest (`models`, `plugin list`, `skills list`, `jobs list`, `spec list`, `worktrees list`, `config keys`, `enterprise …`) and a test running each against the fake service are for later.
- **CLP-54** **Exit codes.** "Not found" exits with `exitNotFound` everywhere (a new code, documented in spec_020), and usage errors with `exitUsage`.
- **CLP-55** **Localised help.** Cobra's Short, Long and Example text and flag usage come from the i18n catalogs (`cli.cmd.<path>.short` and so on), so `blitz --help` reads in the person's language. The i18n lint covers the cobra tree.

### 3.7 Journeys (every phase)

- **CLP-60** Scripted journey tests drive the REPL through a pseudo-terminal against the fake service and a strict fake model. Each phase adds its own:
  - **Banner**: full, compact under 60 columns, none with `NO_COLOR` or a pipe.
  - **Completion**: `/sk` Tab → `/skills`, `/skills re` Tab → `registry`, `/config theme=` Tab → the themes, the menu moved and taken.
  - **Help**: `/help mcp`, `? how do I add an MCP server` with the fake model (the answer cites the MCP guide), and the same with no provider (offline matches).
  - **Configure**: "set up the MCP server described at <local test page>". The fake model fetches the page, proposes `mcp add` with a header, the approval shows the diff, the hidden secret is answered, and the server is saved with `{secrets:…}` and listed.
  - **Parity**: each new command's `--json` against the fake service.

## 4. Plan

Phases in order. Each phase is one commit with its tests, and each leaves the CLI working. The time estimates are for this session's pace.

| Phase | What | Requirements | Est. |
|---|---|---|---|
| 1 | Banner, colour rules, start-up tidy | CLP-01–04 | 1 h |
| 2 | The command tree; completion menu, ghost text, fuzzy; shell completion | CLP-10–14 | 2 h |
| 3 | `/help` from the tree, docs embedded, `blitz docs`, ask Blitz with offline fallback, helpful errors | CLP-20–24 | 1.5 h |
| 4 | Configure through the agent: `mcp` with env, headers and secrets, settings at any key, providers, deletes, `/setup <what>` | CLP-30–35 | 1.5 h |
| 5 | Parity commands, highest use first: config get/set/edit, providers, sessions list, connectors, secrets, reasoning tree, personality/profile, the Studio nouns, browser, health/logs/lsp, voice | CLP-40–49 | 2.5 h+ |
| 6 | Consistency: runs/agents, flags, remove, `--json`, exit codes, localised help | CLP-50–55 | 1 h |

Phase 5 is the longest and can be cut at any noun. What isn't done by the end of the session stays listed in §5 with its status.

Each phase also updates:
- spec_019 and spec_020 where it changes them
- the guide: the CLI page, `docs/content/products/cli.md` (the banner, completion, help, configure by asking), with `mcp.md`, `configuration.md` and `blitz-commands.md` updated
- the three locales, edited line by line
- the 1.3.0 release notes

## 5. Not in this spec

- A full-screen TUI: panes, mouse, a file tree. The CLI stays a line REPL with menus.
- Voice in the terminal.
- Git commit, branches and sync from the REPL: `!git` is there, and the agent has git.
- A terminal workflow canvas: workflows are edited as files and run from the CLI.

## 6. Decisions (the owner, 2026-11-22)

1. **The wordmark**: block letters (█-style, 5 lines) with "⚡ powered by Retail Cortex · v<version>" under them.
2. **Renames** (CLP-50, 51): yes — `blitz agents` → `blitz runs`, `-w` → `--workspace`, with one release of hidden aliases.
3. **Docs in the binary** (CLP-21): yes, embedded for offline help.
4. **Ask Blitz's model**: the session's model; `help.model` overrides it.
