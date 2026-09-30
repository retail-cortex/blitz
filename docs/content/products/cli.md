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
```

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

### Commands

| Command | |
|---|---|
| `/undo [--force]` | Revert the last turn's file changes (checkpoints are kept between runs) |
| `/rewind [n [mode]] [--force]` | Go back to before an earlier prompt: its files, the conversation, or both, or summarize from or up to it |
| `/checkpoints`, `/diff [git]` | Turns that changed files; everything tools changed in this session (or `git diff`) |
| `/cost`, `/context` | Token usage, estimated cost, context size against the compaction threshold |
| `/compact [focus]` | Summarize everything before the latest turn now |
| `/memory [reload\|add <note>]`, `/init` | Project instructions; have the agent write `BLITZ.md` |
| `/approvals [revoke <n>\|clear]` | Remembered approval rules |
| `/session list [--all]\|new\|load <id\|name>\|save <name>`, `/resume`, `/rename` | Sessions and snapshots, scoped to the workspace |
| `/cd [path]` | Move the session to another workspace: it opens from scratch (its settings, trust question, sandbox, MCP servers), background work is dealt with as at exit, and `--continue` there finds the session; earlier prompts can't be rewound or undone from the new one |
| `/tasks [show\|stop <id>]` | Background tasks (`invoke_agent` in the background) |
| `/trust` | The workspace's project settings (`.blitz/settings.toml`), and trusting them |
| `/agents`, `/agent <name>`, `/model <name>` | Agents and models; `/model anthropic/claude-sonnet-5` can switch provider |
| `/pin_model`, `/unpin`, `/model_settings`, `/effort` | Per-agent models, per-model settings, reasoning effort ([models](../guide/models.md)) |
| `/mode [name]`, `/permissions …` | Permission mode and rules ([safety](../guide/safety.md)) |
| `/plan <goal>` | A plan for approval, without changing anything |
| `/btw <question>` | A side question: answered with what the session knows, and not kept |
| `/search web <terms>`, `/search session <terms>` | [Search](../guide/search.md) the web or this session |
| `!<command>` | Run a command yourself, outside the agent's sandbox; the audit log records it |
| `/attach [path\|clear]`, `/paste` | Queue an [image](../guide/images.md) for the next message |
| `/sandbox`, `/mcp`, `/tools`, `/skills …`, `/envs …` | The active policy, MCP servers, the agent's tools, skills and their Python environments |
| `/locale [code]` | The [interface language](../guide/language.md) |
| `/license`, `/help`, `/exit` | |

Every [custom command](../guide/extending.md#custom-commands) and skill is also a slash command.
