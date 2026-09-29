---
title: "018 · Workspace"
weight: 18
---

*Workspace and the UI-agnostic application layer* (`spec_workspace_018`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/*.go` |
| Tests | `pkg/engine/*_test.go` (notably `multi_test.go`, `turn_test.go`, `workspace_test.go`) |
| Related | [spec_engine_016](spec_engine_016.md), [spec_sessions_017](spec_sessions_017.md), [spec_client_022](spec_client_022.md), [spec_service_021](spec_service_021.md) |

## 1. Purpose

`pkg/engine` is Blitz without a user interface. `engine.Open` builds a `Workspace` (agents, skills, tools, sessions, audit, model, engine) for one project directory, and the workspace exposes typed operations that return data and typed errors and **never print**. The terminal UI, the service and the desktop app all drive the same operations through the `api.Backend` interface, so a front end behaves identically whether the workspace is in-process or held by the service.

## 2. Opening a workspace

- **WS-01** `Open(ctx, cfg, Options)` resolves `cfg.Tools.WorkspaceDir` (expand `~`, absolute) and writes the absolute path back into `cfg`. Everything relative resolves against the workspace, never the process CWD: one process may hold several workspaces.
- **WS-02** The workspace owns `cfg` after `Open` and mutates it (model, pins, settings), so every workspace needs its own copy.
- **WS-03** One owner per workspace across processes: an exclusive, non-blocking OS file lock (`flock` on Unix) on `~/.blitz/locks/<hex(sha256(realpath))[:24]>.lock`. The lock file records the directory and PID for humans. A held lock yields `ErrWorkspaceBusy`. The lock is released on `Close` or any failure during `Open`, and by the OS if the process dies.
- **WS-04** Build order: locale → agent registry (+ external agents from trusted paths; errors warn) → skills provider (+ discovery when enabled; policy problems warn) → tool registry → image store prune (`retain_days`) → audit log (failure warns and disables) → session storage scoped to the workspace → persistent ADK event store → model → project memory → per-agent models → engine.
- **WS-05** If the configured model cannot be built, the workspace still opens, on a stand-in (`runtime.NewUnavailableModel`) named after the configured model whose every call fails with `ErrModelUnavailable` and the reason (`ModelErrorSummary`, secrets removed), so turns and worker runs fail with it instead of being answered; `ModelErr()` reports why. The same stand-in replaces the model when a settings change leaves it unbuildable (not the previous model, which the settings no longer describe), until `RetryModel` builds it.
- **WS-06** Per-agent models: an agent's frontmatter `default_model` applies, and an `[agent_models]` pin wins over it. Pins naming unknown agents warn and are skipped; pins whose model fails to build warn and are skipped.
- **WS-07** `Options`: `Streaming` (partial text), `Warn` (non-fatal problems), `Model` (test override), `Workers` (shared worker store), `NewModel` (model factory override).
- **WS-08** Extra system instructions = rendered project memory + (for non-English reply locales) a reply-language instruction.
- **WS-09** `Close` kills background processes and MCP servers, drains post-tool hooks, flushes the audit log and releases the lock.

## 3. Backend interface

Front ends use only `api.Backend` (implemented by `*Workspace` and by the service client). Groups:

| Group | Operations |
|---|---|
| Lifecycle / UI | `Dir`, `ModelErr`, `SetUI(approver, prompter)`, `Processes` (nil when remote), `AuditShell`, `ImagesEnabled`, `Close` |
| Turns | `Run`, `Steer` |
| Sessions | `OpenSession`, `ActiveSession`, `ListSessions(all)`, `NewSession`, `LoadSession`, `SaveSnapshot`, `RenameSession` |
| Agents & models | `ListAgents`, `ActiveAgent`, `SetAgent`, `Model`, `SetModel`, `PinModel`, `Unpin`, `AllModelSettings`, `ModelSettings`, `UpdateModelSettings`, `Settings`, `Set` |
| Extensions | `ListSkills`, `SearchSkills`, `Skill`, `ListEnvs`, `RemoveEnv`, `PruneEnvs`, `ListMCPServers`, `ActiveAgentTools` |
| Context | `SessionUsage`, `Context`, `Compact`, `MemoryFiles`, `ReloadMemory`, `AddMemory`, `AvailableLocales`, `SetLocale`, `SandboxSummary` |
| Changes | `ListCheckpoints`, `Undo`, `SessionDiff`, `GitDiff`, `ListApprovals`, `RevokeApprovals`, `ClearApprovals` |
| Images & search | `LoadImage`, `AddImage`, `LoadAttachments`, `SearchProvider`, `SearchWeb`, `SearchSession` |

## 4. Turn lifecycle (`Run`)

`Turn` fields: `Text`, `Prompt` (sent instead of Text), `Plan`, `ReadOnly` (mode name), `Aside` (/btw), `Accepted`, `Images`, `MaxTurns`, `MaxCostUSD`, `Timeout`, `FetchGrants`, `OnAccepted`, `OnFinished`.

- **WS-29** Limits: `MaxTurns` (model calls; `ErrMaxTurns`), `MaxCostUSD` (checked after every event against the session's cost since the turn began; needs a priced model; `ErrCostLimit`, message "the turn reached its cost limit ($x)"), `Timeout` (`ErrTimeLimit`). The cost and time limits cancel the turn's context with the limit as the cause, and the turn returns that cause. `IsLimit(err)` matches all three; workers use the same fields for their `limits`.

- **WS-20** Unless `Accepted`, the text passes `prompt_submit` hooks first; a refusal returns `*BlockedError{Reason}` with nothing recorded or sent. Accepted prompts are audited (`prompt`).
- **WS-21** The sent prompt is `Prompt` if set, else `Text`. With `Plan`, the prompt is wrapped by `runtime.PlanPrompt` and the transcript records `/plan <Text>`.
- **WS-21a** Mentions: every `@path` in the text as typed (IMG-01's grammar, `images.MentionedPaths`) that names a file or folder the agent may read (resolved through the workspace: inside a root, not blocked; an image is the front ends', IMG-01) adds a block to the prompt sent, after the hook contexts, in `<mentioned-files>`: a file as `<file path="…">` with its text (at most 64 KiB, else its start cut at a line and a note to read the rest with `read_file`; a binary file as a note of its size), a folder as `<folder path="…">` with its entries (folders with a slash, hidden and blocked ones left out, at most 200). At most 20 mentions and 256 KiB a prompt; paths that aren't there are left as written. The transcript records the text as typed.
- **WS-22** For non-aside turns: a checkpoint begins, labelled with the recorded text collapsed to single spaces and ellipsized to 60 chars; unless `Accepted`, the user message is recorded in the transcript with an attachment note `\n[images: a.png, b.png]`.
- **WS-23** `OnAccepted` runs after recording and before sending, so steer messages taken from then on follow the prompt in the transcript.
- **WS-24** `FetchGrants` pre-approve exactly those URLs for this turn (`tools.WithFetchGrants`).
- **WS-25** Aside turns go to `Engine.Aside` (no images, no checkpoint, no transcript, steer queue untouched). Other turns go to `Engine.Execute` with `WithMaxTurns`, one attachment per image, `WithPlanOnly` or `WithReadOnly(mode)`.
- **WS-26** After the engine stops: `OnFinished` runs; usage `Before`/`After` are captured; the concatenated final (non-partial, non-thought) model text is `Output` and is recorded as the model message if non-empty; unread steer messages are returned in `Leftover`. A turn that fails part-way still returns what it produced.
- **WS-27** `Steer(text)` runs `prompt_submit` hooks (refusal → `*BlockedError`), records the text as a user message, and queues it on the engine.
- **WS-28** Transcript write failures warn but never fail the turn. Messages are written to the turn's session by ID (SES-16).

### 4.1 Events
- **WS-30** Each ADK event becomes one `api.Event` per part, in order, with `Author`; exactly one of `Text{Text, Partial, Repeat, Thought}`, `ToolCall{ID, Name, Args, Partial}`, `ToolResult{ID, Name, Result}`.
- **WS-31** With streaming, partial text chunks arrive first; the final text event then repeats them with `Repeat=true`. Front ends show partial chunks and non-`Repeat` final text, so text appears once. `streamed` resets after each non-partial event.

## 5. Agents, models and settings

- **WS-40** `SetModel(ref)` builds the model (may switch provider), swaps it for all unpinned agents, sets `blitz.default_model`, and returns the active agent's pin if any (it still decides what the active agent runs on). Not persisted to the config file.
- **WS-41** `PinModel(agent, ref)` requires a known agent (`*UnknownAgentError`), builds the model, pins it, and saves `[agent_models]` in the workspace's own settings file (never the global one), so pins are per workspace and win over global ones. `Unpin` removes the pin, restores the agent's own `default_model` if declared, and removes the workspace's line, or, when the global settings pin the agent, saves `agent = ""` there to mask it. An empty pin means "not pinned". Results carry `Saved{Path, Err}`.
- **WS-42** `ModelSettings(ref)` rejects refs containing `=` or with no name (`ErrBadModelRef`); returns settings plus global temperature/max_tokens. `UpdateModelSettings(ref, reset, changes)` validates (`*InvalidSettingError`), applies from the next model call, lists keys the provider ignores as `Unsupported`, and saves under the key the file already uses for that model (e.g. a hand-written `openai/gpt-5`) else the bare name.
- **WS-43** `Set(key, value)` supports only `agency`/`agency_level` (`low|medium|high|extreme`, else `ErrInvalidAgency`; unknown keys `*UnknownSettingError`); agents' instructions embed agency, so the engine rebuilds. Session-only.
- **WS-44** `Settings()` returns agency, model, active agent, the reply locale and the permission mode. `SetPermissionMode(mode)` changes the workspace's mode (`ErrUnknownMode`, `ErrBypassNeedsSandbox`); in plan mode `Run` plans every prompt ([spec_approvals_005](spec_approvals_005.md) APR-14).
- **WS-45** Plans for approval: each non-aside turn gets a `tools.PlanGate`, planning when the turn is `Plan`, the session is in plan mode, or `[blitz] plan_review = always` (every prompt that isn't read-only or an aside). When the planning run ends with an approved plan, the workspace leaves plan mode for the chosen mode (outside plan mode only `accept-edits` changes it), records `(plan approved) <go-ahead>` (kind `plan`, not a rewind point) and runs `CarryOutPrompt` in the same turn without plan-only — at most 3 approved plans per prompt; stop hooks then apply to the carried-out run. The output of successive runs in a turn is separated by a blank line, in the result and as a text event. Commands with `mode: plan` run read-only (`ReadOnly = "/<name>"`) with their own prompt, not as plans.

## 6. Context, memory, locale, search

- **WS-50** `Context()` returns the latest prompt size (`Usage.LastPrompt`), whether auto-compaction is active (`compaction && token_threshold > 0`), the threshold and retained events.
- **WS-51** `Compact(focus)` summarises all but the latest user turn (keep = 1) and returns events compacted, summary length and usage before/after; `ErrNothingToCompact` when too short.
- **WS-52** `ReloadMemory` re-reads memory files and rebuilds instructions; `AddMemory(text)` appends to the last configured memory file (default `BLITZ.md`) in the workspace and reloads.
- **WS-53** `SetLocale(input)` resolves a tag or language name (`ErrUnknownLocale`), changes the **reply** language for this workspace, rebuilds instructions, and saves `[ui] locale`. The interface language belongs to the client, which switches its own catalog with the returned tag.
- **WS-54** `SearchWeb(terms)` requires fetching enabled (`ErrNoFetch`) and a provider (`ErrNoSearch`); requests 10 results and keeps up to 5 viable links: http(s) with host, not a binary/document extension (pdf, archives, images, media, office), not an unresolved `/grounding-api-redirect/`, deduplicated by host+request URI (fragment ignored). Returns the links and a prompt (`runtime.WebSearchPrompt`); the caller runs it as a read-only turn with `FetchGrants` = the links.
- **WS-55** `SearchSession(terms)` searches the active transcript (up to 12 passages) and returns the match count and a prompt (`runtime.SessionSearchPrompt`).

## 7. Changes and approvals

- **WS-60** `ListCheckpoints` (newest first), `Undo(force)` (audits restored files as `undo`; partial restores return both restored files and the error), `SessionDiff()` (unified diff of every file changed this session vs. its pre-change state).
- **WS-61** `GitDiff(color)` runs `git diff --no-ext-diff --no-textconv --stat --patch` with `color.ui`, `core.fsmonitor=false`, and every configured `filter.*.{clean,smudge,process}` blanked (`required` set false), because the agent can write `.git/config` and git would otherwise run commands from it outside the sandbox.
- **WS-62** `ListApprovals` returns session rules then saved rules, parsed from keys `kind:subject` (`cmd:<dir>\x00<command>`, `write:<dir>`, `delete:<dir>`, `web:<host>`, `mcp:<server>:<tool>`, `uc-run:…`). `RevokeApprovals(keys…)` removes from both; `ClearApprovals` removes all.

## 8. Extensions

- **WS-70** `SkillInfo` combines a skill's metadata with the policy evaluation (hash, tier, bypass, network, env passed/withheld, blocked reasons, per-script verdicts). See [spec_skills_013](spec_skills_013.md).
- **WS-71** `PruneEnvs` removes script environments that are incomplete or not needed by any script the policy lets run; `ErrScriptsDisabled` when skills are off.
- **WS-72** `ListMCPServers` returns nothing if no server could be started; otherwise name, target (URL or command line), auto-approve.
- **WS-73** `ActiveAgentTools` lists the active agent's tools sorted by name with `PlanAllowed`, plus MCP servers offered to it (no `agents` list = primary only; `*` = all).

## 9. Errors (typed, for front ends to word and localise)

`ErrWorkspaceBusy`, `ErrNoActiveSession`, `ErrSnapshotNameTaken`, `*ResumeError`, `*BlockedError`, `*UnknownAgentError`, `ErrBadModelRef`, `*InvalidSettingError`, `*UnknownSettingError`, `ErrInvalidAgency`, `ErrNothingToCompact`, `ErrUnknownLocale`, `ErrImagesDisabled`, `ErrNoFetch`, `ErrNoSearch`, `ErrUndoConflict`, `ErrScriptsDisabled`, worker errors in [spec_workers_023](spec_workers_023.md).

- **WS-80** `ModelErrorSummary` truncates SDK errors at `ClientConfig:` and at the first newline and masks secrets, because some SDK errors embed the API key.
- **WS-81** `SecretRedactor` masks configured API keys, the web search key, every MCP `env` value and the values of scrubbed environment variables in audit, logs and telemetry.
