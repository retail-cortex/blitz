---
title: "026 · Backlog"
weight: 26
---

*What's missing: unimplemented requirements* (`spec_backlog_026`)

| | |
|---|---|
| Status | **Partly implemented:** 23 of its 49 requirements done (counted 2026-09-29); the rest open. Forward-looking requirements, collected 2026-09-26 from the known-gaps sections of specs 001–025, the roadmap (`docs/content/about/roadmap.md`) (item gaps, the Antigravity review's candidates) and `.agents/NEXT_STEPS.md` |
| Depends on | Everything before it: each item extends the spec named in its **Extends** line |

## 1. Purpose

Specs 001–025 describe what Blitz **does**. This spec lists what it **doesn't do yet** but should, written as requirements, so each item can be picked up, built, tested and moved into its home spec. Everything here is actually missing, not a design limitation: deliberate limitations are listed separately in §10 so they aren't mistaken for work.

Each item gives: the problem, requirements (`BL-…`) that define "done", where the change lands, a rough size (S ≤ half a day, M ≈ 1–2 days, L ≈ 3+ days), and where it came from. When an item is done, move its requirements into the spec it extends and delete it here.

Gaps against Claude Code and Antigravity are in [spec_parity_027](spec_parity_027.md); items appearing in both are built once, in 027's order.

Suggested order (from NEXT_STEPS): run the manual checks (§9) first — their failures come before anything else — then §5.1, §5.2, §4.1, §7.1–7.3, then the rest by value.

## 2. Engine and models

### 2.1 Reasoning settings per model — M
**Extends** [spec_models_015](spec_models_015.md). *Source: NEXT_STEPS 6.* `/model_settings` covers temperature, max_tokens, top_p and seed, but not reasoning effort or thinking budgets.
- **BL-ENG-01** ✅ *Done (ROADMAP 25.8; MDL-73).* `[model_settings."<model>"]` accepts `reasoning_effort` (`low|medium|high`) and `thinking_budget` (tokens; `0` disables thinking where the model allows it), validated like the existing keys (CFG-30).
- **BL-ENG-02** ✅ *Done (ROADMAP 25.8; MDL-73).* They map to genai `ThinkingConfig` in the settings wrapper and each adapter translates them: Gemini natively; Anthropic to extended thinking with `budget_tokens` (and `max_tokens` raised above the budget when needed); OpenAI to `reasoning.effort`. A setting a model doesn't accept is dropped and reported as unsupported (MDL-72).
- **BL-ENG-03** ✅ *Done (ROADMAP 25.8; MDL-73).* Thinking blocks keep round-tripping unchanged across tool calls (MDL-24) with thinking on.

### 2.2 Usage survives the process — S
**Extends** [spec_engine_016](spec_engine_016.md) ENG-82. Usage lives in memory only, so after `--resume` (or a service restart) `/cost` and `/context` start at zero and a worker run's cost is known only while the service runs.
- **BL-ENG-10** ✅ *Done 2026-09-29 (SES-15).* A session's cumulative usage (calls, tokens by kind, cost, priced flag, last prompt size) is stored in its metadata after each model call's turn and restored when the session is opened.
- **BL-ENG-11** ✅ *Done 2026-09-29 (SES-15).* `/context` after resuming reports the last prompt's size, not 0.

### 2.3 Signing in with Google Cloud and Anthropic accounts — S
**Extends** [spec_models_015](spec_models_015.md) (Gemini and Claude on Vertex AI, `auth = "adc"`; Claude with `auth = "oauth"`). Found 2026-09-28 trying Claude Opus 5.5 on Vertex AI, which answered `429 RESOURCE_EXHAUSTED` (no quota yet for the project).
- **BL-ENG-20** ✅ *Done 2026-09-28 (MDL-30a).* A quota error from Vertex AI (`429` with `RESOURCE_EXHAUSTED`, "Quota exceeded for …") is reported as quota, with Google's message and the quota to raise, not as "rate limited": in `blitz doctor --online` (the whole message, not its first line), the "model isn't available" note, and a turn's error.
- **BL-ENG-21** ✅ *Done 2026-09-28 (MDL-73).* `thinking_budget` on current Claude models: a budget above 0 sends `budget_tokens`, which Claude Opus 4.7 and later, Claude Sonnet 5 and Fable reject (400); `0` sends thinking disabled, which Claude Opus 5.5 and Fable reject. On those models thinking stays adaptive and the setting maps to effort, or is reported as unsupported (MDL-72).
- **BL-ENG-22** ✅ *Done 2026-09-30 ([spec_models_015](spec_models_015.md) MDL-26a); `default` means `claude-opus-4-8` there.* Claude on Vertex AI has no server-side refusal fallback (it's off there): use the SDK's client-side fallback middleware (`lib/betafallback`) with per-conversation state, so `fallbacks` works on Vertex AI too.

## 3. Sessions

### 3.1 `/fork [n]` — M
**Extends** [spec_sessions_017](spec_sessions_017.md). *Source: Antigravity review, candidate 4.* Snapshots branch only from the current point.
- **BL-SES-01** ✅ *Done 2026-09-30 ([spec_sessions_017](spec_sessions_017.md) SES-20).* `/fork [n]` starts a new session copied from this one up to and including user turn `n` (default: the previous turn), in both stores (transcript and ADK event log), with `from` set; the source is unchanged. Compaction events whose range reaches past the cut are dropped from the copy.
- **BL-SES-02** ✅ *Done 2026-09-30: `/session list` shows the copy; `ForkSession` RPC.* `/session list` shows the fork; `SessionService` gains a `ForkSession` RPC.

## 4. Tools and sandbox

### 4.1 Faster Linux blocked-path masking — M
**Extends** [spec_shell_007](spec_shell_007.md) SH-24. *Source: NEXT_STEPS 4.* Scanning the writable roots before every sandboxed command costs ~0.1 s (3.4 s under `-race`); CI's parallel-cap test runs unsandboxed because of it.
- **BL-SH-01** ✅ *Done 2026-09-29 (SH-24a).* Median sandbox start-up on Linux ≤ 20 ms for a workspace of 50 000 entries, measured by a benchmark in CI.
- **BL-SH-02** ✅ *Done 2026-09-29 (SH-24a; cache directories aren't excluded).* No weaker masking than today for files that exist when the command starts: a reused scan is invalidated when a watched root changes (e.g. directory mtimes or inotify), and cache directories are excluded from name-pattern scans only for patterns that can't match there.
- **BL-SH-03** ✅ *Done 2026-09-29 (SH-24a).* The parallel-cap test runs with the sandbox on.

### 4.2 `--add-dir <path>` — S
**Extends** [spec_filetools_006](spec_filetools_006.md), [spec_cli_020](spec_cli_020.md). *Source: Antigravity review, candidate 2.*
- **BL-FS-01** ✅ *Done 2026-09-30 ([spec_filetools_006](spec_filetools_006.md) FS-01a).* `--add-dir PATH` (repeatable, startup only) adds an existing directory as an extra read-write root for this run, exactly like `sandbox.allowed_paths` (file roots and OS sandbox writable dirs), and `/sandbox` lists it. It can't widen `blocked_paths`. Not persisted.

### 4.3 Workers' edits join `/undo` — S
**Extends** [spec_workers_023](spec_workers_023.md), [spec_filetools_006](spec_filetools_006.md). *Source: ROADMAP 24c.* A worker's file changes go into the workspace's shared checkpoint stack, so `/undo` in an interactive session can revert a worker's edits (or be blocked by them) without saying so.
- **BL-WK-01** ✅ *Done 2026-09-30 ([spec_workers_023](spec_workers_023.md) WK-54).* Each worker run gets its own checkpoint stack; `/checkpoints` and `/undo` in interactive sessions don't see it.
- **BL-WK-02** ✅ *Done 2026-09-30 (WK-55).* A run's changed files are recorded in its run record (`files`), and `blitz workers undo <run-id>` restores them with the same conflict rules as `/undo` (FS-63).

### 4.4 Web search cost — S
**Extends** [spec_web_008](spec_web_008.md). Google grounding bills each search query Gemini runs; `/cost` doesn't count them.
- **BL-WEB-01** ✅ *Done 2026-09-30 ([spec_web_008](spec_web_008.md) WEB-15); the prices are `[search_pricing."google"]`, a table of their own, since `[pricing]` is keyed by model.* The number of grounding search queries per `/search web` and `web_search` call is recorded, shown in `/cost` as a separate line with its estimated price (`[pricing.search."google"] per_1k_queries`, default the published rate after the free tier), and never mixed into token costs.

## 5. Service and attached clients

### 5.1 Attached CLI: processes and `!cmd` audit — M
**Extends** [spec_client_022](spec_client_022.md), [spec_service_021](spec_service_021.md). *Source: ROADMAP 8b, NEXT_STEPS 1.* Attached, `Processes()` is nil and `AuditShell` does nothing.
- **BL-SVC-01** ✅ *Done 2026-09-29 (CL-04).* `WorkspaceService` gains `ListProcesses`, `GetProcessOutput`, `KillProcess`, and `AuditShell(command, exit_code, start_error)`; the client implements `Processes()` through them so the REPL's exit prompt (TUI-40) lists and kills the service's background processes started from this client's turns.
- **BL-SVC-02** ✅ *Done 2026-09-29 (CL-04).* `!cmd` run by an attached CLI is recorded in the service workspace's audit log, as `user_shell`.
- **BL-SVC-03** ✅ *Done 2026-09-29 (CL-04).* Background processes started in a turn record the session that started them; a client only lists its sessions' processes.

### 5.2 Steer messages at the end of a remote turn — S
**Extends** [spec_client_022](spec_client_022.md) CL-05. Attached, `OnFinished` runs after the service already collected unread steer messages, so a message typed in that instant waits for the agent's next turn instead of being sent as the follow-up turn.
- **BL-SVC-10** ✅ *Done 2026-09-30 ([spec_engine_016](spec_engine_016.md) ENG-42), locally too.* A steer message accepted after the service collected the leftovers is returned to the client (e.g. in `SteerResponse` as "too late: send as the next turn"), and the REPL and the app send it as the next turn, as they do locally.

### 5.3 Worker run notifications — S
**Extends** [spec_workers_023](spec_workers_023.md). Scheduled runs are unattended and silent; a failed or limited run is only visible with `blitz workers runs`.
- **BL-WK-10** ✅ *Done 2026-09-30 ([spec_workers_023](spec_workers_023.md) WK-56).* Optional `[workers] notify` command (like a hook: JSON run record on stdin, outside the sandbox, trusted config) runs after every scheduled run whose status is in `notify_on` (default `failed`, `limited`).
- **BL-WK-11** The desktop app shows a badge on a workspace tab with unseen failed runs.

### 5.4 Session metadata written twice — S
**Extends** [spec_workers_023](spec_workers_023.md), [spec_sessions_017](spec_sessions_017.md). *Source: ROADMAP 24c.* With telemetry on, the workspace's and the run's session storage may both write a run session's metadata (`last_turn`).
- **BL-WK-20** Only the storage that owns a session writes its metadata; a test with telemetry on runs a worker while the workspace records turns and checks the run session's metadata is intact.

## 6. Skills

**Extends** [spec_skills_013](spec_skills_013.md). *Source: NEXT_STEPS 5.*
- **BL-SK-01 TypeScript scripts** (M): `language: typescript` runs with a pinned runtime (Node or Deno) inside the script sandbox; dependencies are installed into a per-requirement environment like Python's (SK-41), with the same approval and policy rules (`packages` gains an npm section).
- **BL-SK-02 Scripts that write the workspace** (M): a skill may declare `writes_workspace: true`; its script then runs with the workspace writable, the run's changes are snapshotted into the turn's checkpoint first (so `/undo` reverts them), and it needs tier ≥ 3 approval showing the resulting diff before the changes are kept.
- **BL-SK-03 `requires-python`** (S): a script's interpreter constraint is honoured with uv-managed interpreters when `uv` is present; otherwise the script is blocked with the reason.
- **BL-SK-04 `storage_uri` and resources** (M): scripts from `storage_uri` are fetched through the web rules (approval per host), pinned by hash in the definition, and cached; Castor `resources` are listed by `activate_skill`.
- **BL-SK-05 Network field** (S, needs Castor): replace `custom_hints.network` with a proto field once Castor adds one; keep reading the hint.
- **BL-SK-06 Real installs in CI** (S): CI runs the opt-in environment tests (`BLITZ_PYENV_TESTS=1`) on Linux.

## 7. Desktop app

**Extends** [spec_desktop_024](spec_desktop_024.md) §11. *Source: NEXT_STEPS 1, ROADMAP 23.9.*

### 7.1 Service lifecycle and tabs — S
- **BL-DSK-01** ✅ *Done 2026-09-27 ([spec_desktop_024](spec_desktop_024.md) DSK-51): a banner and polling, not the full service screen; open workspaces are kept.* When a call fails with `unavailable`, the app returns to the "service isn't running" screen (DSK-40) with **Start the service**, and restores the open tabs once it answers again.
- **BL-DSK-02** ✅ *Done (DSK-40–55): closed tabs move to Recent; the service is asked to close the workspace unless a turn runs in it (SVC-14).* Tabs can be closed (the workspace stays open in the service unless no other client uses it; then `CloseWorkspace`), and the open tabs and active tab are restored at the next start.

### 7.2 Commands in the composer — M
- **BL-DSK-10** ✅ *Done 2026-09-27 ([spec_desktop_024](spec_desktop_024.md) DSK-78c–e).* A line starting with `/` in the composer runs the same commands as the REPL, through the same `api.Backend` operations the REPL uses, with results rendered as notices; unknown commands are refused, not sent to the agent. At least: `/plan`, `/btw`, `/search web|session`, `/undo`, `/checkpoints`, `/diff`, `/cost`, `/context`, `/compact`, `/agent`, `/model`, `/session save`, `/rename`.
- **BL-DSK-11** ✅ *Done 2026-09-27 ([spec_desktop_024](spec_desktop_024.md) DSK-78c–e).* A command palette (Cmd/Ctrl+K) lists them with completion for agents, models and sessions.

### 7.3 Rendering — S
- **BL-DSK-20** ✅ *Done 2026-09-27: Markdown, safe links, coloured diffs, expandable tool entries (DSK-74/79/90) and syntax highlighting in code blocks and diffs (DSK-79a).* Model text renders as Markdown (code blocks highlighted, links shown but opened only in the system browser after a click, never inside the web view); approval diffs are coloured; a tool entry expands to its arguments and result.
- **BL-DSK-21** ✅ *Done (DSK-78).* The usage line adds the session total, as in the terminal (TUI-05).

### 7.4 Images — S
- **BL-DSK-30** ✅ *Done 2026-09-27 ([spec_desktop_024](spec_desktop_024.md) DSK-78b).* Images can be pasted or dropped into the composer; they're uploaded with `AddImage` and sent with the next turn by ID (SVC-13), shown as thumbnails.

### 7.5 Localisation — M
- **BL-DSK-40** ✅ *Done 2026-09-27 ([spec_desktop_024](spec_desktop_024.md) DSK-78f/g): embedded at build time; the window has its own language setting (System by default) rather than following `/locale` — see BL-DSK-41.* Every string in the page comes from the same catalogs as the terminal (served by the service or embedded at build time), follows the interface language (`/locale`), and passes the same catalog checks (I18N-05, I18N-06).
- **BL-DSK-41** The window's **System** language follows the terminal's `[ui] locale` when one is set, and user catalogs in `~/.blitz/locales` load too (the service would serve them: the desktop module can't read the configuration itself).

### 7.6 Workers view — S
- **BL-DSK-50** The workers list refreshes when `WORKER.md` files change (polling `ListWorkers`, or a watch RPC); a run's session opens in the conversation view with one click.

### 7.7 Releasing the app — M
- **BL-DSK-60** ✅ *Done 2026-09-27 ([spec_release_025](spec_release_025.md) REL-20–24): signed and notarised once the Apple secrets are set, ad hoc until then.* A macOS release job builds a universal `Blitz.app`, signs it with a Developer ID, notarises and staples it, and attaches a `.dmg` to the release; the app's version is the release tag's.
- **BL-DSK-61** ✅ *Done 2026-09-27 (REL-23, REL-25): `.deb` for amd64 and arm64, smoke-tested by installing it; Windows packaging removed.* Linux: an AppImage (or `.deb`) built in CI. Windows: the NSIS installer is built and smoke-tested, or the Windows packaging files are removed.

## 8. CLI and REPL

### 8.1 Candidates from the Antigravity review
**Extends** [spec_tui_019](spec_tui_019.md), [spec_cli_020](spec_cli_020.md).
- **BL-CLI-01 `/copy`** ✅ *Done 2026-09-30 (PAR-UI-04).* (S): copies the last reply to the clipboard (`pbcopy`, `wl-copy`, `xclip`, `clip.exe`), falling back to OSC 52 (works over SSH); says which was used.
- **BL-CLI-02 `/grill-me <task>`** (S): a prompt mode like `/plan` in which the agent must ask clarifying questions (`ask_user_question`) before proposing anything, and writes nothing; the transcript records `/grill-me <task>`.

### 8.2 `blz` in release archives — S
**Extends** [spec_release_025](spec_release_025.md). *Source: NEXT_STEPS decisions.*
- **BL-REL-01** Unix release archives contain `blz` as a symlink to `blitz` (Windows: `blz.exe` is not shipped); the README's install steps say so.

### 8.3 `blz refactor` — design first
**Extends** [spec_cli_020](spec_cli_020.md). *Source: NEXT_STEPS decisions.* Proposed, not designed. Before implementation, its spec must say what it adds over `blitz exec` (e.g. a plan-then-apply flow across files with one approval and `--dry-run` output), its flags, exit codes and output formats.

## 9. Verification still owed

Not code, but blocking confidence in the specs above. *Source: ROADMAP 9, MANUAL_VERIFICATION.*
- **BL-VER-01** Run every unchecked item of `MANUAL_VERIFICATION.md` (real providers 💲, terminal behaviour, macOS and Linux sandboxes, MCP, steering, fallback and pinning, the service, workers and the desktop app — §§36–37), and record results there; each failure becomes a backlog item here.
- **BL-VER-02** Verify the first release from `retail-cortex/blitz` against its new cosign identity (§18).
- **BL-VER-04** Claude on Vertex AI, live, once project `rmcguinness-lab` has Claude Opus quota (requested after 2026-09-28; `global` and `us-east5` both answered `429 RESOURCE_EXHAUSTED`): `blitz config set-auth anthropic adc`, then `blitz doctor --online` and a session with Claude Opus 5.5 (streaming, a tool call, an image, prompt caching in `/cost`), and the desktop form's "Google Cloud (ADC)". Blitz's side was checked up to the request: the credentials, the Vertex path and the model were accepted.
- **BL-VER-03** Windows: build, run, and document what's unsupported (no OS sandbox, no process guard, no skill scripts; shell tools need Git Bash or WSL).

## 10. Deliberate limitations (not backlog)

Recorded so they aren't re-proposed; change only with a new reason. From ROADMAP "Accepted limitations" and "Rejected", and the specs:
- The command policy is a guardrail; the OS sandbox is the boundary. Shell commands can read anything outside `blocked_paths`.
- On Linux a blocked-name file created by a command is visible to that same command (bubblewrap masks existing paths).
- File-tool symlink checks happen at call time (a swap between check and open is theoretically possible; `os.Root` still prevents escaping the roots).
- `/search web` pre-approves exactly five URLs for one turn and judges readability from the URL; `/search session` is literal and covers prompts and replies only.
- Google search returns the pages Gemini cited; Vertex AI credentials aren't supported for search; no Google Custom Search API (shuts down 2027-01-01); no Anthropic server-side web search.
- The OpenAI-compatible adapter never streams (text-encoded tool calls need complete responses).
- `/truncate`, `/tutorial`, model catalogs (`/add_model`, `/refresh_models`), full-screen overlays in the REPL (they belong in the desktop app), `/boost` and `/teamwork-preview` (deferred until background sub-agents exist), a one-word `--dangerously-skip-permissions` flag (a sandbox-only `bypass` mode replaces it). `/cd`, `bypass` and project configuration were decided on 2026-09-26 and are requirements in [spec_parity_027](spec_parity_027.md) §12, automatic complexity-based model routing, `/open`, `/usage`.
- No shared event bus; no Bazel; one engine service per user; snapshots are never continued in place; `fallback_models` switch only before any output.
