---
title: CLI (blitz)
weight: 10
---

`blitz` (or its shortcut `blz`) is Blitz in the terminal: an interactive session in the current directory, or a single prompt that runs and exits. Source: `apps/cli`, with the interactive session in `apps/cli/internal/tui`. Specs: [CLI](../about/specs/spec_020_cli.md), [REPL](../about/specs/spec_019_tui.md).

## Running it

```bash
blitz                                   # an interactive session in the current directory
blitz -d ~/src/app                      # ...in another workspace
blitz "fix the failing test"            # run once and exit
blitz -i "fix the failing test"         # send it, then stay in the session
git diff | blitz review this change     # piped input becomes part of the prompt
blitz -p - < task.md                    # the prompt from stdin
blitz --continue "now add docs"         # continue this directory's most recent session
blitz --resume session-2026…            # resume a session (-r: this directory's latest)
blitz --resume=before-refactor          # start a new session from a snapshot
blitz --plan "add rate limiting"        # a plan only: reads and searches, no edits or commands
blitz --image ui.png "why is this misaligned?"   # attach images (@ui.png in the prompt works too)
blitz --add-dir ../shared-lib           # let the agent work in another directory too, for this run
```

`blitz` runs no workspace itself: it attaches to the [service](service.md), starting it if it isn't running (without a login item, a service started this way stops after 15 minutes with nothing to do). A run with settings of its own (`--local`, `--model`, `--agent`, `--append-system-prompt`, `--add-dir`, `--plugin-dir`, `--no-session-persistence`, `--trust-project`) gets a private service that stops when the run does; if the shared service has the workspace open and idle, it hands it over, and while a turn runs there the run is refused. `blitz doctor` runs its checks in `blitzd`, so it works with no service running.

### Run flags

The flags a run takes, on `blitz` and on `blitz exec`:

