---
title: CLI (blitz)
weight: 10
---

`blitz` (or its shortcut `blz`) is Blitz in the terminal: an interactive session in the current directory, or a single prompt that runs and exits. Source: `apps/cli`, with the interactive session in `apps/cli/internal/tui`. Specs: [CLI](../about/specs/spec_cli_020.md), [REPL](../about/specs/spec_tui_019.md).

## Running it

```bash
blitz                                   # an interactive session in the current directory
blitz -d ~/src/app                      # ...in another workspace
blitz "fix the failing test"            # run once and exit
git diff | blitz review this change     # piped input becomes part of the prompt
blitz -p - < task.md                    # the prompt from stdin
blitz --continue "now add docs"         # continue this directory's most recent session
blitz --resume session-2026…            # resume a session (-r: this directory's latest)
blitz --resume=before-refactor          # start a new session from a snapshot
blitz --plan "add rate limiting"        # a plan only: reads and searches, no edits or commands
blitz --image ui.png "why is this misaligned?"   # attach images (@ui.png in the prompt works too)
blitz --add-dir ../shared-lib           # let the agent work in another directory too, for this run
```

When the [service](service.md) is running, `blitz` attaches to it and says so; `--local` runs the workspace in its own process instead. A workspace held by the service can't also be opened with `--local`.

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
| `blitz doctor [--online]` | Checks the settings, credentials, sandbox, MCP servers, hooks and skills; `--online` also calls each model and a test search |
| `blitz config init\|show\|path` | Writes, shows or locates the settings file |
| `blitz config set-key\|keys\|secure-key\|remove-key` | API keys in the OS keychain ([configuration](../guide/configuration.md)) |
| `blitz workers …` | Lists, enables, runs and shows the history of [workers](service.md#workers) |
| `blitz service install\|uninstall\|status` | Starts the service at login, or stops doing so |
| `blitz init` | Has the agent write or update the project's `BLITZ.md` |
| `blitz license [full\|third-party]` | The NOTICE, the Apache License, or the notices of the software Blitz includes |
| `blitz completion bash\|zsh\|fish\|powershell` | Shell completion |

## The interactive session

- **Line editing** with history (`~/.blitz/history`, owner-only), Ctrl+R search, and Tab completion for `/commands`, their arguments and `@path` references.
- **Multi-line input**: end a line with `\`, or put a block between two lines of `"""`.
- **Streaming Markdown**, a progress spinner, and a usage line after each turn: `↳ 12.4k in · 1.2k out · context 12.3k · $0.0023`.
- **Steering**: while the agent works, start typing (or press Ctrl+T) to send it a message. It reaches the agent with the next tool result, so nothing is interrupted. (macOS and Linux.)
- **Ctrl+C** or **Esc** cancels the running turn; at the prompt, Ctrl+C exits.
- **Pickers**: `/resume`, `/agent` and `/model` without an argument, approvals and the agent's questions open a menu under the prompt. Arrow keys move, typing filters, Enter chooses.
- **Shift+Tab** cycles the permission mode (`default` → `accept-edits` → `plan`). **Ctrl+G** opens the prompt in `$VISUAL` or `$EDITOR`. **Esc Esc** clears the line, or on an empty line opens `/rewind`.
- **Vim and keys**: `ui.editor = "vim"` edits the prompt vi-style. `~/.blitz/keybindings.toml` moves the REPL's keys and binds keys to commands:

  ```toml
  editor = "ctrl+e"        # instead of Ctrl+G
  cycle_mode = "shift+tab"
  [commands]
  "ctrl+t" = "/tasks"      # at an empty prompt
  ```

- **Status line**: `ui.status_line = "default"` shows the model, mode, context and cost above each prompt; set it to a command instead and its first line of output is shown, with the session's state as JSON on its stdin (fields in [spec_tui_019](../about/specs/spec_tui_019.md) TUI-48).
- **Notifications**: when a turn has run longer than `ui.notify_after` seconds (30), its end and any approval or question it waits on ring the bell and show a desktop notification. `ui.notify` picks `both`, `bell`, `desktop` or `off`.

### Commands

| Command | |
|---|---|
| `/undo [--force]` | Revert the last turn's file changes (checkpoints are kept between runs) |
| `/rewind [n [mode]] [--force]` | Go back to before an earlier prompt: its files, the conversation, or both, or summarize from or up to it |
| `/checkpoints`, `/diff [git]` | Turns that changed files; everything tools changed in this session, a file at a time (or `git diff`) |
| `/cost`, `/context` | Token usage, estimated cost, context size against the compaction threshold and what it's made of |
| `/status` | Version, workspace, session, agent, model, mode, sandbox, MCP servers, settings files |
| `/config [key=value [--save]]`, `/set` | The session's settings; change one, and keep it with `--save` |
| `/theme [name [--save]]` | Markdown and diff colours: `auto`, `dark`, `light`, `dracula`, `tokyo-night`, `pink`, `ascii`, `notty` |
| `/copy [n]` | Copy the last answer (or the nth from last) to the clipboard; OSC 52 over SSH |
| `/compact [focus]` | Summarize everything before the latest turn now |
| `/memory [reload\|add <note>\|notes\|forget <name>]`, `/init` | Project instructions and the agent's notes; `/init` runs `/setup` |
| `/approvals [revoke <n>\|clear]` | Remembered approval rules |
| `/hooks` | The hooks, where they come from, and their recent failures |
| `/fork [n]` | Continue in a copy of the session, up to prompt `n` |
| `/export [file]` | The session as Markdown: prompts, answers, tool calls (`blitz sessions export <id>` from the shell) |
| `/style [name]` | How the agent writes its answers: `default`, `concise`, `explanatory`, or yours in `~/.blitz/styles` |
| `/session list [--all]\|new\|load <id\|name>\|save <name>`, `/resume`, `/rename` | Sessions and snapshots, scoped to the workspace |
| `blitz plugin install\|list\|show\|enable\|disable\|remove\|update`, `blitz plugin marketplace add\|list\|remove`, `blitz plugin import claude\|gemini <dir>`, `--plugin-dir <dir>` | Plugins: skills, commands, agents, hooks and MCP servers in one bundle ([extending](../guide/extending.md#plugins)) |
| `blitz --bg "<prompt>"` | Start the prompt as a background run in the Blitz service and return |
| `blitz agents [--all]`, `blitz attach <id>`, `blitz logs <id> [-f]`, `blitz stop <id>` | The background runs in every workspace; follow one in the REPL, answering what it asks, then carry on in its session; show what it has done; stop it |
| `blitz update [--check]` | Install the latest release over this one, its signature and checksum checked |
| `blitz models [provider…]` | The models your providers offer, with the prices Blitz knows |
| `blitz memory list\|show\|edit\|forget <name>` | The notes the agent saved in the workspace with its `remember` tool |
| `blitz --worktree[=name] [--ref r]` (the name after `=`: a word after a space is the prompt), `blitz worktrees list\|remove <name>\|prune` | Work in a new git worktree and branch (`.blitz/worktrees/<name>`, `blitz/<name>`), with `.worktreeinclude`'s files copied in; list and remove them |
| `/cd [path]` | Move the session to another workspace: it opens from scratch (its settings, trust question, sandbox, MCP servers), background work is dealt with as at exit, and `--continue` there finds the session; earlier prompts can't be rewound or undone from the new one |
| `/tasks [show\|stop <id>]` | Background tasks (`invoke_agent` in the background) |
| `/trust` | The workspace's project settings (`.blitz/settings.toml`), and trusting them |
| `/agents`, `/agent <name>`, `/model <name>` | Agents and models; `/model anthropic/claude-sonnet-5` can switch provider |
| `/pin_model`, `/unpin`, `/model_settings`, `/effort` | Per-agent models, per-model settings, reasoning effort ([models](../guide/models.md)) |
| `/mode [name]`, `/permissions …` | Permission mode and rules ([safety](../guide/safety.md)) |
| `/plan <goal>` | A plan for approval, without changing anything |
| `/grill-me <task>` | The agent asks you clarifying questions first, then proposes an approach; it changes nothing |
| `/setup [focus]` | Set up the project's agent harness: the setup agent asks about the project, makes it a git repository with a `.gitignore` for its stack, writes `.agents/AGENT.md`, makes `AGENTS.md`, `CLAUDE.md`, `GEMINI.md` and `BLITZ.md` import it, and offers skills and agents to add (also `/init`, `blitz init`) |
| `/goal <condition>`, `/goal [clear]` | Keep the agent working until the condition holds, judged after each turn |
| `/loop <interval> <prompt>`, `/loop [stop <n>]` | Send a prompt again every interval while the REPL stays open |
| `/btw <question>` | A side question: answered with what the session knows, and not kept |
| `/search web <terms>`, `/search session <terms>` | [Search](../guide/search.md) the web or this session |
| `!<command>` | Run a command yourself, outside the agent's sandbox; the audit log records it |
| `/attach [path\|clear]`, `/paste` | Queue an [image](../guide/images.md) for the next message |
| `/sandbox`, `/mcp`, `/tools`, `/skills …`, `/envs …` | The active policy, MCP servers, the agent's tools, skills and their Python environments |
| `/locale [code]` | The [interface language](../guide/language.md) |
| `/license`, `/help`, `/exit` | |

Every [custom command](../guide/extending.md#custom-commands) and skill is also a slash command.
