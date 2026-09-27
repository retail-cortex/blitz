# spec_tui_019 — Interactive terminal (REPL)

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `internal/tui/*.go` |
| Tests | `internal/tui/*_test.go` (notably `tui_test.go`, `steer_test.go`, `exit_test.go`, `btw_test.go`, `search_test.go`, `remote_test.go`, `i18n_lint_test.go`) |
| Depends on | [spec_workspace_018](spec_workspace_018.md) (all behaviour goes through `app.Backend`), [spec_i18n_004](spec_i18n_004.md) |
| Used by | [spec_cli_020](spec_cli_020.md) |

## 1. Purpose

The REPL is a thin front end over `app.Backend`: it reads input, dispatches slash commands to typed workspace operations, renders turn events, and handles terminal concerns (line editing, steering, Ctrl+C, background processes on exit). It works identically in-process or attached to the service. All user-facing text comes from i18n catalogs.

## 2. Presentation

- **TUI-01** Banner: one line `Blitz <version>  agent <name> · model <name>`, then a dim hint (`/help`, Ctrl+C); then the first sandbox summary line, a hint line, and (when steering is available) a steering hint.
- **TUI-02** Prompt: bold green `<agent> ›`. Status marks only `✓ ✗ ! ↳`; no mascot or persona.
- **TUI-03** Model text streams; with `ui.markdown` on a TTY it is rendered as Markdown (glamour, theme `ui.theme`: auto/dark/light/notty), falling back to plain text on error. Thought text and final text that repeats streamed chunks are not printed.
- **TUI-04** Tool calls and results are printed as compact one-line summaries; a spinner (`ui.spinner`, TTY only) shows "thinking"/"working" with elapsed time.
- **TUI-05** After each turn, a dim usage line: `↳ <in> in · <out> out · context <ctx> · $<turn> ($<total>)` (tokens humanised `12.4k`, `1.2M`; cost only when priced).
- **TUI-06** Terminal window title (`ui.terminal_title`, TTY): `Blitz · <session title>`, restored on exit. On exit, if the session has messages: a dim `blitz --resume=<id>` hint.
- **TUI-07** All printed model/tool/user-controlled text is sanitised of terminal control sequences.

## 3. Input

- **TUI-10** One input source for everything read from the terminal (REPL, approvals, `ask_user_question`, steering) — separate readers on the same fd would steal each other's buffered input. A read abandoned by cancellation stays in flight and its line goes to the next caller.
- **TUI-11** On a TTY: line editor with history in `ui.history_file` (created owner-only, `ui.history_size` 1000), Ctrl+R reverse search, Tab completion for `/commands`, their arguments ([spec_cli_020](spec_cli_020.md) CLI-40/41) and `@path` files.
- **TUI-12** Multi-line entries: end a line with `\`, or enclose a block between two `"""` lines.

## 4. Dispatch

- **TUI-20** Empty lines are ignored. `!<cmd>` runs a user shell command ([spec_shell_007](spec_shell_007.md) SH-50): terminal attached (interactive programs work), user's environment, audited; the Ctrl+C it receives doesn't count as exit.
- **TUI-21** `/btw <q>` → aside turn; `/search web|session <terms>` → read-only turn (mode `search`) with the recorded text being the command; `/plan <goal>` → plan turn (a goal starting with `/` is still text). Missing arguments print usage.
- **TUI-22** Other `/commands` go to `HandleCommand`; anything else is a prompt to the active session (re-read each turn, since `/session new|load` switch sessions).

### 4.1 Commands

| Command | Behaviour |
|---|---|
| `/help` | Command list |
| `/agents`, `/agent <name>` | List (active and pinned marked) / switch agent |
| `/model [<ref>]` | Show / switch model for unpinned agents; warns if the active agent is pinned |
| `/pin_model [<agent> <model>]`, `/unpin <agent>` | Show pins / pin / unpin; reports where it was saved |
| `/model_settings [<model> [k=v…\|reset]]` | Show or change one model's settings; `k=` clears; warns about unsupported keys |
| `/set [agency=<level>]`, `/show` | Show or change settings |
| `/skills list\|show <n>\|search <q>` | Skills and policy verdicts |
| `/envs [prune\|remove <key>]` | Script environments |
| `/session list [--all]\|new\|load <ref>\|save <name> [--force]`, `/resume <ref>`, `/rename <name>` | Sessions ([spec_sessions_017](spec_sessions_017.md)); resuming prints a recap |
| `/undo [--force]`, `/checkpoints`, `/diff [git]` | Checkpoints ([spec_filetools_006](spec_filetools_006.md)) |
| `/cost`, `/context` | Usage incl. cache reads/writes and cost; context size vs threshold |
| `/compact [focus]` | Manual compaction with before/after |
| `/memory [show\|reload\|add <note>]` | Project memory |
| `/approvals [revoke <n>\|clear]` | Rules grouped by kind (`cmd`, `write`, `delete`, `web`, `mcp`), numbered for revoke |
| `/sandbox`, `/mcp`, `/tools` | Policy summary; MCP servers; the active agent's tools (plan-mode-allowed marked) and MCP offers |
| `/attach [path\|clear]`, `/paste` | Queue images for the next prompt |
| `/locale [code]` | Show / switch language |
| `/clear` | Clear the screen |
| `/exit`, `/quit` | Exit (with background-process confirmation) |

## 5. Turns

- **TUI-30** Queued attachments (and `@image` mentions) are collected before a non-aside turn; a failure aborts the turn. Once accepted, attachment summaries print and the queue clears; plan/btw modes print a dim note.
- **TUI-31** Ctrl+C during a turn cancels only that turn ("interrupted"); during an approval prompt (raw mode) it also cancels the turn. A blocked prompt prints "✗ prompt blocked: <reason>".
- **TUI-32** Steering (macOS, Linux; TTY line editor): while a turn runs, typing or Ctrl+T opens a steer prompt pre-filled with what was typed; output is held back while typing and released afterwards (the spinner resumes only if it was showing). An empty message cancels. The message goes through `Backend.Steer` (hooks apply; a block is shown). Stopping the watcher at the end of the turn waits for an open steer prompt, so the message isn't lost.
- **TUI-33** Leftover steer messages (sent after the model's last tool call) are sent as the next turn, marked accepted; if the turn was interrupted they are dropped with a notice.

## 6. Exit

- **TUI-40** Ctrl+C or `/exit` at the prompt, or EOF: if background processes run, list them (`[id] command (Ns)`) and ask **k**ill / **w**ait / **c**ancel (cancel only in the REPL); another Ctrl+C or EOF force-quits and kills them; waiting can itself be interrupted (force-quit). Without a way to ask, they are killed. SIGTERM exits without prompting (cleanup kills processes).
- **TUI-41** When attached to the service, `Processes()` is nil and no exit prompt is needed.

## 7. Approver and questions
- **TUI-50** Approval prompt: tool detail, coloured diff truncated to `ui.diff_lines` (`d` shows all), answers `y`/`yes`, `s`/`session`, `a`/`always`, `n`; anything other than an explicit yes-type answer denies. The rememberable scope is described by the request's key label.
- **TUI-51** `ask_user_question`: shows the question and numbered options; a numeric answer selects that option.
