---
title: "020 · CLI"
weight: 20
---

*Command-line interface* (`spec_cli_020`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `apps/cli/main.go`, `exitcode.go`, `oneshot.go`, `setup.go`, `configcmd.go`, `doctor.go` |
| Tests | `apps/cli/main_test.go`, `images_test.go`, `locale_test.go` |
| Related | [spec_config_002](spec_config_002.md), [spec_workspace_018](spec_workspace_018.md), [spec_tui_019](spec_tui_019.md), [spec_service_021](spec_service_021.md), [spec_workers_023](spec_workers_023.md) |

## 1. Purpose

`blitz` (also `blz`, a link to it in the release archives) is a single static Go binary. With no prompt it starts an interactive REPL in a workspace; with a prompt it runs one turn and exits. Subcommands cover diagnostics, configuration, the per-user service and workers.

## 2. Command tree

| Command | Purpose |
|---|---|
| `blitz [flags] [prompt...]` | REPL, or a one-shot run when a prompt is given |
| `blitz exec [flags] <prompt...>` | Always a one-shot run; no prompt is a usage error |
| `blitz doctor [--online]` | Health checks (§7) |
| `blitz config init [--force] \| path \| show` | Configuration file management (§8) |
| `blitz serve [...]` | Hidden: runs `blitzd`, the per-user service, with the same arguments ([spec_service_021](spec_service_021.md) SVC-00) |
| `blitz service install \| uninstall \| status` | Login item for the service |
| `blitz workers [list] \| enable <name> [-y] \| disable <name> \| run <name> \| runs <name> [-n N]` | Workers ([spec_workers_023](spec_workers_023.md)) |
| `blitz license [full\|third-party]` | The NOTICE (with pointers to the rest), the Apache License, or the third-party notices, paged on a terminal; the REPL's `/license` shows the same ([spec_release_readiness_030](spec_release_readiness_030.md) RR-05) |
| `blitz completion bash\|zsh\|fish\|powershell` | Cobra-generated shell completion |

## 3. Flags

Persistent (all commands): `-c/--config DIR` (config directory, default `~/.blitz`), `-d/--dir DIR` (workspace; default the current directory).

Run flags (root and `exec`):

| Flag | Meaning |
|---|---|
| `-p/--prompt TEXT` | One-shot prompt; `-` reads stdin |
| `-a/--agent NAME` | Agent to activate |
| `-m/--model NAME` | Model (may be `provider/model`) |
| `--agency low\|medium\|high\|extreme` | Agency level (lower-cased) |
| `--trust-workspace` | Load agents/skills from the workspace |
| `-r/--resume [ID\|NAME]` | Resume a session by ID; bare `-r` means `latest`; a snapshot name starts a new session from it |
| `-C/--continue` | Continue this workspace's most recent non-snapshot session |
| `--output-format text\|json\|stream-json` | One-shot output format (default `text`) |
| `--max-turns N` | Cap model calls in a one-shot run (0 = unlimited) |
| `--max-cost-usd N` | Stop a one-shot run once it has cost more than N USD (0 = unlimited; needs a priced model, else a warning) |
| `--timeout D` | Stop a one-shot run after D (e.g. `15m`; 0 = unlimited) |
| `--plan` | One-shot plan-only run (read-only tools) |
| `--local` | Run in-process even if the service is running |
| `--allow RULE`, `--deny RULE` | Add permission rules for this run (repeatable; `read(…)` can't be given this way) |
| `--effort LEVEL` | Session reasoning effort (`minimal`…`max`; otherwise a usage error). Attached, it changes the service workspace's effort |
| `--permission-mode MODE` | Start in `default`, `accept-edits`, `plan`, `dont-ask` or `bypass` (needs the OS sandbox; otherwise a usage error). Attached, it changes the service workspace's mode |
| `--image PATH` | Attach an image to the first prompt (repeatable) |

Root only: `-i/--interactive` (REPL even with a prompt), `-v/--version` (prints `Blitz Go (Google ADK) version <v>`; version stamped into release builds from the git tag (`bazel build --config=release`), `dev` otherwise).

## 4. Requirements

### 4.1 Prompt resolution
- **CLI-01** If `-p -` is given, or no `-p` and stdin is not a TTY and `-i` is not set, the prompt is read from stdin (max 10 MiB) and trimmed.
- **CLI-02** Positional arguments frame piped input: `args + "\n\n" + piped` when both exist; args alone or piped alone otherwise.
- **CLI-03** Empty stdin when stdin was to be read is a usage error ("stdin was empty (use -i …)").
- **CLI-04** `-p TEXT` together with positional arguments is a usage error.
- **CLI-05** A run is one-shot iff a prompt exists and `-i` is not set.
- **CLI-06** `--plan`, a non-text `--output-format`, or any of `--max-turns`/`--max-cost-usd`/`--timeout` without a one-shot prompt is a usage error. Negative limits and an unknown `--output-format` are usage errors.
- **CLI-07** `exec` with no prompt from any source is a usage error.

### 4.2 Startup
- **CLI-10** `--dir` is expanded (`~`), made absolute, and must be an existing directory, else usage error. The process working directory is never changed; the workspace is passed explicitly (`cfg.Tools.WorkspaceDir`).
- **CLI-11** Flag overrides are applied after config load: `--model` → `blitz.default_model`, `--agent` → `blitz.default_agent`, `--agency` → `blitz.agency_level`, `--trust-workspace` → `blitz.trust_workspace`.
- **CLI-12** The default `slog` logger discards output until the diagnostic log is installed, so warnings are not duplicated on stderr.
- **CLI-13** Observability (log file, optional OTel) starts before the workspace opens and is flushed on exit; failures only disable the affected part and print a warning.
- **CLI-14** Backend selection: unless `--local`, if the service socket answers, the CLI attaches to the service ([spec_client_022](spec_client_022.md)); in the REPL it prints a dim "attached" line. Otherwise the workspace opens in-process. `ErrWorkspaceBusy` in-process maps to a usage error ("another blitz has it open").
- **CLI-15** If the configured model fails to initialise: one-shot runs fail with exit 1 and "(run 'blitz doctor')"; the REPL warns and runs on a placeholder model.
- **CLI-16** Input source: nothing interactive if stdin carried the prompt; the line editor on a TTY in REPL mode (falls back to a plain line reader with a warning); otherwise a line reader whose prompts go to stderr when output is JSON (stdout stays pure JSON). The input is wired as the approver and `ask_user_question` prompter.
- **CLI-17** `--image` paths are loaded through the workspace sandbox; a failure is a usage error. `@path` image mentions in a one-shot prompt are loaded too, with failures only warned (see [spec_images_011](spec_images_011.md)).
- **CLI-18** Session selection follows [spec_sessions_017](spec_sessions_017.md); a `ResumeError` is a usage error. When resuming in the REPL, a green line reports the session (or "branched from snapshot") and the last 3 messages are recapped; resuming a session whose workspace differs warns.
- **CLI-19** SIGTERM always cancels the run context; in one-shot mode SIGINT does too. The REPL handles Ctrl+C itself.

### 4.3 One-shot runs
- **CLI-20** `text`: a printer renders events (Markdown/spinner only when stdout is a TTY and enabled in `[ui]`); after the turn a dim usage line goes to stderr when pretty.
- **CLI-21** `stream-json`: after the prompt is accepted, emit `{"type":"session","session_id","model","agent"}`; then one line per event: `text` (non-thought; with `partial`, `author`), `tool_call` (`name`,`args`), `tool_result` (`name`,`result`); then the result object.
- **CLI-22** `json`: a single result object at the end, with tool calls collected (args and matching result by ID+name).
- **CLI-23** Result object: `type:"result"`, `session_id`, `result` (final model text), `is_error`, `error`, `exit_code`, `duration_ms`, `usage{input_tokens,cached_input_tokens,output_tokens,model_calls}`, `cost_usd` (only when priced and ≥1 call), `tool_calls`.
- **CLI-24** A `prompt_submit` hook refusal maps to exit 4; interruption (context cancelled, not max-turns) maps to 130.
- **CLI-25** Before exit, background processes are handled by `ConfirmExit`: prompt only in text mode on a TTY with an input and no cancellation; otherwise they are killed ([spec_shell_007](spec_shell_007.md)).

### 4.4 Exit codes (stable contract)

| Code | Meaning | Mapping |
|---|---|---|
| 0 | success | `nil` |
| 1 | runtime or model error | default |
| 2 | invalid flags/arguments | `withCode(exitUsage, …)`, cobra flag errors |
| 3 | a limit stopped the run (`--max-turns`, `--max-cost-usd`, `--timeout`) | `api.IsLimit(err)`: `ErrMaxTurns`, `ErrCostLimit`, `ErrTimeLimit` (also when attached: `MAX_TURNS`, `COST_LIMIT`, `TIME_LIMIT`) |
| 4 | prompt blocked by a `prompt_submit` hook | `*api.BlockedError` |
| 130 | interrupted | `errors.Is(err, context.Canceled)` |

- **CLI-30** Errors are printed once to stderr via the localized `repl.error` message; usage is silenced. Flag errors append "Run '<cmd> --help' for usage." `doctor`, `--help` and CLI errors are English by policy (see [spec_i18n_004](spec_i18n_004.md)).

## 5. Tab completion (REPL)
- **CLI-40** The completer registers every slash command and fixed sub-arguments (`skills list|show|search`, `session list|new|load|save`, `search web|session`, `envs prune|remove`, `memory show|reload|add`, `approvals revoke|clear`, `diff git`, `attach clear`, `undo --force`, `set agency=`).
- **CLI-41** Dynamic sources: agent names (`/agent`, `/pin_model`); pinned agents (`/unpin`); models in use plus models with settings (`/model_settings`); locale tags (`/locale`); the 20 newest workspace session IDs plus all snapshot names (`/resume`). `@path` completion is provided by the TUI completer.

## 6. Doctor
- **CLI-50** `doctor` prints one line per check: `✓` (ok), `!` (warn), `✗` (fail), name padded to 16. Exit 1 if any check failed.
- **CLI-51** Checks, in order: config file (exists; warn if group/other-readable); config parse; skills (policy problems, parse failures, scripts blocked by policy, script sandbox in use); credentials for the provider (Anthropic may rely on `ANTHROPIC_AUTH_TOKEN` or a login profile; Ollama needs none; otherwise a missing key fails; keys shown masked `abc…wxyz`); pricing for the active model; log level/dir; telemetry endpoint and content capture; the primary model, each fallback (warn, not fail) and each `[agent_models]` pin; `bash` (fail if missing) and `git` (warn); workspace and shell sandbox status; web search provider; each MCP server (command on PATH); hooks (first word resolvable); session dir permissions; audit log; project memory files found.
- **CLI-52** `--online` additionally sends `"Reply with the single word: ok"` (max 16 output tokens, 30 s timeout) to each model separately (never through the fallback chain), runs a test web search ("Go programming language", 3 results), and connects to each MCP server to count its tools (20 s timeout).

## 7. Config subcommands
- **CLI-60** `config init` writes a commented template to `<configdir>/.env.toml` with mode 0600 (dir 0700); refuses to overwrite without `--force` (usage error); the template is validated as TOML before writing.
- **CLI-61** `config path` prints the file location; `config show` prints the effective configuration as TOML with Gemini/OpenAI/Anthropic keys and every MCP `env` value masked.

## 8. Non-goals / known gaps
- A `blz refactor` command is proposed but not designed.
