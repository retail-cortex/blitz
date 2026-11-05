---
aliases: ["/about/specs/spec_cli_020/"]
title: "020 · CLI"
weight: 20
---

*Command-line interface* (`spec_020_cli`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths checked 2026-09-27 (`//tools/specs`). |
| Source | `apps/cli/main.go`, `exitcode.go`, `oneshot.go`, `setup.go`, `configcmd.go`, `doctor.go` |
| Tests | `apps/cli/main_test.go`, `images_test.go`, `locale_test.go` |
| Related | [spec_002_config](spec_002_config.md), [spec_018_workspace](spec_018_workspace.md), [spec_019_tui](spec_019_tui.md), [spec_021_service](spec_021_service.md), [spec_023_workers](spec_023_workers.md) |

## 1. Purpose

`blitz` (also `blz`, a link to it in the release archives) is a single static Go binary. With no prompt it starts an interactive REPL in a workspace; with a prompt it runs one turn and exits. Subcommands cover diagnostics, configuration, the per-user service and jobs.

## 2. Command tree

| Command | Purpose |
|---|---|
| `blitz [flags] [prompt...]` | REPL, or a one-shot run when a prompt is given |
| `blitz exec [flags] <prompt...>` | Always a one-shot run; no prompt is a usage error |
| `blitz doctor [--online]` | Health checks (§7) |
| `blitz auth status`, `blitz auth login <google|anthropic|aws|azure> [--profile P]`, `blitz auth logout <provider>` | *Since 2026-10-04.* Each provider's sign-in, and signing in or out with its vendor's tool, run by the service (`GetSignIn`, `SignIn`, `SignOut`, [spec_021_service](spec_021_service.md) SVC-56): login prints the tool's lines, the sign-in page's URL ("If the browser didn't open: …") and a device code; Ctrl+C stops it; an unknown provider, a bad profile or a missing tool is a usage error. API keys keep working: signing in adds a way |
| `blitz enterprise connect <address>`, `disconnect`, `login [--browser]`, `logout`, `status`, `terms`, `about`, `workspaces`, `open <workspace>`, `create <name> [--personal] [--handle h]`, `share [folder] [--name n] [--handle h] [--personal]`, `sync [status]`, `jobs [runs\|push\|run\|on\|off\|files\|pending\|approve\|deny\|take\|watch …] [--workspace h]`, `usage`, `publish [<kind>/<name>…] [--lock] [--groups] [--title]`, `reviews [--state s]`, `catalog [--kind k] \| copy <kind>/<name>`, `notices [--email[=false]] [--webhook u] [--every-run[=false]]`; `blitz git-credential get\|store\|erase` | *Since 2026-11-01.* The organization's service and the sign-in to it, kept by the service (`GetEnterprise`, `ConnectEnterprise`, `EnterpriseSignIn`, `EnterpriseSignOut`, [spec_053_enterprise](spec_053_enterprise.md) EN-10 to EN-14): connect checks the address is a service (else a usage error); login shows an address and a code to enter in any browser, or opens one on this computer with `--browser`; status asks the service whether it answers. Disconnecting signs out and gives Blitz back the person's own providers. A version of the organization's terms waiting after login, or asked for with `terms`, is shown and asked `[y/N]`: no signs out (exit 1) |
| `blitz config init [--force] \| path \| show` | Configuration file management (§8) |
| `blitz serve [...]` | Hidden: runs `blitzd`, the per-user service, with the same arguments ([spec_021_service](spec_021_service.md) SVC-00) |
| `blitz service install \| uninstall \| restart \| status` | Login item for the service. *Since 2026-11-05 (scan N26-cli-2#2: the MCP, plugin and update hints named it, and it didn't exist)* `restart` restarts it: through the login item when there is one (waiting for it to answer), else the running service is asked to stop (`Shutdown`) and started again on demand; with none running it says the next one reads the change |
| `blitz jobs [list] \| enable <name> [-y] \| disable <name> \| run <name> \| stop <name\|run-id> \| runs <name> [-n N] \| undo <run-id> [-f] \| move <name> [--anyway]` | Jobs ([spec_023_workers](spec_023_workers.md)) |
| `blitz mcp add <name> <command\|url> [--env K=V] [--header K:V] [--prefix p] [--agents a,b] \| list \| get <name> \| remove \| enable \| disable \| login \| logout <name>` | MCP servers in the user's settings ([spec_009_mcp](spec_009_mcp.md) MCP-03, MCP-38) |
| `blitz license [full\|third-party]` | The NOTICE (with pointers to the rest), the Apache License, or the third-party notices, paged on a terminal; the REPL's `/license` shows the same ([spec_030_release_readiness](spec_030_release_readiness.md) RR-05) |
| `blitz init` | `/setup` unattended: the project's agent harness ([spec_019_tui](spec_019_tui.md)) |
| `blitz trust [dir]` | A workspace's project settings: show them, trust or decline ([spec_031_project_config](spec_031_project_config.md)) |
| `blitz worktrees list \| remove <name> \| prune` | The git worktrees `--worktree` made (CLI-12a) |
| `blitz memory list \| show \| edit \| forget <name>` | The agent's notes in the workspace ([spec_012_memory](spec_012_memory.md)) |
| `blitz plugin install \| update \| list \| show \| enable \| disable \| remove`, `blitz plugin marketplace add \| list \| remove`, `blitz plugin import claude\|gemini <dir>` | Plugins |
| `blitz skills registry add \| list \| remove`, `blitz skills search \| install \| update \| uninstall \| list` | Skills from registries |
| `blitz models [provider…]` | The providers' models, with known prices |
| `blitz update [--check]` | Install the latest release over this one |
| `blitz sessions export <id> [-o file]` | A session as Markdown, or Word for a `.docx` name ([spec_017_sessions](spec_017_sessions.md) SES-21) |
| `blitz agents [--all]`, `blitz attach <id>`, `blitz logs <id> [-f]`, `blitz stop <id>` | Background runs (`--bg`) in the service |
| `blitz security [fix-apparmor] \| scan-secrets [--staged] [file…] \| install-hook [dir]` | The OS sandbox's state, and the secrets checks |
| `blitz search <terms> [--in …] [--json] [--status] [--reindex]` | Workspace search |
| `blitz eval <name> \| --skill <skill>` | Run an eval |
| `blitz config set-key \| keys \| secure-key \| remove-key <provider>`, `blitz config set-auth <provider> <api_key\|adc\|oauth>`, `blitz config permissions [allow\|ask\|deny\|remove <rule>] \| check <rule> [sample] \| defaults on\|off\|inherit` | API keys, how a provider signs in, and the saved permission rules (`--workspace`: the workspace's own) |
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
| `--trust-project` | Trust the workspace's project settings for this run, without recording it ([spec_031_project_config](spec_031_project_config.md)); `--trust-workspace` is a hidden alias |
| `-r/--resume [ID\|NAME]` | Resume a session by ID; bare `-r` means `latest`; a snapshot name starts a new session from it |
| `-C/--continue` | Continue this workspace's most recent non-snapshot session |
| `--output-format text\|json\|stream-json` | One-shot output format (default `text`) |
| `--input-format text\|stream-json` | stream-json: stdin carries JSON lines driving the session (CLI-07); needs `--output-format stream-json` |
| `--json-schema FILE\|JSON` | The answer must be JSON valid against the schema (CLI-08) |
| `--no-session-persistence` | Keep nothing of the run's session (CLI-09) |
| `--fork` | With `--resume`/`--continue`: continue in a copy ([spec_017_sessions](spec_017_sessions.md) SES-20) |
| `--name TITLE` | Name the new session |
| `--append-system-prompt TEXT`, `--append-system-prompt-file PATH` | Add to the agent's instructions for the run, in a service of its own ([spec_012_memory](spec_012_memory.md) MEM-41) |
| `--max-turns N` | Cap model calls in a one-shot run (0 = unlimited) |
| `--max-cost-usd N` | Stop a one-shot run once it has cost more than N USD (0 = unlimited; needs a priced model, else a warning) |
| `--timeout D` | Stop a one-shot run after D (e.g. `15m`; 0 = unlimited) |
| `--plan` | One-shot plan-only run (read-only tools) |
| `--local` | Run in a service of the run's own (a private `blitzd`), not the shared one (CLI-14) |
| `--allow RULE`, `--deny RULE` | Add permission rules for this run (repeatable; `read(…)` can't be given this way) |
| `--effort LEVEL` | Session reasoning effort (`minimal`…`max`; otherwise a usage error). Attached, it changes the service workspace's effort |
| `--permission-mode MODE` | Start in `auto` (the default), `default`, `accept-edits`, `plan`, `dont-ask` or `bypass` (needs the OS sandbox; otherwise a usage error). Attached, it changes the service workspace's mode |
| `--bg` | Start the prompt as a background run in the service and return (`blitz agents`) |
| `-w/--worktree[=NAME]`, `--ref REF` | Start in a new git worktree (CLI-12a) |
| `--add-dir DIR`, `--plugin-dir DIR` | Another read-write directory, or a plugin, for this run, in a service of its own (CLI-11; repeatable) |
| `--image PATH` | Attach an image to the first prompt (repeatable) |

Root only: `-i/--interactive` (REPL even with a prompt), `-v/--version` (prints `Blitz Go (Google ADK) version <v>`; version stamped into release builds from the git tag (`bazel build --config=release`), `dev` otherwise).

*2026-11-05 (scan N26-cli-1#6).* The enterprise commands that work on a workspace (`share` without a folder, `sync`, `jobs push` and its "why not", `publish`, `catalog`) take it from the global `-d/--dir`, else the current directory; they no longer declare a `-d` of their own.

## 4. Requirements

### 4.1 Prompt resolution
- **CLI-01** If `-p -` is given, or no `-p` and stdin is not a TTY and `-i` is not set, the prompt is read from stdin (max 10 MiB) and trimmed.
- **CLI-02** Positional arguments frame piped input: `args + "\n\n" + piped` when both exist; args alone or piped alone otherwise.
- **CLI-03** Empty stdin when stdin was to be read is a usage error ("stdin was empty (use -i …)").
- **CLI-04** `-p TEXT` together with positional arguments is a usage error.
- **CLI-05** A run is one-shot iff a prompt exists and `-i` is not set.
- **CLI-05a** *2026-11-05 (scan N26-cli-2#4: the prompt was dropped).* With `-i` and a prompt, the REPL sends the prompt first, as if typed, once the banner is up (`App.FirstPrompt`), and the session stays open.
- **CLI-06** `--plan`, a non-text `--output-format`, or any of `--max-turns`/`--max-cost-usd`/`--timeout` without a one-shot prompt is a usage error. Negative limits and an unknown `--output-format` are usage errors.
- **CLI-07** `exec` with no prompt from any source is a usage error.

### 4.2 Startup
- **CLI-10** `--dir` is expanded (`~`), made absolute, and must be an existing directory, else usage error. The process working directory is never changed; the workspace is passed explicitly (`cfg.Tools.WorkspaceDir`).
- **CLI-12a** `--worktree[=name]` (`-w`; the name only after `=`, since a word after a space is the prompt; no name: `wt-<date>-<hex>`) with `--ref` starts the run in a new git worktree of the repository `--dir` (or the working directory) is in: `<repo>/.blitz/worktrees/<name>` on a new branch `blitz/<name>` from `--ref` (default `HEAD`), `.blitz/worktrees/` added to `info/exclude`, and the untracked files `.worktreeinclude` lists (glob patterns, `#` comments) copied in (`pkg/worktree`). *2026-11-05 (scan N26-pkg-small-2, spec gaps):* the patterns are Go's `filepath.Glob`, one directory level per `*` (no `**`, no `!` negation, not gitignore's rules as Claude Code's file of the same name has); regular files only, none outside the repository or under `.blitz/worktrees` (checked with slashes, so on Windows too). The workspace is then the worktree, with its own lock and checkpoints. `blitz worktrees list` (name, branch, path; "changes not committed" for tracked changes; "folder gone"), `remove <name> [--force]` (the branch stays) and `prune` (PAR-PAR-10, -12).
- **CLI-11** Flag overrides are applied after config load: `--model` → `blitz.default_model`, `--agent` → `blitz.default_agent`, `--agency` → `blitz.agency_level`. *Since 2026-10-04* they, `--trust-project`, `--plugin-dir`, `--add-dir`, `--append-system-prompt(-file)` and `--no-session-persistence` make the run one of its own (CLI-14): its private service opens the workspace with them (attached to the shared service they were ignored, and `--trust-project` only warned).
- **CLI-12** The default `slog` logger discards output until the diagnostic log is installed, so warnings are not duplicated on stderr.
- **CLI-13** Observability (log file, optional OTel) starts before the workspace opens and is flushed on exit; failures only disable the affected part and print a warning.
- **CLI-14** *Since 2026-10-04 (the owner's request: one engine, not two).* The CLI runs no workspace itself and doesn't link the engine (`//tools:check_deps`); every workspace is in a `blitzd` ([spec_022_client](spec_022_client.md)), which it finds beside itself, in its Bazel runfiles (`bazel run`), else on PATH. **The shared service**: it attaches when the socket answers; otherwise it starts the service (its login item when installed, else `blitzd --idle-exit 15m` detached, in the home folder, which stops after 15 minutes with no request, turn or background run) and waits up to 15 s for it. Clients starting it at once take turns through a lock file beside the socket (`<socket>.start`, created exclusively; one older than 30 s, its holder dead, is taken over): one starts it, the others find it answering. A started service's output goes to a temporary file, never a pipe to the CLI, which a service outliving it would die writing to (SIGPIPE); the REPL prints a dim "attached" line. **A run of its own** (`--local`, and the flags CLI-11 names): a private `blitzd` on a socket in a 0700 temporary folder, with `--exit-with-stdin` (it stops when the CLI exits, however it exits) and the run's settings as its hidden flags (the prompt in a file there, not on the command line, removed once the service has answered); a private service never runs the jobs' schedules; the CLI attaches to it and stops it and removes the folder at exit. When the shared service has the workspace open and no turn runs there, it's asked to close it (`CloseWorkspace`) so the private one can; otherwise `ErrWorkspaceBusy` is a usage error ("another blitz has it open"). `blitz trust`, `sessions export`, `memory`, `models`, `config permissions check` and `workers` go through the shared service too (`ForgetProjectTrust`, `ExportSession`, `ListNotes`/`ForgetNote`, `ListModels`, `CheckPermission`); the CLI exports no telemetry (the service does) and keeps only its diagnostic log.
- **CLI-14a** *2026-11-05 (scan N26-cli-behaviour, the owner's decision: the background-run commands refused with "start it with 'blitz service install'" while every other command that needs the service started it).* `blitz --bg`, `agents`, `logs`, `stop` and `attach` start the shared service when it isn't running, as above, instead of refusing.
- **CLI-15** If the configured model fails to initialise: one-shot runs fail with exit 1 and "(run 'blitz doctor')"; the REPL warns that turns fail until the model works (WS-05).
- **CLI-16** Input source: nothing interactive if stdin carried the prompt; the line editor on a TTY in REPL mode (falls back to a plain line reader with a warning); otherwise a line reader whose prompts go to stderr when output is JSON (stdout stays pure JSON). The input is wired as the approver and `ask_user_question` prompter.
- **CLI-17** `--image` paths are loaded through the workspace sandbox; a failure is a usage error. `@path` image mentions in a one-shot prompt are loaded too, with failures only warned (see [spec_011_images](spec_011_images.md)).
- **CLI-18** Session selection follows [spec_017_sessions](spec_017_sessions.md); a `ResumeError` is a usage error. When resuming in the REPL, a green line reports the session (or "branched from snapshot") and the last 3 messages are recapped; resuming a session whose workspace differs warns.
- **CLI-19** SIGTERM always cancels the run context; in one-shot mode SIGINT does too. The REPL handles Ctrl+C itself.

### 4.3 One-shot runs
- **CLI-20** `text`: a printer renders events (Markdown/spinner only when stdout is a TTY and enabled in `[ui]`); after the turn a dim usage line goes to stderr when pretty.
- **CLI-21** `stream-json`: after the prompt is accepted, emit `{"type":"session","session_id","model","agent"}`; then one line per event: `text` (non-thought; with `partial`, `author`), `tool_call` (`name`,`args`), `tool_result` (`name`,`result`); then the result object.
- **CLI-21a** *2026-11-05 (scan N26-cli-2 spec gap, the owner's decision: a guardrail stopping a turn with "Send continue…", the after-rules' and other notices, workflow steps and the task list never reached a stream-json reader).* Three more line types, new types only, the first three unchanged, every key always present: `notice` (`text`, `error` bool, `transient` bool: a passing status such as a retry, `author`); `workflow_step` (`workflow`, `id`, `title`, `step_type`, `index` and `count` from 1, `status`: `running`, `done`, `failed`, `skipped` or `waiting`, `output` (its start, ended), `error`, `run`); `tasks` (`tasks`: the agent's whole list each time it changes, `[{"content","status"}]` with `pending`, `in_progress` or `done`, `[]` when cleared; `author`). A reader skips a type it doesn't know.
- **CLI-22** `json`: a single result object at the end, with tool calls collected (args and matching result by ID+name).
- **CLI-23** Result object: `type:"result"`, `session_id`, `result` (final model text), `is_error`, `error`, `exit_code`, `duration_ms`, `usage{input_tokens,cached_input_tokens,output_tokens,model_calls}`, `cost_usd` (only when priced and ≥1 call), `tool_calls`.
- **CLI-24** A `prompt_submit` hook refusal maps to exit 4; interruption (context cancelled, not max-turns) maps to 130.
- **CLI-25** Before exit, background processes are handled by `ConfirmExit`: prompt only in text mode on a TTY with an input and no cancellation; otherwise they are killed ([spec_007_shell](spec_007_shell.md)).

### 4.4 Exit codes (stable contract)

| Code | Meaning | Mapping |
|---|---|---|
| 0 | success | `nil` |
| 1 | runtime or model error | default |
| 2 | invalid flags/arguments | `withCode(exitUsage, …)`, cobra flag errors |
| 3 | a limit stopped the run (`--max-turns`, `--max-cost-usd`, `--timeout`) | `api.IsLimit(err)`: `ErrMaxTurns`, `ErrCostLimit`, `ErrTimeLimit` (also when attached: `MAX_TURNS`, `COST_LIMIT`, `TIME_LIMIT`) |
| 4 | prompt blocked by a `prompt_submit` hook | `*api.BlockedError` |
| 130 | interrupted | `errors.Is(err, context.Canceled)` |

- **CLI-30** Errors are printed once to stderr via the localized `repl.error` message; usage is silenced. Flag errors append "Run '<cmd> --help' for usage." `doctor`, `--help` and CLI errors are English by policy (see [spec_004_i18n](spec_004_i18n.md)).
- **CLI-30a** *2026-11-05 (scan N26-cli-2, staticcheck ST1005).* The flag-error line is "Run '<cmd> --help' for usage", without the closing period.
- **CLI-30b** *2026-11-05 (scan N26-cli-2#18, cli-1#7: the owner chose to translate the subcommands' output, as the REPL's is).* Every subcommand prints what people read through the catalogs, so it follows `ui.locale` (`mcp`, `plugin`, `skills`, `jobs`, `memory`, `sessions`, `worktrees`, `models`, `update`, `service`, `config` keys and permissions, and the `enterprise` commands). What stays English, and why: errors and `--help` (one language to search and report them in), `doctor`'s report (pasted into support requests), `--version` (scripts parse it), and machine-readable output, `--json` and `--output-format json`/`stream-json` with its protocol errors, which programs read. A lint test (`TestNoUntranslatedOutput`) fails on a literal English string printed by any CLI source but `doctor.go` and `headless.go`.
- **CLI-62** *2026-10-13 (the owner's request).* `blitz --help` is grouped, each group sorted by name (`apps/cli/helpgroups.go`): commands in cobra groups (Runs and sessions, The workspace, Models and extensions, Setup and maintenance; none left under "Additional Commands"), and the run's flags, for `blitz` and `blitz exec`, in Session, Agent and model, One-shot run, Permissions and sandbox, Instructions and plugins, then Other flags. Other commands keep cobra's single Flags list. A test checks every visible command has a group and every grouped flag exists.

- **CLI-07** `--input-format stream-json` (PAR-CLI-03): a one-shot run (the prompt argument is optional) reads JSON lines from stdin: `{"type": "user", "text": …}` runs a turn (in order, one at a time, after the prompt argument's), `{"type": "approval", "id": …, "decision": "once|session|always|deny"}` answers an `approval_request` line (`id`, `tool`, `kind`, `detail`, `diff`, `targets`, `key_label`, `must_ask`), and `{"type": "answer", "id": …, "answer": …}` answers a `question` line (`id`, `question`, `options`). Each turn writes its events and its `result` line as stream-json output does. A line that isn't JSON, has an unknown type, or answers nothing waiting gets an `error` line; the run goes on. When stdin ends, the queued turns finish, requests still waiting are denied, background processes are stopped, and the run exits with the last turn's error code. `--image` and `blitz init`'s prompt apply to the first turn only.
- **CLI-07a** *2026-11-05 (scan N26-cli-1#5, #17).* The exit code is the last turn's, as written: a failed turn followed by one that succeeds exits 0 (it kept the first failure's). A `decision` other than `once`, `session`, `always` or `deny` (`yes` and `allow` are taken as `once`, `no` as `deny`) denies and gets an `error` line naming it, instead of denying without a word. A `question` line also carries `multi_select`; the answer to a multi-select question is the options chosen, one per line in one `answer` (as the terminal sends them).
- **CLI-08** `--json-schema` (PAR-CLI-02) takes a schema (draft 2020-12 or 07) inline or from a file; an invalid one is a usage error. The prompt sent (not the transcript's) asks for a single JSON value valid against it; the answer's JSON (all of it, a fenced block, or the outermost braces or brackets) is validated. If it fails, one more turn in the session gives the error and asks again. The valid value is `structured_result` in json and stream-json results; still invalid, the run fails (exit 1, the error in the result). Providers' native structured output isn't used: it would turn off tools for Gemini.
- **CLI-08a** *2026-11-05 (scan N26-cli-2#7: the retry had the whole budget again, so `--max-cost-usd 0.50 --timeout 15m` could spend $1.00 over 30 minutes).* The retry turn gets what's left of `--max-turns`, `--max-cost-usd` and `--timeout` after the first turn; a limit the first turn used up stops the run there (exit 3).
- **CLI-09** `--no-session-persistence` (PAR-CLI-04) keeps the run's sessions in a temporary folder removed at exit, and runs locally (not in the service); the audit log is written as usual. With `--resume` or `--continue`, or without a prompt, it's a usage error, as is `--json-schema` without one.

## 5. Tab completion (REPL)
- **CLI-40** The completer registers every slash command and fixed sub-arguments (`skills list|show|search`, `session list|new|load|save`, `search web|session`, `envs prune|remove`, `memory show|reload|add`, `approvals revoke|clear`, `diff git`, `attach clear`, `undo --force`, `set agency=`).
- **CLI-41** Dynamic sources: agent names (`/agent`, `/pin_model`); pinned agents (`/unpin`); models in use plus models with settings (`/model_settings`); locale tags (`/locale`); the 20 newest workspace session IDs plus all snapshot names (`/resume`). `@path` completion is provided by the TUI completer.

## 6. Doctor
- **CLI-49** *Since 2026-10-04* `blitz doctor` runs `blitzd doctor` with the run's `--config`, `--dir`, `--model`, `--agent`, `--agency`, `--plugin-dir`, `--add-dir` and `--online`, passing its output and exit code through: the checks build models, sandboxes and MCP clients, which only the service carries (`apps/service/internal/doctor`). It needs no running service; without `blitzd` it's a usage error.
- **CLI-50** `doctor` prints one line per check: `✓` (ok), `!` (warn), `✗` (fail), name padded to 16. Exit 1 if any check failed.
- **CLI-51** Checks, in order: config file (exists; warn if group/other-readable); config parse; skills (policy problems, parse failures, scripts blocked by policy, script sandbox in use); credentials for the provider (Gemini or Anthropic with `adc`: a project is required, and the credentials file of `GOOGLE_APPLICATION_CREDENTIALS` or gcloud's login is looked for, a warning without one; Anthropic with `oauth`: the profile's credentials must exist; otherwise Anthropic may rely on `ANTHROPIC_AUTH_TOKEN` or a login profile; Ollama needs none; otherwise a missing key fails; keys shown masked `abc…wxyz`); pricing for the active model; log level/dir; telemetry endpoint and content capture; the primary model, each fallback (warn, not fail) and each `[agent_models]` pin; `bash` (fail if missing) and `git` (warn); workspace and shell sandbox status; web search provider; each MCP server (command on PATH); hooks (first word resolvable); session dir permissions; audit log; project memory files found.
- **CLI-52** `--online` additionally sends `"Reply with the single word: ok"` (max 16 output tokens, 30 s timeout) to each model separately (never through the fallback chain), runs a test web search ("Go programming language", 3 results), and connects to each MCP server to count its tools (20 s timeout).

## 7. Config subcommands
- **CLI-60** `config init` writes a commented template to `<configdir>/.env.toml` with mode 0600 (dir 0700); refuses to overwrite without `--force` (usage error); the template is validated as TOML before writing.
- **CLI-61** `config path` prints the file location; `config show` prints the effective configuration as TOML with Gemini/OpenAI/Anthropic keys and every MCP `env` value masked.
- **CLI-61a** *2026-11-05 (scan N26-cli-1#3: Azure's and the web search key, and a gateway's `Authorization` header, were printed in full).* `config show` masks every credential the settings hold, from one list (`config.EachSecret`) the log redactor uses too: the providers' API keys (Azure's included), `web.search_api_key`, `llm.api.headers`, http hooks' headers, and MCP servers' `env` and `headers`. A test fails when a setting named like a key, token, secret or password is missing from it.

## 8. Non-goals / known gaps
- A `blz refactor` command is proposed but not designed.
