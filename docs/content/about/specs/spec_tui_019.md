---
title: "019 · TUI"
weight: 19
---

*Interactive terminal (REPL)* (`spec_tui_019`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `apps/cli/internal/tui/*.go` |
| Tests | `apps/cli/internal/tui/*_test.go` (notably `tui_test.go`, `steer_test.go`, `exit_test.go`, `btw_test.go`, `search_test.go`, `remote_test.go`, `i18n_lint_test.go`) |
| Depends on | [spec_workspace_018](spec_workspace_018.md) (all behaviour goes through `api.Backend`), [spec_i18n_004](spec_i18n_004.md) |
| Used by | [spec_cli_020](spec_cli_020.md) |

## 1. Purpose

The REPL is a thin front end over `api.Backend`: it reads input, dispatches slash commands to typed workspace operations, renders turn events, and handles terminal concerns (line editing, steering, Ctrl+C, background processes on exit). It works identically in-process or attached to the service. All user-facing text comes from i18n catalogs.

## 2. Presentation

- **TUI-01** Banner: one line `Blitz <version>  agent <name> · model <name>`, then a dim hint (`/help`, Ctrl+C); then the first sandbox summary line, a hint line, and (when steering is available) a steering hint and a keys hint (Shift+Tab, Ctrl+G, Esc Esc).
- **TUI-02** Prompt: bold green `<agent> ›`, with `[mode]` after the agent when the permission mode isn't `default` (yellow; red for `bypass`). Status marks only `✓ ✗ ! ↳`; no mascot or persona.
- **TUI-03** Model text streams; with `ui.markdown` on a TTY it is rendered as Markdown (glamour, theme `ui.theme`: auto/dark/light/notty), falling back to plain text on error. Thought text and final text that repeats streamed chunks are not printed.
- **TUI-04** Tool calls and results are printed as compact one-line summaries; a spinner (`ui.spinner`, TTY only) shows "thinking"/"working" with elapsed time.
- **TUI-05** After each turn, a dim usage line: `↳ <in> in · <out> out · context <ctx> · $<turn> ($<total>)` (tokens humanised `12.4k`, `1.2M`; cost only when priced).
- **TUI-06** Terminal window title (`ui.terminal_title`, TTY): `Blitz · <session title>`, restored on exit. On exit, if the session has messages: a dim `blitz --resume=<id>` hint.
- **TUI-07** All printed model/tool/user-controlled text is sanitised of terminal control sequences.

## 3. Input

- **TUI-10** One input source for everything read from the terminal (REPL, approvals, `ask_user_question`, steering) — separate readers on the same fd would steal each other's buffered input. A read abandoned by cancellation stays in flight and its line goes to the next caller.
- **TUI-11** On a TTY: line editor with history in `ui.history_file` (created owner-only, `ui.history_size` 1000), Ctrl+R reverse search, Tab completion for `/commands`, their arguments ([spec_cli_020](spec_cli_020.md) CLI-40/41) and `@path` files.
- **TUI-13** Keys at the REPL prompt: Shift+Tab cycles `default → accept-edits → plan → default` (inserting `bypass` after `plan` only when the REPL started in `bypass`; failing to enter it skips to `default`) and redraws the prompt; Ctrl+G opens the line in `$VISUAL`/`$EDITOR` (run through `/bin/sh`, so it may carry arguments) and submits the saved text, echoed after the prompt (no editor, a failure or an empty file: a note, and the typed line comes back); Esc Esc within 600 ms clears the line, or on an empty line opens `/rewind` (TUI-17). Ctrl+G works in the steer prompt too; Shift+Tab and Ctrl+G do nothing at approvals and questions.
- **TUI-14** Esc: the line editor reads a lone Esc byte as the start of an escape sequence, so its stdin (`escReader`) turns a read of just `ESC` into a private-use rune first (a terminal sends a key's sequence in one write). Keys that must change or end the line inject bytes the editor reads next (Ctrl+E Ctrl+U to clear, Enter to submit, Ctrl+C to interrupt). Esc at an approval or question is Ctrl+C; in the steer prompt it submits nothing (the message is dropped, the turn goes on).
- **TUI-15** Pickers (`Picker`, on a TTY only; piped input keeps the line prompts): a title (earlier lines of a multi-line title print once above), up to 10 items with the current one marked `❯`, a filter typed after the title (words matched in label and detail, case-insensitive), and a hint line with `n/total` when scrolling. ↑/↓, Ctrl+P/N, PgUp/PgDn move; Backspace and Ctrl+U edit the filter; Enter chooses; Esc clears the filter, then cancels (`ErrPickCancelled`); Ctrl+C (the terminal's signal key is off while picking) cancels and runs the turn's interrupt handler. An item's `Key` chooses it at once when typed before any filter. Every drawn line is clipped to the width so the menu can be erased and redrawn in place; afterwards one dim line `<title> <choice>` stays. Without raw keys it falls back to a numbered list read on the line editor (number, key, or text matching one item).
- **TUI-16** Picker uses: `/agent` and `/model` without an argument (choosing the current one just shows it; models offered are the current one, pinned ones and those with settings), `/resume` without an argument (this workspace's sessions with messages, newest first, up to 50, then snapshots; Esc does nothing), approvals (Yes; Yes this session / always for the request's label; Show the whole diff; No — keys `y s a d n`; Esc denies and stops the turn) and `ask_user_question` with options (plus "Another answer…", read on the line; Esc stops the turn).
- **TUI-17** `/rewind [n [mode]] [--force]` (Esc Esc on an empty REPL line, via `ErrRewindKey`): on a TTY, a picker of the session's prompts (newest first; detail: time and files changed from it), then of the modes that apply (code modes only if files changed from that prompt on; conversation and summarize modes only if the prompt's place in the event log is known), a conflict asks to overwrite or cancel; on a pipe, a numbered list (1 = latest) and `/rewind <n> [both|conversation|code|summarize-from|summarize-up-to]` (default: both, or conversation when no files changed, or code for an old prompt). After a conversation rewind the prompt is the next line's starting text (`SetNextInput`), or printed on a pipe.
- **TUI-12** Multi-line entries: end a line with `\`, or enclose a block between two `"""` lines.

## 4. Dispatch

- **TUI-20** Empty lines are ignored. `!<cmd>` runs a user shell command ([spec_shell_007](spec_shell_007.md) SH-50): terminal attached (interactive programs work), user's environment, audited; the Ctrl+C it receives doesn't count as exit.
- **TUI-21** `/btw <q>` → aside turn; `/search web|session <terms>` → read-only turn (mode `search`) with the recorded text being the command; `/plan <goal>` → plan turn (a goal starting with `/` is still text). Missing arguments print usage.
- **TUI-22** Other `/commands` go to `HandleCommand`; a name that isn't built in but is a custom command (SK-80) runs as a turn with `Command` set; anything else is a prompt to the active session (re-read each turn, since `/session new|load` switch sessions).

### 4.1 Commands

| Command | Behaviour |
|---|---|
| `/help` | Command list |
| `/agents`, `/agent <name>` | List (active and pinned marked) / switch agent |
| `/model [<ref>]` | Show / switch model for unpinned agents; warns if the active agent is pinned |
| `/pin_model [<agent> <model>]`, `/unpin <agent>` | Show pins / pin / unpin; reports where it was saved |
| `/model_settings [<model> [k=v…\|reset]]` | Show or change one model's settings; `k=` clears; warns about unsupported keys |
| `/config [key=value [--save]]`, `/set`, `/show` | Show the session's settings or change one (`model`, `agent`, `mode`, `agency`, `effort`, `style`, `locale`); `--save` keeps it in `.env.toml` (not `effort`) |
| `/status` | Version, workspace (local or in the service), session, agent, model (and why it can't be built), mode, sandbox, MCP servers, settings files |
| `/copy [n]` | Copy the last answer, or the nth from last, to the clipboard |
| `/permissions [allow\|ask\|deny\|remove <rule> [--save [--workspace]]]` | List rules (deny, ask, allow, with source; the built-in read-only ones on one line per effect, and "none" while there are no others) and change them for the session or, with `--save`, in the global settings (`--workspace`, which implies `--save`: the workspace's own) |
| `/effort [level\|auto]` | Show or set the session reasoning effort (MDL-74); `/set` shows it |
| `/mode [name]` | Show the permission modes with the current one marked, or switch (`bypass` refused without the OS sandbox) |
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

- **TUI-30** Queued attachments (and `@image` mentions; other `@paths`, which Tab completes, go to the agent as content, WS-21a) are collected before a non-aside turn; a failure aborts the turn. Once accepted, attachment summaries print and the queue clears; plan/btw modes print a dim note.
- **TUI-31** Ctrl+C, or Esc on its own (read by the steering key watcher), during a turn cancels only that turn ("interrupted"); during an approval prompt (raw mode) it also cancels the turn. A blocked prompt prints "✗ prompt blocked: <reason>".
- **TUI-32** Steering (macOS, Linux; TTY line editor): while a turn runs, typing or Ctrl+T opens a steer prompt pre-filled with what was typed; output is held back while typing and released afterwards (the spinner resumes only if it was showing). An empty message cancels. The message goes through `Backend.Steer` (hooks apply; a block is shown). Stopping the watcher at the end of the turn waits for an open steer prompt, so the message isn't lost.
- **TUI-33** Leftover steer messages (sent after the model's last tool call) are sent as the next turn, marked accepted; if the turn was interrupted they are dropped with a notice.

## 6. Exit

- **TUI-40** Ctrl+C or `/exit` at the prompt, or EOF: if background processes or tasks run, list them (`[id] command (Ns)`, `[task-3] agent: prompt (Ns)`) and ask **k**ill / **w**ait / **c**ancel (cancel only in the REPL); another Ctrl+C or EOF force-quits and kills them; waiting can itself be interrupted (force-quit). Without a way to ask, they are killed. SIGTERM exits without prompting (cleanup kills processes).
- **TUI-41** When attached to the service, the exit prompt covers the processes and tasks this client's turns started (CL-04); others' keep running.
- **TUI-43** `/cd [path]` (PAR-SES-40–43): without a path, the workspace; with one (relative to the workspace, `~` expanded), a directory that isn't the current one. Background processes and tasks are dealt with as at exit (kill, wait, or cancel the move); then the target opens as a workspace does at start — its settings and flags, the project trust question, attached to the service when it runs (`App.Cd`, from main) — and a target open elsewhere (`ErrWorkspaceBusy`) or any failure leaves the session where it was ("Stayed in …"). A session with messages moves (`MoveSession`), else the target starts a new one; then the old workspace closes (attached, it stays open in the service for other clients), and path completion follows. `/undo` with nothing to undo after a move says the earlier changes are in the old workspace.
- **TUI-42** `/tasks` lists the session's background tasks (`[task-3] qa · running · 1m20s · $0.02 — prompt`), `/tasks show <id>` prints one's latest events and result, `/tasks stop <id>` stops it. At the prompt, a dim line says which tasks have ended since, and tasks' approval requests and questions are asked there, labelled ("qa · task-3 asks, from the background:"), so a turn's own questions are never interrupted ([spec_background_agents_032](spec_background_agents_032.md)).

- **TUI-44** `/goal <condition>` sets the session's goal ([spec_sessions_017](spec_sessions_017.md) SES-19) and sends "Work toward this goal, and keep going until it holds: …" (the transcript records `/goal …`); `/goal` shows it with the continuations so far, `/goal clear` removes it.
- **TUI-46** (spec_parity_027 §8.3) `/status` (PAR-UI-05) prints the session's state. `/config key=value [--save]` (PAR-UI-07; `/set` is the same) changes a session setting; `--save` writes it through `config.SetValue` (`blitz.default_model`, `default_agent`, `permission_mode`, `agency_level`, `ui.style`, `ui.locale`) to the global `.env.toml`, and says when a key is for the session only. `/copy [n]` (PAR-UI-04) copies an answer (a model message that isn't a notice) with the first of `pbcopy`, `wl-copy` (Wayland), `xclip`, `xsel`, `clip.exe` that works, or OSC 52 when there is none or over SSH (`SSH_CONNECTION`/`SSH_TTY`, where the program would reach the remote machine's clipboard), and says which. `/context` (PAR-UI-06) adds a line per category (system prompt, instructions and notes, tool declarations, your messages, replies, tool calls, tool results, images, earlier summary) with tokens, percent and a bar. `/diff` (PAR-UI-10) with more than one file opens a picker of the changed files (`+added −removed`) and pages the chosen one through `$PAGER` (`less -R`), returning to the list until Esc; without a picker it prints the whole diff.
- **TUI-47** Notifications (PAR-UI-03): in a turn that has run longer than `ui.notify_after` seconds (30; 0 never), an approval or question that waits, and the turn's end (not an interrupted one), ring the bell and show a desktop notification (`osascript` on macOS, `notify-send` on Linux), as `ui.notify` says: `both`, `bell`, `desktop` or `off`; only on a terminal. The engine fires notification hooks with type `turn_finished` for such a turn in any front end.
- **TUI-48** Status line (PAR-UI-08): `ui.status_line` (none by default; terminal only) shows a dim line above each prompt. `default` is the built-in one: model · mode · context tokens (and percent of the compaction threshold) · cost when priced. Anything else is a shell command, run in the workspace with a 2 s limit, reading the session's state as JSON on stdin (`version`, `workspace`, `attached`, `session_id`, `session_name`, `agent`, `model`, `provider`, `permission_mode`, `effort`, `style`, `context_tokens`, `context_percent`, `cost_usd`, `priced`); its first line of output, control characters removed and cut to the terminal's width, is shown, or "status line: <error>" when it fails without output. It's above the prompt, not under it: the line editor clears the screen below the cursor as you type. A project's settings can't set it (it runs a command).
- **TUI-49** Editing and themes (PAR-UI-09). `ui.editor = "vim"` edits the prompt vi-style (the line editor's vim mode, starting in insert mode); Esc is then vim's, so Esc Esc neither clears the line nor opens `/rewind` (use `/rewind`), while Esc at an approval or in the steer prompt keeps its meaning. `ui.keybindings` (`~/.blitz/keybindings.toml`) rebinds `editor` (Ctrl+G) and `cycle_mode` (Shift+Tab) to `ctrl+<letter>`, `shift+tab` or `none`, and `[commands]` binds keys to slash commands, sent when pressed at an empty prompt (`"ctrl+t" = "/tasks"`). Ctrl+C, Ctrl+D, Enter (Ctrl+M, Ctrl+J) and Tab (Ctrl+I) can't be bound; a key the line editor uses is taken over; an unknown key, a command without `/`, or two bindings on one key is a startup warning and the defaults. `/theme [name] [--save]` lists or switches the Markdown style (`auto`, `dark`, `light`, `dracula`, `tokyo-night`, `pink`, `ascii`, `notty`) from the next answer; `ascii` and `notty` show diffs without colour; `--save` writes `ui.theme`.
- **TUI-45** `/loop <interval> <prompt>` (PAR-SES-31) sends the prompt again every interval (a Go duration or a number of minutes; at least 1m) while the REPL stays open, numbered from 1; `/loop` lists them with their next time, `/loop stop <n>` stops one. A loop due at the prompt interrupts it and runs at once (an unfinished line is dropped); one due during a turn runs when it ends; each run says "Loop n: …" and reschedules from then. Loops end with the REPL (durable schedules are workers); the desktop app has none.

## 7. Approver and questions
- **TUI-50** Approval prompt: tool detail, coloured diff truncated to `ui.diff_lines` (`d` shows all), answers `y`/`yes`, `s`/`session`, `a`/`always`, `n`; anything other than an explicit yes-type answer denies. The rememberable scope is described by the request's key label.
- **TUI-51** `ask_user_question`: shows the question and numbered options; a numeric answer selects that option.
