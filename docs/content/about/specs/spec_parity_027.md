---
title: "027 · Parity"
weight: 27
---

*Feature parity with Claude Code and Antigravity* (`spec_parity_027`)

| | |
|---|---|
| Status | **Partly implemented:** 56 of its 99 requirements done and 1 in part (counted 2026-09-30); the rest open. A gap analysis of 2026-09-26, to be closed before new features are added. |
| Compared with | **Claude Code** (current docs at code.claude.com, September 2026: v2.1.28x); **Antigravity CLI** (`agy` 1.1–1.2, September 2026); **Antigravity** 2.0 (Google's agent-first IDE and Agent Manager) |
| Depends on | Specs 001–025 (what Blitz does today) and [spec_backlog_026](spec_backlog_026.md) (Blitz's own gaps; overlapping items are referenced, not repeated) |

## 1. Purpose and method

This spec lists what the three reference products do that Blitz doesn't, written as Blitz requirements (`PAR-…`) so each gap can be built, tested and moved into its home spec. It is the work to finish **before** new features.

Method: every documented command, tool, flag, hook event, setting and surface of the references was compared with specs 001–025. A reference feature appears here only if Blitz lacks it or has a materially weaker form. Each item says which products have it (**CC** Claude Code, **AGY** Antigravity CLI, **AG** Antigravity IDE/Agent Manager) and what Blitz has today.

**Sources and confidence.** Claude Code features come from its official documentation index and reference pages (commands, tools, hooks, CLI, permission modes, interactive mode, memory, checkpointing). Antigravity's documentation site returned no usable content, so Antigravity features come from third-party guides (listed in §13). As the ROADMAP noted for an earlier review, some of those read as AI-generated, so treat AGY/AG-only details as approximate and confirm them against the product before building.

**Priorities.** **P0**: affects everyday use or safety parity; do first. **P1**: important for common workflows. **P2**: valuable, less common. Sizes as in 026 (S ≤ ½ day, M 1–2 days, L 3+ days).

**Not in scope** (§11): features tied to a vendor's accounts or cloud (claude.ai login, cloud sessions, mobile, Slack, Remote Control over claude.ai, marketplace directories run by the vendor), and IDE editing features that aren't agent features (tab completion). Where a local equivalent makes sense, it is listed instead.

**Decisions** (§12): five gaps conflicted with earlier Blitz decisions. The owner decided them on 2026-09-26; the resulting requirements are in the sections below, and §12 records what was decided and why.

## 2. Permissions and autonomy

### 2.1 Permission modes — P0, M
**CC** default / acceptEdits / plan / auto / dontAsk / bypassPermissions, cycled with Shift+Tab, `--permission-mode`. **AGY** default / accept-edits / plan (`--mode`, Shift+Tab); `toolPermission` request-review / proceed-in-sandbox / always-proceed / strict. **AG** Secure / Review-driven / Agent-driven / Custom. **Blitz today:** approvals per action, `blitz.auto_approve` and `tools.auto_approve_commands` booleans in config, plan mode per turn (`/plan`, `--plan`).
- **PAR-PERM-01** ✅ *Done 2026-09-26 (ROADMAP 25.4; now [spec_approvals_005](spec_approvals_005.md) APR-14).* Named session permission modes: `default` (ask, as today), `accept-edits` (file creates/edits/deletes inside writable roots run without asking; commands still follow rules), `plan` (the existing read-only tool set, for the whole session until switched), `dont-ask` (anything that would ask is denied — for CI and workers), `bypass` (PAR-PERM-05).
- **PAR-PERM-02** ✅ *Done (ROADMAP 25.4, 25.9; the desktop app 2026-09-27): `/mode`, `--permission-mode`, `[blitz] permission_mode`, `SetPermissionMode`, prompt tag. Shift+Tab done (ROADMAP 25.9); the desktop app's mode chip and run settings done 2026-09-27 ([spec_desktop_024](spec_desktop_024.md) DSK-77/85).* Switch with Shift+Tab in the REPL (the prompt shows the mode), `/mode <name>`, `--permission-mode` at start, `[blitz] permission_mode` as the default; `SetPermissionMode` in the API; the desktop app has a mode selector.
- **PAR-PERM-03** ✅ *Done (ROADMAP 25.4).* Hard limits apply in every mode: deny rules, blocked paths, the file sandbox and the OS sandbox.
- **PAR-PERM-05** ✅ *Done (ROADMAP 25.4); the red `bypass` prompt tag is in; Shift+Tab reaches it only when the REPL started in it (ROADMAP 25.9).* `bypass` mode (decision §12.1): nothing asks. It is entered only with `--permission-mode bypass` or `/mode bypass` — no one-word flag and not reachable by Shift+Tab unless started with `--permission-mode bypass` — and **refuses to start or switch unless the OS sandbox is active** for shell commands (`sandbox.shell` resolved to on). Deny rules, blocked paths, the file sandbox and the network setting still apply. The prompt shows `bypass` in red; every action is audited with decision `bypass`.
- **PAR-PERM-06** ✅ *`auto_approve` → bypass done (ROADMAP 25.4), reported by `doctor`. `tools.auto_approve_commands` stays a separate switch (it also covers commands the policy can't verify, which a `shell(*)` rule doesn't), documented as the broader form of `allow shell(*)`.* `blitz.auto_approve = true` becomes the configured default mode `bypass` (with the same sandbox requirement: without an active sandbox, startup warns and falls back to `default`); `tools.auto_approve_commands` becomes an `allow` rule `shell(*)`. `doctor` reports the translation.
- **PAR-PERM-04** ✅ *Done (ROADMAP 25.13; WS-45).* Leaving plan mode: the agent can end a plan with a request to execute it (tool `exit_plan_mode` with the plan); approving switches to `default` or `accept-edits` and continues in the same turn — see §4.3.

### 2.2 Unified permission rules — P0, M
**CC** `allow` / `ask` / `deny` rules with tool specifiers (`Bash(git *)`, `Edit(src/**)`, `Read(~/secrets/**)`, `WebFetch(domain:x)`, `mcp__server__tool`), parameter matching, `/permissions`, `--allowedTools`, `--disallowedTools`. **AGY** `command(git)`, `command(regex:…)`, `write_file(src/)`, `mcp(server/tool)`, `/permissions`. **Blitz today:** separate lists — `sandbox.commands.{allow,deny,auto_approve}`, `web.allow_domains`/`deny_domains`, MCP `auto_approve` per server, `blocked_paths`.
- **PAR-PERM-10** ✅ *Done 2026-09-26 (ROADMAP 25.5; now [spec_approvals_005](spec_approvals_005.md) APR-15). The existing keys are not translated into rules: they keep working alongside them.* `[permissions] allow`, `ask`, `deny` lists of rules `Tool(specifier)`: `shell(<command pattern>)` (the existing policy parser and pattern syntax), `read(<path glob>)`, `write(<path glob>)`, `delete(<path glob>)`, `web(<domain glob>)`, `search(<provider>)`, `mcp(<server>:<tool glob>)`, `skill(<name>)`, `agent(<name>)`, bare tool names. Precedence: deny > ask > allow > mode default. The existing keys keep working and are translated into rules (reported by `doctor`).
- **PAR-PERM-11** ✅ *Done (ROADMAP 25.5).* `--allow <rule>` / `--deny <rule>` (repeatable) for one run.
- **PAR-PERM-12** ✅ *Done (ROADMAP 25.5); remembered approvals stay in `/approvals`, which the listing points to.* `/permissions` lists effective rules with their source (config, session, saved), and adds or removes rules; saved "always" approvals become `allow` rules in the same view.

### 2.3 Auto mode (a reviewing model) — P1, L
**CC** `auto` mode: a classifier model reviews each action instead of the user, with configurable trusted repos/domains and hard deny rules. **AG** "Agent decides" review policy. **Blitz today:** none.
- **PAR-PERM-20** ✅ *Done 2026-09-30 ([spec_approvals_005](spec_approvals_005.md) APR-14a).* Mode `auto`: every action that would ask is sent with the request's detail, diff and recent context to a reviewer model (`[permissions.auto] model`, default a small fast model), which answers allow or deny with a reason. A deny is returned to the agent as the refusal reason. Actions matching `deny` rules never reach the reviewer; `ask` rules still ask the user.
- **PAR-PERM-21** ✅ *Done 2026-09-30 ([spec_approvals_005](spec_approvals_005.md) APR-14a).* `[permissions.auto] environment` describes trusted repositories, hosts and paths for the reviewer; the reviewer's decisions are audited (`auto-allow` / `auto-deny`) and priced in `/cost`.

### 2.4 Project configuration and workspace trust — P1, M
Decision §12.3. **CC** `.claude/settings.json` (shared) and `.claude/settings.local.json`; **AGY** `.agents/` configuration. **Blitz today:** configuration only from trusted locations (CFG-01); `--trust-workspace` loads agents and skills from the workspace.

The threat is real: Claude Code's project settings have had CVE-2025-59536 (hooks ran before the trust dialog), CVE-2026-21852 (a repository redirected the API base URL and received the user's key) and CVE-2026-40068 (a crafted repository reused another directory's trust). Blitz's rules are layered so that none of these can happen.
- **PAR-CFG-01** ✅ *Done 2026-09-29 ([spec_project_config_031](spec_project_config_031.md)).* A workspace may carry `.blitz/settings.toml` (shared, committed) and `.blitz/settings.local.toml` (personal, warned about if tracked by git). They are merged on top of the user's configuration with the restrictions below; user-level settings and command-line flags keep precedence where they conflict, except that project deny rules always add.
- **PAR-CFG-02** ✅ *Done 2026-09-29 ([spec_project_config_031](spec_project_config_031.md)).* Loaded **without asking** (they only inform or tighten): instruction files and rules (§3.1), skills and slash commands as prompt text (§7.1, not their scripts), agent definitions, and permission rules that only tighten (`deny`, `ask`), stricter `blocked_paths`, stricter `[skills.policy]` values, and lower limits.
- **PAR-CFG-03** ✅ *Done 2026-09-29 ([spec_project_config_031](spec_project_config_031.md)).* Loaded **only after the user trusts this exact content**: anything that runs code or loosens policy — hooks, MCP servers (stdio commands; HTTP servers too, since they receive data), plugins, skill scripts, `allow` rules, `shell_writable_paths`, workers' `[workers.policy]` loosening. On first open (and whenever this part of the configuration changes), the REPL and the desktop app show exactly these items and ask **trust / don't trust**; trust is recorded in `~/.blitz/trust.json` keyed by the canonical workspace path **and** the SHA-256 of the trust-relevant content (the model of workers, WK-30), so an edit, or the same content at another path, asks again. Nothing in this group loads or runs before the answer; one-shot runs without a terminal treat it as untrusted (`--trust-project` accepts it for that run).
- **PAR-CFG-04** ✅ *Done 2026-09-29 ([spec_project_config_031](spec_project_config_031.md)).* **Never** taken from project configuration, even when trusted: credentials and API keys, provider base URLs and endpoints, `api_key_command`, telemetry endpoints, `auto_approve`/`bypass`, `sandbox.shell = off`, `allow_network`/`allow_private` loosening, blocked-path removals, log and audit locations. Such keys are ignored with a warning naming the file and key.
- **PAR-CFG-05** ✅ *Done 2026-09-29 ([spec_project_config_031](spec_project_config_031.md)).* `--trust-workspace` and `blitz.trust_workspace` keep their meaning for `./agents` and `./skills` and are folded into this model; `blitz trust [--revoke] [dir]` and `/trust` show and change the decision; `doctor` lists project settings and what was ignored or is awaiting trust.
- **PAR-CFG-06** ✅ *Done 2026-09-29 ([spec_project_config_031](spec_project_config_031.md)).* Workspace trust is decided before any project file is executed or parsed beyond TOML decoding, and the trust check reads no repository-controlled indirection (e.g. git `commondir` or worktree pointers) to decide *which* path is being trusted.

## 3. Instructions and memory

### 3.1 Instruction-file compatibility, imports and scoped rules — P0, S
**CC** CLAUDE.md hierarchy (user, project, local), `@path` imports, `.claude/rules/*.md` with path globs, AGENTS.md support, `/init`. **AGY/AG** GEMINI.md and AGENTS.md, workspace rules in `.agents/rules/`, global rules. **Blitz today:** AGENTS.md and BLITZ.md from repo root to workspace, `~/.blitz/BLITZ.md` ([spec_memory_012](spec_memory_012.md)).
- **PAR-MEM-01** ✅ *Done 2026-09-26 (ROADMAP 25.3; now [spec_memory_012](spec_memory_012.md)).* `CLAUDE.md` and `GEMINI.md` are read like `AGENTS.md` (same trust: instructions only), in the order `memory.files` gives, de-duplicated by content, so a repository set up for another agent works unchanged. `CLAUDE.local.md` / `BLITZ.local.md` are personal files (warned about if tracked by git).
- **PAR-MEM-02** ✅ *Done 2026-09-26 (ROADMAP 25.3; now [spec_memory_012](spec_memory_012.md)).* `@path` lines in instruction files import other files (relative to the importing file, max depth 5, cycles and paths outside the repository refused, sizes counted in `max_bytes`).
- **PAR-MEM-03** ✅ *Done 2026-09-26 (ROADMAP 25.3; now [spec_memory_012](spec_memory_012.md)).* Rule files in `.blitz/rules/*.md` and `.agents/rules/*.md` (and `~/.blitz/rules/`) with optional frontmatter `paths: [globs]`: unscoped rules load always; scoped rules load when the agent first reads or edits a matching file in the turn.
- **PAR-MEM-04** ✅ *Done 2026-09-26 (ROADMAP 25.3; now [spec_memory_012](spec_memory_012.md)).* `/init` (and `blitz init`) asks the agent to study the repository and write `BLITZ.md` (build, test and lint commands, layout, conventions), showing the diff for approval; it updates an existing file instead of replacing it.

### 3.2 Auto memory / knowledge — P1, M
**CC** auto memory: notes Claude writes from corrections and preferences, loaded each session, reviewable with `/memory`. **AG** Knowledge items learned from past work. **Blitz today:** `/memory add` by the user only.
- **PAR-MEM-10** ✅ *Done 2026-09-30 ([spec_memory_012](spec_memory_012.md) MEM-30..33). Notes load when a session starts or `/memory reload` runs, not in the turn that saved them (the agent knows what it just said), and the files are named by date rather than indexed: the newest go in first, up to 8,000 bytes.* A `remember` tool lets the agent save a short note (fact, preference or correction) to `~/.blitz/memory/<workspace-hash>/` as one Markdown file per note, with an index loaded into instructions (capped); `[memory] auto = true|false` (default on), notes never grant permissions, and secrets are redacted before saving.
- **PAR-MEM-11** ✅ *Done 2026-09-30 ([spec_memory_012](spec_memory_012.md) MEM-34). `/memory notes` lists them in full and `/memory forget` deletes one (in the REPL and through `WorkspaceService.ListNotes`/`ForgetNote`); editing is `blitz memory edit`, since the file may be on the service's machine.* `/memory` lists, shows, edits (opens `$EDITOR`) and deletes notes; `blitz memory` does the same from the shell.

### 3.3 System prompt and output styles — P2, S
**CC** `--append-system-prompt[-file]`, `--system-prompt`, output styles (`/output-style`: default, Concise, Explanatory, custom files). **Blitz today:** fixed agent prompts plus memory.
- **PAR-MEM-20** `--append-system-prompt TEXT` / `--append-system-prompt-file PATH` for one run.
- **PAR-MEM-21** Output styles: Markdown files in `~/.blitz/styles/` (and built-ins `default`, `concise`, `explanatory`) appended to the active agent's instructions; `/style <name>`, `[ui] style`.

## 4. Sessions, checkpoints and planning

### 4.1 Rewind to any checkpoint — P0, M
**CC** `/rewind` (or Esc Esc): pick any earlier prompt, then restore code and conversation, conversation only, or code only, or summarize from/up to there; checkpoints persist with the session (100 kept, ~30 days). **AGY** `/rewind`. **Blitz today:** `/undo` of the latest turn with file changes, checkpoints in memory only ([spec_filetools_006](spec_filetools_006.md) FS-60–66).
- **PAR-SES-01** ✅ *Done 2026-09-26 (ROADMAP 25.11; now [spec_filetools_006](spec_filetools_006.md) FS-67).* Checkpoints are persisted with the session (snapshots content-addressed under `~/.blitz/checkpoints/`, owner-only; the same retention and size budget as today, then a configurable age), so `/undo` and rewind work after `--resume`.
- **PAR-SES-02** ✅ *Done 2026-09-26 (ROADMAP 25.12; now [spec_sessions_017](spec_sessions_017.md) SES-45–47, [spec_tui_019](spec_tui_019.md) TUI-17).* `/rewind` lists every prompt of the session; for the chosen one: **code and conversation**, **conversation only** (truncates both the transcript and the ADK event log to before that prompt, keeping files), **code only** (restores files to their state before that prompt, with the same conflict rules as `/undo`), **summarize from here** / **summarize up to here** (targeted compaction). After rewinding the conversation, the chosen prompt is put back in the input line. Esc Esc on an empty prompt opens it.
- **PAR-SES-03** ✅ *Done (SES-45).* Messages that steered a running turn aren't rewind points (as in CC); rewinding to the turn's prompt removes them.

### 4.2 Branching, export and naming — P1, S
**CC** `/branch`, `--fork-session`, `/export`, `--name`, `/resume` picker with search, `--from-pr`. **AGY** `/fork`, `/export`. **Blitz today:** snapshots, `/rename`, `/resume <id|name>`.
- **PAR-SES-10** `/fork [n]` — see [spec_backlog_026](spec_backlog_026.md) BL-SES-01; also `--fork` with `--resume`/`--continue` to continue in a copy.
- **PAR-SES-11** `/export [file]` writes the transcript as Markdown (prompts, replies, tool calls with summarised arguments and results, timestamps), secrets redacted; `blitz sessions export <id>` from the shell.
- **PAR-SES-12** `--name <title>` names a new session; `/resume` with no argument opens an interactive picker (arrow keys, type-to-filter over titles and first prompts) listing this workspace's sessions.

### 4.3 Plans and task lists as reviewable artifacts — P0, M
**CC** `EnterPlanMode`/`ExitPlanMode` (the agent presents a plan for approval, then executes), `TodoWrite`/task list shown live. **AG/AGY** Task List, Implementation Plan and Walkthrough artifacts, commentable mid-run; artifact review policy. **Blitz today:** `/plan` returns a plan as text and stops.
- **PAR-SES-20** ✅ *Done 2026-09-26 (ROADMAP 25.13; [spec_approvals_005](spec_approvals_005.md) APR-43) for the REPL and the API, and the desktop app's task card (2026-09-27, [spec_desktop_024](spec_desktop_024.md) DSK-76).* A `todo` tool keeps the turn's task list (items with status pending / in progress / done); the REPL shows it as a live checklist, the desktop app as a panel, the API as `TurnEvent.tasks`.
- **PAR-SES-21** ✅ *Done (ROADMAP 25.13; APR-41, WS-45). "Revise" is typing feedback at the review.* In plan mode the agent ends with `exit_plan_mode(plan)`; the user sees the plan and chooses **execute** (switches to `default` or `accept-edits` and continues), **revise** (types feedback; the agent revises the plan), or **keep planning**. The approved plan is saved as `.blitz/plans/<session>-<n>.md` and can be referenced later.
- **PAR-SES-22** ✅ *Done (ROADMAP 25.13; WS-45, APR-42); `always` plans every prompt (Blitz can't judge "non-trivial"), default `agent-decides`.* Artifact review policy `[blitz] plan_review = always | agent-decides | never` decides whether non-trivial prompts first produce a plan for approval.
- **PAR-SES-23** ✅ *Done 2026-09-27 ([spec_desktop_024](spec_desktop_024.md) DSK-90): the desktop app's Changes view shows the agent's latest summary (the agents end with what changed and how it was verified) above the diff, with the turns that changed files.* At the end of a turn that changed files, the agent can write a walkthrough (what changed, how it was verified); the desktop app shows it with the diff.

### 4.4 Goals and in-session loops — P1, S
**CC** `/goal <condition>` (keep working until a condition holds, judged by a model), `/loop [interval] <prompt>` and in-session cron tools. **AGY** `/goal`, `/schedule`. **Blitz today:** agency levels; scheduled workers ([spec_workers_023](spec_workers_023.md)).
- **PAR-SES-30** `/goal <condition>`: after each turn, a judge call checks the condition against the transcript; if unmet and not judged impossible, the agent continues automatically (bounded by `max_turns`/cost limits shown in the status); `/goal clear` stops.
- **PAR-SES-31** `/loop <interval> <prompt>` re-runs a prompt in the session while it stays open (minimum 1 minute), `/loop` lists and `/loop stop <n>` stops; loops die with the session. Durable schedules remain workers.

### 4.5 `/cd` — P1, M
Decision §12.2. **CC** `/cd <path>` (June 2026) moves the session to another directory and asks to trust a new one. **Blitz today:** rejected; `blitz -d <dir>` at start.
- **PAR-SES-40** ✅ *Done 2026-09-30 ([spec_tui_019](spec_tui_019.md) TUI-43, [spec_sessions_017](spec_sessions_017.md) SES-18).* `/cd <path>` (and `CdSession` in the API) closes the current workspace and opens the target from scratch through `engine.Open` — new workspace lock, file roots, blocked paths, OS sandbox profile, memory, MCP servers and project configuration (§2.4, including its trust prompt) — then moves the active session to the new workspace so `--continue` there finds it. Nothing of the old boundary is reused or patched.
- **PAR-SES-41** ✅ *Done 2026-09-30 ([spec_tui_019](spec_tui_019.md) TUI-43, [spec_sessions_017](spec_sessions_017.md) SES-18).* Before leaving, running background processes are handled as at exit (TUI-40: kill, wait or cancel the move); a target already open elsewhere (`ErrWorkspaceBusy`) refuses the move and leaves the session where it was.
- **PAR-SES-42** ✅ *Done 2026-09-30 ([spec_tui_019](spec_tui_019.md) TUI-43, [spec_sessions_017](spec_sessions_017.md) SES-18).* Checkpoints taken in the old workspace can't be undone or rewound from the new one (`/undo` says to `/cd` back); the new workspace's instruction files are added to the conversation as a message rather than rebuilding earlier context.
- **PAR-SES-43** ✅ *Done 2026-09-30 ([spec_tui_019](spec_tui_019.md) TUI-43, [spec_sessions_017](spec_sessions_017.md) SES-18).* Attached to the service, `/cd` switches the client to the target workspace in the service; the old workspace stays open there for other clients.

## 5. Tools

### 5.1 Missing built-in tools — P0/P1
**CC** `Glob`, `NotebookEdit`, `LSP`, `Monitor`, `WebSearch`, `TodoWrite`, `AskUserQuestion` (multiple-choice). **Blitz today:** `list_files`, `grep`, file tools, shell, background processes, web, `ask_user_question` (free text + options).
- **PAR-TOOL-01 (P0, S)** ✅ *Done 2026-09-26 (ROADMAP 25.1; now [spec_filetools_006](spec_filetools_006.md)).* `glob(pattern, path?)`: files matching `**` globs under the roots, blocked paths excluded, sorted by modification time, capped (default 200), with the same skip rules as `grep`.
- **PAR-TOOL-02 (P1, S)** `notebook_edit(path, cell, action, source)`: replace, insert or delete a Jupyter cell (JSON-preserving), through the file sandbox, approvals, diffs and checkpoints; `read_file` renders notebooks as cells with outputs.
- **PAR-TOOL-03 (P1, L)** Code intelligence: an `lsp` tool (definition, references, hover, workspace symbols, diagnostics) backed by language servers configured under `[lsp.<language>]` (e.g. `gopls`, `typescript-language-server`, `pyright`), started lazily in the OS sandbox; after each edit, new diagnostics for the edited file are appended to the tool result.
- **PAR-TOOL-04 (P1, S)** Background output reaches the agent: `run_shell_command(background: true, notify: true)` delivers the process's exit (and optionally lines matching a pattern) to the agent as a steer-style message at its next tool result or as a new turn when idle (CC `Monitor`).
- **PAR-TOOL-05 (P2, S)** `ask_user_question` supports multiple questions per call and multi-select options, rendered as a menu in the REPL and the app.

### 5.2 Browser automation — P1, L
**CC** Chrome integration (navigate, click, fill, read console, screenshots). **AG** browser subagent in a managed Chrome with recordings and screenshots, JavaScript execution policy, URL allowlist. **AGY** `/browser`. **Blitz today:** `web_fetch` (text only, no JavaScript).
- **PAR-TOOL-10** A `browser` tool set (open, navigate, click, type, select, read page text/DOM, run script, screenshot, read console) driving a headless or visible Chromium through the Chrome DevTools Protocol, in its own profile, never the user's.
- **PAR-TOOL-11** Every navigation goes through the web rules (approval per host unless allowed, SSRF checks, `deny_domains`; `localhost` allowed only with `[browser] allow_local = true`, for testing the user's own app); running page scripts needs its own approval unless allowed.
- **PAR-TOOL-12** Screenshots are images the model sees (via the image store); a session can record a short video or screenshot series as a walkthrough artifact.

### 5.3 Headless and scripting — P0/P1
**CC** `--output-format stream-json` plus `--input-format stream-json`, `--json-schema`, `--max-budget-usd`, `--include-partial-messages`, `--permission-prompt-tool`, `--no-session-persistence`. **AGY** `--json-schema`, `--print-timeout`. **Blitz today:** text / json / stream-json output, `--max-turns`, `--plan`.
- **PAR-CLI-01 (P0, S)** ✅ *Done 2026-09-26 (ROADMAP 25.2; now [spec_cli_020](spec_cli_020.md) CLI-06 and [spec_workspace_018](spec_workspace_018.md) WS-29).* `--max-cost-usd N` stops a one-shot run when its cost passes N (exit 3, like `--max-turns`), and `--timeout <duration>` stops it after a wall-clock limit (exit 3).
- **PAR-CLI-02 (P1, M)** `--json-schema <file|json>`: the final answer must be JSON valid against the schema; the model is asked for it (native structured output where the provider supports it), the result is validated and retried once with the validation error, and it appears as `structured_result` in json output (exit 1 if still invalid).
- **PAR-CLI-03 (P1, M)** `--input-format stream-json`: stdin carries a stream of user messages, approval answers and question answers as JSON lines, so another program can drive a multi-turn session and answer approvals without a terminal.
- **PAR-CLI-04 (P2, S)** `--no-session-persistence` runs without saving the session or the audit context (the audit log itself is still written).

### 5.4 MCP completeness — P1, M
**CC** `claude mcp add/list/remove/get`, project `.mcp.json`, `--mcp-config`, OAuth (`claude mcp login`), resources (`ListMcpResources`, `ReadMcpResource`), prompts as slash commands, elicitation, tool search for large tool sets, SSE and HTTP transports. **AGY** `agy mcp add/remove/list/enable/disable`, `.agents/mcp_config.json`. **Blitz today:** stdio and streamable-HTTP servers from `[[mcp.servers]]` in config ([spec_mcp_009](spec_mcp_009.md)).
- **PAR-MCP-01** ✅ *Done 2026-09-30 ([spec_mcp_009](spec_mcp_009.md) MCP-03–04, MCP-34–38).* `blitz mcp add <name> <command…|url> [--env K=V] [--header K:V] [--prefix p] [--agents a,b]`, `list`, `get`, `remove`, `enable`, `disable`, editing `~/.blitz/.env.toml` in place (CFG-20).
- **PAR-MCP-02** ✅ *Done 2026-09-30 ([spec_mcp_009](spec_mcp_009.md) MCP-03–04, MCP-34–38).* Resources: tools `list_mcp_resources(server?)` and `read_mcp_resource(server, uri)` (read-only, allowed in plan mode, subject to the server's approval rules), and `@server:uri` mentions in prompts.
- **PAR-MCP-03** ✅ *Done 2026-09-30 ([spec_mcp_009](spec_mcp_009.md) MCP-03–04, MCP-34–38).* Prompts: each server prompt is a slash command `/mcp__<server>__<prompt> [args]`, completed in the REPL.
- **PAR-MCP-04** ✅ *Done 2026-09-30 ([spec_mcp_009](spec_mcp_009.md) MCP-03–04, MCP-34–38).* OAuth for HTTP servers: `blitz mcp login <name>` runs the authorization-code flow with PKCE via a local callback (or a pasted code over SSH), storing tokens in the OS keychain (file fallback, owner-only); tokens refresh automatically and are never shown to the model.
- **PAR-MCP-05** ✅ *Done 2026-09-30 ([spec_mcp_009](spec_mcp_009.md) MCP-03–04, MCP-34–38).* Elicitation: a server's request for user input during a tool call is shown like `ask_user_question` (refused in unattended runs).
- **PAR-MCP-06** ✅ *Done 2026-09-30 ([spec_mcp_009](spec_mcp_009.md) MCP-03–04, MCP-34–38).* Workspace MCP configuration (`.mcp.json` / `.agents/mcp_config.json`) follows the project-configuration trust rules (§2.4): servers that start a command need the workspace trusted.

## 6. Hooks

**CC** 30+ events, five handler types (command, http, mcp_tool, prompt, agent), matchers, JSON decisions (`permissionDecision`, `updatedInput`, `additionalContext`, `continue`), `if` rule filters, `/hooks`. **AGY** lifecycle hooks (before tool call, after file edit, session start). **Blitz today:** `pre_tool`, `post_tool`, `prompt_submit` command hooks; block by exit 2 or `{"decision":"block"}` ([spec_hooks_010](spec_hooks_010.md)).

### 6.1 Events — P0, M
- **PAR-HK-01** ✅ *Done 2026-09-26 (ROADMAP 25.7; now [spec_hooks_010](spec_hooks_010.md) HK-50–53), except `config_change` (Blitz doesn't watch its config) and hooks on automatic compaction (no hook point in the ADK).* Add events: `session_start` (startup, resume, new, compact), `session_end`, `stop` (the agent finished; may return `{"continue": true, "reason": …}` to make it keep going, bounded by limits), `post_tool_failure`, `subagent_start` / `subagent_stop`, `pre_compact` / `post_compact`, `notification` (approval waiting, turn finished, idle), `permission_request` (may answer allow/deny for the user), `config_change`.
- **PAR-HK-02** ✅ *Done (ROADMAP 25.7).* Hook input gains `prompt_id`, `transcript_path`, `permission_mode`, `agent`, `cwd`; `session_start` and `prompt_submit` stdout (plain text) is added to the agent's context.

### 6.2 Decisions and handler types — P1, M
- **PAR-HK-10** JSON output: `decision` (`allow`/`deny`/`ask` for `pre_tool` and `permission_request`), `updated_args` (`pre_tool` may rewrite a tool's arguments, re-validated by the sandbox), `additional_context` (text given to the agent), `system_message` (shown to the user). A hook allow never overrides deny rules or the sandbox.
- **PAR-HK-11** Handler types besides `command`: `http` (POST the event, same response format; headers with `$VAR` interpolation from an allowlist) and `prompt` (a model judges the event against a prompt and returns the decision).
- **PAR-HK-12** `if = "<permission rule>"` filters tool hooks by arguments (e.g. `shell(git push *)`); `args = [...]` runs without a shell.
- **PAR-HK-13** `/hooks` lists configured hooks by event with their source and recent failures.

## 7. Skills, commands and plugins

### 7.1 Skills and custom commands as slash commands — P0, S
**CC** every user-invocable skill is `/<name> [args]` (`$ARGUMENTS`), custom commands, bundled skills (`/code-review`, `/security-review`, `/simplify`, `/verify`, `/batch`, `/loop`, `/doctor`). **AGY** skills in `.agents/skills/` become slash commands; **AG** workflows (saved prompts invoked with `/`). **Blitz today:** skills load through `activate_skill` only.
- **PAR-SK-01** ✅ *Done 2026-09-26 (ROADMAP 25.6; now [spec_skills_013](spec_skills_013.md) SK-80–82).* Each skill (and each Markdown file in `.blitz/commands/`, `.agents/workflows/`, `~/.blitz/commands/`, trusted as skills are) is a slash command `/<name> [args]`; its body (with `$ARGUMENTS`, `$1`…) becomes the prompt, and frontmatter may set `description`, `argument-hint`, `agent`, `model`, `allowed-tools` (a narrower tool set for that turn) and `mode` (e.g. `plan`). Built-in commands win name clashes. Tab completion lists them.
- **PAR-SK-02** ✅ *Done (ROADMAP 25.6); `/init` came with ROADMAP 25.3.* Bundled commands: `/review` (review the working-tree diff or a branch for bugs, read-only), `/security-review`, `/simplify`, `/verify` (run the project's tests and linters and report), `/init` (§3.1).

### 7.2 Plugins — P1, L
**CC** plugins bundling skills, agents, hooks, MCP servers, output styles; marketplaces (`marketplace.json`), `/plugin install|list|enable|disable`, `--plugin-dir`, dependencies, trust review. **AGY** plugins, `agy plugin import gemini`. **Blitz today:** none.
- **PAR-PLG-01** A plugin is a directory (or `.zip`) with `plugin.toml` (name, version, description, components) holding any of `skills/`, `commands/`, `agents/`, `hooks.toml`, `mcp.toml`, `styles/`. Installed under `~/.blitz/plugins/<name>/<version>`, pinned by content hash (the worker/skill model), enabled per user or per workspace.
- **PAR-PLG-02** `blitz plugin install <path|url|git>`, `list`, `enable`, `disable`, `remove`, `update`; installing shows exactly what the plugin adds (hooks and MCP commands run code) and needs confirmation; `--plugin-dir` loads one for a run.
- **PAR-PLG-03** Marketplaces: a `marketplace.toml` index (URL or git repo) listing plugins with versions and hashes; `blitz plugin marketplace add <url>`.
- **PAR-PLG-04** Importers: `blitz plugin import claude <dir>` and `import gemini` convert Claude Code / Gemini CLI configuration (commands, skills, agents, hooks, MCP servers) where the formats map, reporting what didn't.

## 8. Parallel work

### 8.1 Background sub-agents and task view — P1, M
**CC** subagents run in the background by default, `/subtask`, `/tasks`, subagents with their own model/tools/permission mode. **AGY** parallel subagents with independent approval gates, Ctrl+J to jump to a pending one. **Blitz today:** `invoke_agent` runs a sub-agent synchronously inside the tool call (depth ≤ 3).
- **PAR-PAR-01** ✅ *Done 2026-09-29 ([spec_background_agents_032](spec_background_agents_032.md)).* `invoke_agent(..., background: true)` returns a task ID immediately; the sub-agent runs concurrently in its own session; its result reaches the parent at its next tool result (or as a new turn when idle). `list_tasks` / `task_output` / `stop_task` tools, and `/tasks` in the REPL (status, cost, pending approvals).
- **PAR-PAR-02** ✅ *Done 2026-09-29 ([spec_background_agents_032](spec_background_agents_032.md)).* Sub-agents' approval requests are labelled with the sub-agent and can be answered while the parent keeps working (Ctrl+J jumps to the next pending one).
- **PAR-PAR-03** ✅ *Done 2026-09-29 ([spec_background_agents_032](spec_background_agents_032.md)).* Agent frontmatter gains `permission_mode`, `max_turns` and `background` defaults.

### 8.2 Worktree isolation — P1, M
**CC** `--worktree`, `EnterWorktree`/`ExitWorktree`, subagent isolation, `.worktreeinclude`. **AG** separate workspaces per parallel agent. **Blitz today:** none.
- **PAR-PAR-10** ✅ *Done 2026-09-30 ([spec_cli_020](spec_cli_020.md) CLI-12a, [spec_background_agents_032](spec_background_agents_032.md) BGA-08).* `--worktree [name]` starts the session in a new git worktree (`<repo>/.blitz/worktrees/<name>`, branch `blitz/<name>`, from HEAD or `--ref`), with files listed in `.worktreeinclude` (e.g. untracked `.env` examples) copied in; the workspace lock and checkpoints are per worktree.
- **PAR-PAR-11** ✅ *Done 2026-09-30 ([spec_cli_020](spec_cli_020.md) CLI-12a, [spec_background_agents_032](spec_background_agents_032.md) BGA-08).* Background sub-agents may run in their own worktree (`isolation: worktree`), and report their branch for the user to merge.
- **PAR-PAR-12** ✅ *Done 2026-09-30 ([spec_cli_020](spec_cli_020.md) CLI-12a, [spec_background_agents_032](spec_background_agents_032.md) BGA-08).* `blitz worktrees list|remove|prune`.

### 8.3 Session manager — P2, M
**CC** agent view (`claude agents`, `--bg`, `attach`, `logs`, `stop`), cross-session messaging. **AG** Agent Manager inbox. **Blitz today:** the service holds sessions but only one turn view per client.
- **PAR-PAR-20** `blitz agents` lists every running and waiting turn in the service across workspaces (state, cost, waiting for approval); `blitz --bg "<prompt>"` starts a turn in the service and returns; `blitz attach <id>` attaches the REPL to it; `blitz logs <id>`, `blitz stop <id>`. The desktop app shows the same as an inbox.

## 9. Terminal experience

**CC** Shift+Tab modes, Esc to interrupt, Esc Esc rewind, Ctrl+G external editor, vim mode, `/keybindings`, `/statusline`, `/theme`, `/color`, notifications, `/copy`, `/diff` viewer, `/config`, `/status`, `/context` breakdown, prompt suggestions, session recap, cross-project history search. **AGY** Ctrl+G editor, Ctrl+K fast approve, `/keybindings`, `/config`, themes. **Blitz today:** line editor with history, Ctrl+R, Tab completion, Ctrl+C, Ctrl+T steering.
- **PAR-UI-01 (P0, S)** ✅ *Done 2026-09-26 (ROADMAP 25.9, 25.12; now [spec_tui_019](spec_tui_019.md) TUI-13/14/17/31).* Esc interrupts the running turn (as Ctrl+C does now); Esc Esc on an empty prompt opens `/rewind`; Shift+Tab cycles permission modes.
- **PAR-UI-02 (P0, S)** ✅ *Done (ROADMAP 25.9; TUI-13).* Ctrl+G opens `$VISUAL`/`$EDITOR` with the current input and submits what is saved.
- **PAR-UI-03 (P1, S)** 🟡 *The desktop app's part done 2026-09-27 ([spec_desktop_024](spec_desktop_024.md) DSK-78a); the terminal's bell and notifications not yet.* Notifications: when a turn finishes after more than `ui.notify_after` seconds, or an approval/question waits, ring the terminal bell and send a desktop notification (`osascript`, `notify-send`), configurable; the `notification` hook event fires too.
- **PAR-UI-04 (P1, S)** `/copy [n]` — [spec_backlog_026](spec_backlog_026.md) BL-CLI-01.
- **PAR-UI-05 (P1, S)** `/status`: version, workspace, session, agent, model(s) and fallback state, permission mode, sandbox state, MCP server health, attached/local, config sources.
- **PAR-UI-06 (P1, S)** `/context` breaks the context down: system prompt, tool declarations, memory and rules, messages, tool results, images, with tokens per category and the compaction threshold.
- **PAR-UI-07 (P1, S)** `/config [key=value]` shows and sets any supported setting for the session, saving on request, with the in-place editor (CFG-20); `/set` becomes an alias.
- **PAR-UI-08 (P2, S)** Status line: `[ui] status_line = "<command>"` runs a command (JSON session state on stdin) whose first output line is shown under the prompt; a built-in default shows model, mode, context % and cost.
- **PAR-UI-09 (P2, S)** Vim editing mode (`[ui] editor = "vim"`), user keybindings (`~/.blitz/keybindings.toml`), and `/theme` for Markdown and diff colours.
- **PAR-UI-11 (P0, S)** ✅ *Done 2026-09-26 (ROADMAP 25.10; now [spec_tui_019](spec_tui_019.md) TUI-15/16); `/rewind` uses it with PAR-SES-02.* Inline pickers (decision §12.4): an arrow-key, type-to-filter menu inside the existing line editor — no full-screen framework — used by `/rewind` (Esc Esc), `/resume`, `/agent`, `/model`, approval answers and multiple-choice questions. Rich full-screen views (context grid, diff browser, transcript viewer, agent inbox) are built in the desktop app, not the REPL.
- **PAR-UI-10 (P2, S)** `/diff` becomes browsable: a list of changed files with per-file diffs (paged).

## 10. Models, providers and installation

- **PAR-MOD-01 (P0, S)** ✅ *Done 2026-09-26 (ROADMAP 25.8; now [spec_models_015](spec_models_015.md) MDL-73/74).* `/effort low|medium|high|max` and `--effort` — the per-model reasoning settings of [spec_backlog_026](spec_backlog_026.md) BL-ENG-01, with a session-level shortcut. **CC, AGY.**
- **PAR-MOD-02 (P1, M)** Claude on Amazon Bedrock and on Google Cloud (Vertex/Agent Platform), and Azure/Microsoft Foundry for OpenAI and Claude models, as providers (`bedrock`, `vertex-anthropic`, `azure`), with their standard credential chains. **CC.**
- **PAR-MOD-03 (P1, S)** `blitz models` lists the models each configured provider offers (from the provider's list API where one exists), with pricing known to Blitz. **AGY** `agy models`.
- **PAR-MOD-04 (P1, S)** `llm.<provider>.api_key_command`: a command whose output is the key, run when needed and cached for `api_key_ttl`, for keys kept in password managers or rotated by a gateway. **CC** `apiKeyHelper`.
- **PAR-MOD-05 (P1, S)** Proxies: model and web clients honour `HTTPS_PROXY`/`NO_PROXY` and `[network] ca_file` (custom CA); `web_fetch` keeps refusing private addresses and, through a proxy, checks the resolved host before connecting. **CC** enterprise network configuration.
- **PAR-MOD-06 (P1, S)** Installation and updates: an install script (checksum- and cosign-verified), a Homebrew tap, and `blitz update` (checks the latest release, verifies its signature, replaces the binary). **CC, AGY.**
- **PAR-MOD-07 (P2, S)** OpenTelemetry metrics (sessions, turns, tokens, cost, tool calls, approvals, by model and agent) alongside traces and logs. **CC.**

## 11. Integrations (local equivalents only)

- **PAR-INT-01 (P1, M)** GitHub Action: `retail-cortex/blitz-action` runs `blitz exec` on `@blitz` mentions in issues and PRs and on configured events (review a PR, turn an issue into a PR), with a `dont-ask` permission mode, repository-scoped tokens and cost limits. **CC** GitHub Actions / GitLab CI.
- **PAR-INT-02 (P2, L)** Editor integration: a VS Code extension (and later JetBrains) that attaches to the service like the desktop app — conversation panel, approvals with native diff view, sending the current selection and open file as context, `@`-mentions. **CC** VS Code/JetBrains; **AG** is itself an IDE.
- **PAR-INT-03 (P2, S)** Deep links: `blitz://open?dir=…&prompt=…` opens the desktop app (or a terminal) in a workspace with a prompt pre-filled, never sent without the user. **CC** deep links.

Out of scope as not local: claude.ai/Google account login and plans, cloud sessions and teleport, Routines in a vendor cloud (Blitz workers are the local equivalent), mobile apps and Remote Control through a vendor service, Slack, published artifacts on a vendor site, Antigravity's editor tab completion and inline commands, Nano Banana image generation, `/passes`, `/mobile`, `/radio`, `/insights`.

## 12. Decisions (owner, 2026-09-26)

Five reference features conflicted with earlier Blitz decisions. There is no data from Blitz users (telemetry is off by default and there are no issues yet), so each was decided on the references' choices, Anthropic's published data and known incidents.

| # | Feature | Decision | Why | Requirements |
|---|---|---|---|---|
| 12.1 | `--dangerously-skip-permissions` | **Adopt as a `bypass` mode, only inside an active OS sandbox**; no one-word flag. Most effort goes to `accept-edits` and `auto` modes instead | Users approve 93% of prompts (Anthropic), so prompt fatigue is real; Anthropic answered it with a reviewing model, calling skip-all "unsafe in most situations"; documented home-directory wipes. Blitz's OS sandbox, on by default, blocks writes outside the workspace | PAR-PERM-05, -06, §2.3 |
| 12.2 | `/cd` | **Adopt**, by reopening the workspace from scratch | Claude Code added it (June 2026); `engine.Open` now builds everything per workspace, so nothing is patched mid-session | PAR-SES-40–43 |
| 12.3 | Project-level configuration | **Adopt in layers**: tighten-only content without asking; code-running or loosening content after trust pinned to a content hash; credentials, endpoints and sandbox loosening never | Teams expect committed config (CC, AGY); CC's project settings produced CVE-2025-59536, CVE-2026-21852 and CVE-2026-40068 | PAR-CFG-01–06 |
| 12.4 | Full-screen views | **Keep the REPL line-based; add inline pickers**; full-screen views go in the desktop app | Rewind and resume need a picker, which the line editor can host; no evidence users need a full-screen terminal UI | PAR-UI-11 |
| 12.5 | `/boost`, agent teams, dynamic workflows | **Defer** until background sub-agents and worktrees (§8.1–8.2) exist, then decide from how they're used | Even third-party guides describe `/boost` as future or vague; CC's teams build on background agents Blitz lacks | — |

Evidence: [Anthropic — Claude Code auto mode](https://www.anthropic.com/engineering/claude-code-auto-mode) ("Claude Code users approve 93% of permission prompts"); [Check Point Research on CVE-2025-59536 / CVE-2026-21852](https://research.checkpoint.com/2026/rce-and-api-token-exfiltration-through-claude-code-project-files-cve-2025-59536/); [SentinelOne on CVE-2026-40068](https://www.sentinelone.com/vulnerability-database/cve-2026-40068/); [Claude Code week 24 (`/cd`)](https://code.claude.com/docs/en/whats-new/2026-w24.md); [Obsidian Security on skipping permissions](https://www.obsidiansecurity.com/academy/dangerously-skip-permissions-what-it-does-and-how-to-contain-it). The cheapest source of real preference data is to ask early users about 12.1 and 12.3 once they have the build.

## 13. Sources

- Claude Code documentation: [index](https://code.claude.com/docs/llms.txt), [commands](https://code.claude.com/docs/en/commands.md), [tools](https://code.claude.com/docs/en/tools-reference.md), [hooks](https://code.claude.com/docs/en/hooks.md), [CLI](https://code.claude.com/docs/en/cli-reference.md), [permission modes](https://code.claude.com/docs/en/permission-modes.md), [interactive mode](https://code.claude.com/docs/en/interactive-mode.md), [memory](https://code.claude.com/docs/en/memory.md), [checkpointing](https://code.claude.com/docs/en/checkpointing.md).
- Antigravity CLI (third-party): [ComputingForGeeks cheat sheet](https://computingforgeeks.com/antigravity-cli-cheat-sheet/), [AI Builder Club CLI guide](https://www.aibuilderclub.com/blog/antigravity-cli-guide), [explainx.ai reference](https://www.explainx.ai/blog/antigravity-cli-features-sandbox-plugins-subagents-2026).
- Antigravity (IDE): [Google Developers Blog launch post](https://developers.googleblog.com/build-with-google-antigravity-our-new-agentic-development-platform/), [AI Builder Club guide](https://www.aibuilderclub.com/blog/google-antigravity-complete-guide), [product page](https://antigravity.google/). The official docs site (antigravity.google/docs) returned no content when fetched.

## 14. Suggested order

1. P0 safety and daily use: §2.1 modes (including `bypass`), §2.2 rules, §4.1 rewind with the inline pickers (PAR-UI-11), §4.3 plans and task lists, §3.1 instruction compatibility, §5.1 `glob`, §5.3 cost/time limits, §6.1 hook events, §7.1 skills as commands, §9 PAR-UI-01/02, §10 PAR-MOD-01.
2. P1 in order of value: project configuration and trust (§2.4, which MCP workspace config and plugins depend on), `/cd` (§4.5), auto mode (§2.3), MCP (§5.4), background sub-agents and worktrees (§8.1–8.2), auto memory (§3.2), browser (§5.2), hooks decisions (§6.2), plugins (§7.2), `/goal` (§4.4), headless I/O (§5.3), providers and install (§10), GitHub Action (§11).
3. P2.

Items shared with [spec_backlog_026](spec_backlog_026.md) (`/fork`, `/copy`, `--add-dir`, `/grill-me`, reasoning settings, desktop features) are built once; do them in this order.