| Flag | |
|---|---|
| `-d, --dir DIR`, `-c, --config DIR` | The workspace (default the current directory) and the folder of the settings file (default `~/.blitz`); every command takes them |
| `-p, --prompt TEXT` (`-` reads stdin), `-i, --interactive` | A one-shot prompt; `-i` (on `blitz`, not `exec`) opens the interactive session even when a prompt is given |
| `-a, --agent NAME`, `-m, --model REF`, `--agency LEVEL`, `--effort LEVEL` | The agent, the model, how much it does on its own (`low`…`extreme`), and the reasoning effort |
| `-r, --resume [ID\|NAME]`, `-C, --continue`, `--fork`, `--name TITLE` | Resume a session (or start one from a snapshot), continue the latest; `--fork` continues in a copy of it, leaving it as it was; `--name` names the new session |
| `--allow RULE`, `--deny RULE` | A [permission rule](../guide/safety.md) for this run only, such as `--allow "shell(go test *)"` or `--deny "shell(git push *)"` (repeatable) |
| `--permission-mode MODE` | Start in `auto` (the default), `default`, `accept-edits`, `plan`, `dont-ask` or `bypass` (needs the OS sandbox) |
| `--plan`, `--image FILE`, `--bg` | A plan-only run; an image for the first prompt (repeatable); a background run in the service |
| `-w, --worktree[=NAME]`, `--ref REF` | Start in a new git worktree, from `REF` |
| `--add-dir DIR`, `--plugin-dir DIR`, `--append-system-prompt[-file]`, `--local`, `--trust-project` | Settings of the run's own: they run in a private service (above) |
| `--output-format`, `--input-format`, `--json-schema`, `--max-turns`, `--max-cost-usd`, `--timeout`, `--no-session-persistence` | [Scripting](#scripting) |

`blitz exec <prompt>` is `blitz` for one prompt that must be given: it runs it and exits, never opening the interactive session.

## Scripting

A one-shot run suits scripts and CI:

```bash
blitz --output-format json "…"          # one JSON result object on stdout
blitz --output-format stream-json "…"   # one JSON object per event, then the result
blitz --max-turns 20 "…"                # cap the model calls
blitz --max-cost-usd 0.50 --timeout 15m "…"   # cap the cost and the wall-clock time
blitz --permission-mode dont-ask "…"    # refuse whatever would ask, instead of waiting
blitz --output-format json --json-schema person.json "Who wrote this repo?"   # structured_result: JSON valid against the schema
blitz --no-session-persistence "…"      # leave no session behind
```

With `stream-json`, the lines are `session` first, then `text`, `tool_call` and `tool_result` as the turn goes, and these, then `result`:

| `type` | Keys | What it is |
|---|---|---|
| `notice` | `text`, `error`, `transient`, `author` | A message from Blitz about the turn, such as a guardrail stopping it ("Send continue to go on"); `transient` marks a passing status, such as a retry |
| `workflow_step` | `workflow`, `id`, `title`, `step_type`, `index`, `count`, `status`, `output`, `error`, `run` | A workflow's step starting (`running`) or ending (`done`, `failed`, `skipped`, `waiting`), step `index` of `count` |
| `tasks` | `tasks` (`[{"content", "status"}]`), `author` | The agent's whole task list each time it changes; `status` is `pending`, `in_progress` or `done` |

Every key is always there. Skip a `type` you don't know: new ones may come.

Another program can drive a whole session over pipes: with `--input-format stream-json --output-format stream-json`, each stdin line is a prompt or an answer, and approvals and questions come out as lines to answer:

```text
→ {"type": "user", "text": "add a test for parse()"}
← {"type": "approval_request", "id": "approval-1", "tool": "create_file", "detail": "…", …}
→ {"type": "approval", "id": "approval-1", "decision": "once"}
← {"type": "result", "result": "Added parse_test.go …", …}
→ {"type": "user", "text": "now run it"}
```

Decisions are `once`, `session`, `always` or `deny`; a question is answered with `{"type": "answer", "id": …, "answer": "…"}`. When stdin closes, Blitz finishes the queued prompts and exits.

| Exit code | Meaning |
|---|---|
| 0 | Success |
| 1 | A runtime or model error |
| 2 | Invalid flags or arguments |
| 3 | A limit stopped the run: `--max-turns`, `--max-cost-usd` or `--timeout` |
| 4 | The prompt was blocked by a `prompt_submit` hook |
| 130 | Interrupted |

## Other commands

| Command | |
|---|---|
| `blitz security`, `blitz security fix-apparmor` | Whether the OS sandbox for the agent's commands works here; on Linux, where AppArmor stops bubblewrap, install an AppArmor profile that lets bwrap alone through (asks for your password with sudo) |
| `blitz security scan-secrets --staged`, `blitz security scan-secrets <file>…` | Look for API keys and other secrets in what the commit being made adds, or in the files named; exits 1 naming each file and line (the value masked). The pre-commit hook runs it |
| `blitz security install-hook [dir]` | Add the pre-commit hook that stops commits adding secrets (`.githooks/pre-commit`, `core.hooksPath`) to the repository, unless it uses other hooks |
| `blitz doctor [--online]` | Checks the settings, credentials, sandbox, MCP servers, hooks and skills; `--online` also calls each model and a test search |
| `blitz config init\|show\|path` | Writes, shows or locates the settings file |
| `blitz config set-key\|keys\|secure-key\|remove-key` | API keys in the OS keychain ([providers](../guide/providers.md)) |
| `blitz eval <name>` | [Runs an eval](../guide/evals.md): each case on its agents, scored; exits 1 when it fails, for CI (`--case`, `--json`) |
| `blitz search <terms>` | [Searches the workspace](../guide/search.md#workspace-search): files, PDFs and notebooks, chats, notes (`--in`, `--json`, `--status`, `--reindex`) |
| `blitz jobs …` | Lists, enables, runs, undoes and shows the history of [jobs](../guide/workers.md) |
| `blitz mcp add\|list\|get\|remove\|enable\|disable\|login\|logout …` | Adds and manages [MCP servers](../guide/mcp.md); `login` signs in to one over OAuth |
| `blitz service install\|uninstall\|restart\|status` | Starts the service at login, or stops doing so; `restart` restarts it so it reads changed settings, MCP servers and plugins |
| `blitz init` | Runs `/setup` unattended: sets up the project for agents (`.agents/AGENTS.md` and the files that import it) |
| `blitz license [full\|third-party]` | The NOTICE, the Apache License, or the notices of the software Blitz includes |
| `blitz config set-auth <provider> <api_key\|adc\|oauth>` | How a provider signs in: an API key, Google Cloud ADC, or OAuth ([providers](../guide/providers.md)) |
| `blitz config permissions [allow\|ask\|deny\|remove <rule>]`, `… check <rule> [sample]`, `… defaults on\|off\|inherit` | The saved [permission rules](../guide/safety.md) (`--workspace` for the workspace's own): list, change, test a rule against a command or path, or turn the built-in read-only rules off |
| `blitz auth status\|login <provider>\|logout <provider>` | Sign in to or out of Google, Anthropic, AWS or Azure with its own tool ([providers](../guide/providers.md)) |
| `blitz trust [dir]` | Show a workspace's project settings and trust or decline them |
| `blitz sessions export <id> [-o file]` | A session as Markdown, or Word for a `.docx` name, without opening the workspace |
| `blitz jobs move <name> [--anyway]` | Move a job to your organization's service, saying first what can't run there ([organization](../guide/organization.md)) |
| `blitz enterprise connect\|disconnect\|login [--browser]\|logout\|status\|terms\|about` | Your [organization's service](../guide/organization.md): connect, sign in, its status, accepting its terms, and what it says about itself (about text, support, privacy notice) |
| `blitz enterprise workspaces\|open <workspace>\|create <name> [--personal] [--handle h]\|share [folder] [--name n] [--handle h] [--personal]`, `blitz enterprise sync [status]` | The organization's workspaces: list, clone, make, share a folder where it is, and save it to the service now |
| `blitz enterprise jobs [runs\|push\|run\|on\|off\|files\|pending\|approve\|deny\|take\|watch …] [--workspace h]`, `blitz enterprise notices` | The jobs the service runs for you, and how you hear of their runs (`--email=false` turns email off) |
| `blitz enterprise usage`, `blitz enterprise publish [<kind>/<name>…] [--lock] [--groups] [--title]`, `blitz enterprise reviews [--state s]`, `blitz enterprise catalog [--kind k]\|copy <kind>/<name>` | Your use of the organization's models; publishing to its catalog and the reviews; what the catalog gives this workspace (`-d` names the workspace) |
| `blitz git-credential get\|store\|erase` | Git's credential helper for the organization's service |
| `blitz completion bash\|zsh\|fish\|powershell` | Shell completion |

## The interactive session

- **Line editing** with history (`~/.blitz/history`, owner-only), Ctrl+R search, and Tab completion for `/commands`, their arguments and `@path` references.
- **Multi-line input**: end a line with `\`, or put a block between two lines of `"""`.
- **Streaming Markdown**, a progress spinner, and a usage line after each turn: `↳ 12.4k in · 1.2k out · context 12.3k · $0.0023`.
- **Steering**: while the agent works, start typing (or press Ctrl+T) to send it a message. It reaches the agent with the next tool result, so nothing is interrupted. (macOS and Linux.)
- **Ctrl+C** or **Esc** cancels the running turn; at the prompt, Ctrl+C exits.
- **Pickers**: `/resume`, `/agent` and `/model` without an argument, approvals and the agent's questions open a menu under the prompt. Arrow keys move, typing filters, Enter chooses. An approval starts on **No**, so an Enter typed ahead never approves.
- **Shift+Tab** cycles the permission mode (`default` → `accept-edits` → `auto` → `plan`). **Ctrl+G** opens the prompt in `$VISUAL` or `$EDITOR`. **Esc Esc** clears the line, or on an empty line opens `/rewind`.
- **Vim and keys**: `ui.editor = "vim"` edits the prompt vi-style. `~/.blitz/keybindings.toml` moves the REPL's keys and binds keys to commands:

  ```toml
  editor = "ctrl+e"        # instead of Ctrl+G
  cycle_mode = "shift+tab"
  [commands]
  "ctrl+t" = "/tasks"      # at an empty prompt
  ```

- **Status line**: `ui.status_line = "default"` shows the model, mode, context and cost above each prompt; set it to a command instead and its first line of output is shown, with the session's state as JSON on its stdin (fields in [spec_019_tui](../about/specs/spec_019_tui.md) TUI-48).
- **Notifications**: when a turn has run longer than `ui.notify_after` seconds (30), its end and any approval or question it waits on ring the bell and show a desktop notification. `ui.notify` picks `both`, `bell`, `desktop` or `off` (another value is a warning at start, and `both`).

### Commands

| Command | |
|---|---|
| `/undo [--force]` | Revert this session's last turn's file changes, not another chat's (checkpoints are kept between runs) |
| `/rewind [n [mode]] [--force]` | Go back to before an earlier prompt: its files, the conversation, or both, or summarize from or up to it |
| `/checkpoints`, `/diff [git]` | Turns that changed files; everything tools changed in this session, a file at a time (or `git diff`) |
| `/cost`, `/context` | Token usage, estimated cost, context size against the compaction threshold and what it's made of |
| `/status` | Version, workspace, session, agent, model, mode, sandbox, MCP servers, settings files |
| `/config [key=value [--save]]`, `/set` | The session's settings; change one, and keep it with `--save` |
| `/theme [name [--save]]` | Markdown and diff colours: `auto`, `dark`, `light`, `dracula`, `tokyo-night`, `pink`, `ascii`, `notty` |
| `/copy [n]` | Copy the last answer (or the nth from last) to the clipboard; OSC 52 over SSH |
| `/compact [focus]` | Summarize everything before the latest turn now |
| `/reload` | Read the agents, skills, instructions and rules again (each message already picks up changes) |
| `/clear` | Clear the terminal screen |
| `/memory [reload\|add <note>\|notes\|forget <name>]`, `/init` | Project instructions and the agent's notes; `/init` runs `/setup` |
| `/approvals [revoke <n>\|clear]` | Remembered approval rules |
| `/hooks` | The hooks, where they come from, and their recent failures |
| `/fork [n]` | Continue in a copy of the session, up to prompt `n` |
| `/export [file] [--force]` | The session as Markdown, or a Word document for a `.docx` name: prompts, answers, tool calls (`blitz sessions export <id>` from the shell). An existing file is kept unless `--force` |
| `/style [name]` | How the agent writes its answers: `default`, `concise`, `explanatory`, or yours in `~/.blitz/styles` |
| `/session list [--all]\|new\|load <id\|name>\|save <name>`, `/resume`, `/rename` | Sessions and snapshots, scoped to the workspace |
| `blitz skills registry add\|list\|remove`, `blitz skills search\|install\|update\|uninstall\|list` | Skills from registries such as Castor, hash-checked ([skills](../guide/skills.md#skills-from-a-registry)) |
| `blitz plugin install\|list\|show\|enable\|disable\|remove\|update`, `blitz plugin marketplace add\|list\|remove`, `blitz plugin import claude\|gemini <dir>`, `--plugin-dir <dir>` | Plugins: skills, commands, agents, hooks and MCP servers in one bundle ([extending](../guide/extending.md#plugins)) |
| `blitz --bg "<prompt>"` | Start the prompt as a background run in the Blitz service and return |
| `blitz agents [--all]`, `blitz attach <id>`, `blitz logs <id> [-f]`, `blitz stop <id>` | The background runs in every workspace; follow one in the REPL, answering what it asks, then carry on in its session; show what it has done; stop it |
| `blitz update [--check]` | Install the latest release over this one, its signature and checksum checked |
| `blitz models [provider…]` | The models your providers offer, with the prices Blitz knows |
| `blitz memory list\|show\|edit\|forget <name>` | The notes the agent saved in the workspace with its `remember` tool |
| `blitz --worktree[=name] [--ref r]` (the name after `=`: a word after a space is the prompt), `blitz worktrees list\|remove <name>\|prune` | Work in a new git worktree and branch (`.blitz/worktrees/<name>`, `blitz/<name>`), with `.worktreeinclude`'s files copied in; list and remove them |
| `/cd [path]` | Move the session to another workspace: it opens from scratch (its settings, trust question, sandbox, MCP servers), background work is dealt with as at exit, and `--continue` there finds the session; earlier prompts can't be rewound or undone from the new one |
| `/tasks [show\|stop <id>]` | Background tasks (`invoke_agent` in the background) |
| `/processes [all\|log <id>\|stop <id>…\|stop flagged\|stop all]` | The agents' background processes, what they use and the ones that look bad ([Safety](../guide/safety.md#background-processes)) |
| `/trust` | The workspace's project settings (`.blitz/settings.toml`), and trusting them |
| `/agents`, `/agent <name>`, `/model <name>` | Agents and models; `/model anthropic/claude-sonnet-5` can switch provider |
| `/pin_model`, `/unpin`, `/model_settings`, `/effort` | Per-agent models, per-model settings, reasoning effort ([models](../guide/models.md)) |
| `/mode [name]`, `/permissions …` | Permission mode and rules ([safety](../guide/safety.md)) |
| `/plan <goal>` | A plan for approval, without changing anything |
| `/grill-me <task>` | The agent asks you clarifying questions first, then proposes an approach; it changes nothing |
| `/spec [what you're working on, or what changed]` | Write or revise the project's specification, for software, research, data analysis or writing: the spec agent asks what kind of project it is, then what it's for and what that kind needs (for software its users, performance, platform and sign-in; for research the question, data, methods, compute, ethics and outputs), then writes numbered specs in `.agents/specs/` (`001-overview.md`, then each deliverable in the order it's built). Run it before `/setup`, which reads it |
| `/skills update [focus]`, `/agents update [focus]`, `/rules update [focus]` | Revisit the project's skills, agents, or rules and guardrails against its specification and what happened in its runs, and add, change or retire the ones you choose; run them after a spec changes |
| `/reflect [since]` | Learn from this workspace's runs: what went wrong since the last reflection (or a date, or a number of days), the patterns, and the smallest change for each (an entry in `.agents/EXPERIENCE.md`, an instruction, a skill's step, an agent's tools, a rule); changes only what you pick. The banner says when there's enough to learn from |
| `/setup [focus]` | Set up the project's agent harness (with no specification yet, it writes one first, asking the specification's questions): the setup agent asks about the project, makes it a git repository with a `.gitignore` for its stack, writes `.agents/AGENTS.md` and an empty `.agents/EXPERIENCE.md`, makes `AGENTS.md`, `CLAUDE.md`, `GEMINI.md` and `BLITZ.md` import it, and offers skills, agents, rules and guardrails to add (also `/init`, `blitz init`) |
| `/goal <condition>`, `/goal [clear]` | Keep the agent working until the condition holds, judged after each turn |
| `/loop <interval> <prompt>`, `/loop [stop <n>]` | Send a prompt again every interval while the REPL stays open |
| `/btw <question>` | A side question: answered with what the session knows, and not kept |
| `/search <terms>`, `/search files\|documents\|chats\|notes\|all <terms>`, `/search status\|reindex` | [Search the workspace](../guide/search.md#workspace-search) |
| `/search web <terms>`, `/search session <terms>` | [Search](../guide/search.md) the web or this session |
| `!<command>` | Run a command yourself, outside the agent's sandbox; the audit log records it |
| `/attach [path\|clear]`, `/paste` | Queue an [attachment](../guide/images.md) (an image, a PDF, a text file, audio or video, as the model takes them) for the next message |
| `/sandbox`, `/mcp`, `/tools`, `/skills …`, `/envs …` | The active policy, MCP servers, the agent's tools, skills and their Python environments |
| `/locale [code]` | The [interface language](../guide/language.md) |
| `/license`, `/help`, `/exit` | |

Every [custom command](../guide/extending.md#custom-commands) and skill is also a slash command.
