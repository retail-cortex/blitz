---
title: "007 · Shell"
weight: 7
---

*Shell commands, command policy, OS sandbox, background processes* (`spec_shell_007`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/tools/shell.go`, `exec.go`, `cmdpolicy.go`, `ossandbox*.go`, `bwrap.go`, `background.go`, `procgroup_*.go`, `capped_buffer.go` |
| Tests | `pkg/engine/tools/shell_test.go`, `cmdpolicy_test.go`, `ossandbox_test.go`, `bwrap_test.go`, `guard_test.go` |
| Depends on | [spec_filetools_006](spec_filetools_006.md) (roots, blocked patterns), [spec_approvals_005](spec_approvals_005.md) |

## 1. Purpose

The agent runs shell commands through `run_shell_command`. Three layers apply: a parsed **command policy** (guardrail), **approval**, and the **OS sandbox** (the boundary). Every child process is in its own process group, has credentials scrubbed from its environment, and is guarded so it dies with Blitz — even on `SIGKILL`.

## 2. `run_shell_command`

Args: `command`, `cwd?` (inside the workspace), `timeout_seconds?`, `background?`. Result: `output, exit_code, duration_ms, is_background, process_id, truncated, error`.

- **SH-01** Empty command → error. `cwd` must resolve inside the sandbox roots and exist.
- **SH-02** Policy `deny` → "blocked by command policy: <reason>", nothing runs. `auto_approve` → runs without asking. Otherwise approval (kind `run_command`, key = exact command in this workspace, target = the command). The detail shows `(in <cwd>)`, `[background]`, and the policy's reason when it could not auto-approve.
- **SH-03** Runs `bash -c <command>` in the cwd, stdout+stderr interleaved into one buffer capped at 100 KiB (excess discarded while running, with a truncation notice that never splits a UTF-8 rune).
- **SH-04** Timeout: `tools.shell_timeout_seconds` (default 120 s), per-call override, capped at 30 min (clamped before conversion to avoid overflow). Timeout → exit −1 "command timed out after …"; cancellation → exit −1 "command cancelled"; otherwise the process exit code.
- **SH-05** After a kill, `Wait` waits at most 2 s for pipes held open by descendants.

## 3. Command policy

- **SH-10** The script is parsed as Bash (`mvdan.cc/sh`) and **every simple command** is checked: pipelines, lists, subshells, command substitutions, functions, `sh|bash|zsh|dash|ksh -c <literal>` (recursively, max depth 3), `find -exec/-execdir/-ok/-okdir`, and wrappers `env`, `nohup`, `command`, `builtin`, `exec`, `time`, `nice`, `timeout`, `xargs`, `stdbuf`, `sudo`, `doas`, `caffeinate` (option values skipped per wrapper; `env VAR=x` skipped; `timeout` duration skipped). Wrappers are checked against `deny` themselves but are otherwise transparent.
- **SH-11** Patterns match a simple command's words joined by single spaces, in three forms (`tools.CommandPatternForm`): words alone name a command with any arguments (`ls` matches `ls -la`, `git log` matches `git log --oneline` but not `git logs`); with `*` (any text) or `?` (one character) the pattern is a glob over the whole command, and a trailing ` *` also matches the bare command (`git *` matches `git`); `re:` starts an RE2 regular expression that must match the whole command (`re:git (log|show)( .*)?`; an invalid one is an error). A command given by path also matches by base name (`/bin/rm -rf x` matches `rm`). This applies to `sandbox.commands` and to `shell(…)` permission rules alike.
- **SH-11a** A redirection that writes a file (`>`, `>>`, `&>`, `&>>`, `>|`, `<>`, and `>&` to a name; not `/dev/null`, `/dev/stdout`, `/dev/stderr`, `/dev/tty` or a descriptor like `2>&1`), including in a subshell, a `bash -c` string or with a computed target, is not a command: no pattern matches it, and it keeps `auto_approve` and allow rules from approving the script (it needs approval, the reason naming the file).
- **SH-12** Words are literal only if fully static. Unquoted globs, brace expansion (`{r,}m`), `$'…'`/`$"…"`, parameter/command expansions are **dynamic**. Backslash escapes are removed (`s\udo` → `sudo`). A dynamic command name is unverified.
- **SH-13** `eval`, `source` and `.` are unverified (code can't be checked).
- **SH-14** Verdicts: any command matching `deny` → **deny** (always wins). With an `allow` list: a parse error, an unverified command, or a command not matching → deny. `auto_approve` applies only if every non-transparent command matches it and none uses runtime expansion or is unverified. Otherwise → needs approval. A parse error without an allow list → needs approval with the parse error as reason. A nil policy always needs approval.
- **SH-15** The policy is a guardrail, not a security boundary: an allowed interpreter can do anything.

## 4. OS sandbox (`sandbox.shell`)

- **SH-20** Modes: `auto` (sandbox if available, else run unsandboxed and report why), `required` (startup fails if unavailable), `off`. Unknown mode is a startup error.
- **SH-21** Writable dirs: writable file roots + defaults (OS temp dir; on macOS its per-user parent under `/var/folders`; `/tmp`; `/var/tmp`; the user cache dir) + existing `sandbox.shell_writable_paths`. Read-only roots stay read-only even inside writable dirs. Blocked paths are unreadable and unwritable. Network is off when `sandbox.allow_network = false`.
- **SH-22** macOS: Seatbelt via `/usr/bin/sandbox-exec -p <profile>`. Profile: `(allow default)`, `(deny file-write*)`, allow writes to writable subpaths and `/dev/null|zero|tty|dtracehelper`, `/dev/ttys*`, `/dev/fd/*`; deny writes to read-only dirs; deny read/write to blocked regexes (last, so they win); without network `(deny network*)` except unix sockets. The profile is probed once with `/usr/bin/true`.
- **SH-23** Linux: bubblewrap: `--die-with-parent --ro-bind / / --dev-bind /dev /dev`, `--bind` writable dirs, `--ro-bind` read-only dirs, blocked files masked with `/dev/null`, blocked dirs with an empty read-only tmpfs, `--unshare-net` without network. Probed once with `/bin/true` (needs unprivileged user namespaces; Ubuntu 24.04 AppArmor and Docker's seccomp profile block them).
- **SH-24** Because bwrap masks paths not patterns, blocked name patterns are expanded before **every** sandboxed command by scanning the writable and read-only roots (max 50 000 entries, depth 12, skipping `.git`, `node_modules`, `vendor`, `target`, `__pycache__`); absolute patterns are globbed. A file created by a command that matches a blocked pattern is visible to that same command (macOS blocks it immediately).
- **SH-24a** Each sandbox keeps its scan between commands (`blockedScan`): every directory walked is checked with one `lstat` (modification time, size, inode), and only those that changed are read and matched again; a directory read within 2 s of its modification time is read again next time (file systems keep coarse times). It walks in the same order and within the same limits as a full scan, so it masks the same existing paths (tested against the full walk). A command's start-up for a 50,000-entry workspace is about 1 ms after the first scan; CI's Linux job fails above 20 ms (`BenchmarkBlockedScan`), and the parallel-cap test runs with the sandbox on.
- **SH-25** Other platforms have no OS sandbox (`auto` runs unsandboxed; `required` fails).
- **SH-26** The same exec environment runs forged tools and stdio MCP servers ([spec_mcp_009](spec_mcp_009.md), [spec_agents_014](spec_agents_014.md)).

## 5. Process guard and environment

- **SH-30** Children start in their own process group (Unix); cancellation kills the whole group with `SIGKILL`, so grandchildren (`sleep &`) can't outlive a timeout or hold pipes open.
- **SH-31** Parent-death guard: every command is wrapped by `bash -c '( read -r -u 3 _ ; kill -KILL 0 ) … & exec 3<&- ; exec "$@"'` with a pipe's read end as fd 3; Blitz holds the write end. EOF — Blitz closing it after the command exits, or Blitz dying for any reason — kills the group. Not available on non-Unix platforms.
- **SH-32** Environment variables matching `sandbox.scrub_env` globs (case-insensitive) are removed from children. Commands run in the workspace root unless they set a directory.

## 6. Background processes

- **SH-40** `background: true` starts the command under the process manager and returns its ID immediately. Limits: 8 running at once ("too many background processes running"), lifetime 1 h, 256 KiB captured output, 16 finished processes retained.
- **SH-41** `manage_background_process` actions: `list` (id, command, running, exit_code, runtime_ms), `output` (captured output + status), `kill` (group kill, waits ~3 s; error if it doesn't exit).
- **SH-42** Shutdown kills all and refuses new ones. Front ends account for running processes at exit: the REPL/one-shot asks to kill or wait; a second Ctrl+C force-quits; non-interactive exits kill them ([spec_tui_019](spec_tui_019.md)).

## 7. User shell (`!cmd`)

- **SH-50** In the REPL, `!<command>` runs the user's own command in the workspace with the user's environment, outside the agent's sandbox, policy and approvals. The agent doesn't see it; the audit log records it (`user_shell`, exit code, start error).

## 8. Known gaps
