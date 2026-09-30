---
title: "005 · Approvals"
weight: 5
---

*Approvals and user questions* (`spec_approvals_005`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/tools/hooks.go`, `approval_store.go`, `ask_user.go`; approver UI in `apps/cli/internal/tui` |
| Tests | `pkg/engine/tools/approval_test.go`, `approvals_checkpoints_test.go` |
| Depends on | [spec_config_002](spec_config_002.md), [spec_observability_003](spec_observability_003.md) (audit) |
| Used by | [spec_filetools_006](spec_filetools_006.md), [spec_shell_007](spec_shell_007.md), [spec_web_008](spec_web_008.md), [spec_mcp_009](spec_mcp_009.md), [spec_skills_013](spec_skills_013.md), [spec_agents_014](spec_agents_014.md), [spec_workers_023](spec_workers_023.md) |

## 1. Purpose

Sensitive tool actions (edits, deletions, commands, network, MCP tools, forged tools, skill installs) pass one gate, `Hooks.Approve`, which fails closed. Front ends supply the interactive approver; remembered and saved rules skip it; unattended runs (workers) replace it with a permission check.

## 2. Request and decision

- **APR-01** `ApprovalRequest{Tool, Kind, Detail, Diff, Key, KeyLabel, Targets}`. Kinds: `run_command`, `write_file`, `delete_file`, `network`, `mcp_tool`.
- **APR-02** `Key` names what a remembered decision covers; empty means it can only be approved once. `Targets` are what policies match (workspace-relative paths, the command, the URL host or search provider, `server:tool`); empty targets can't be matched by permission policies.
- **APR-03** Decisions: `deny`, `once`, `session` (same key until the process exits), `always` (same key, persisted).

| Action | Key | Label |
|---|---|---|
| File edit | `write:<workspace>` | file edits in the workspace |
| File delete | `delete:<workspace>` | file deletions in the workspace |
| Shell command | `cmd:<workspace>\x00<exact command>` | this exact command in this workspace |
| Web fetch | `web:<host>` (per host) | |
| Web search | per provider | |
| MCP tool | `mcp:<server>:<tool>` | |
| Forged tool run | `uc-run:…` | |
| Tier-3 skill script | none (never rememberable) | |

## 3. Evaluation order (`Approve`)

- **APR-10** Unattended context (`tools.Unattended(ctx, decide)`): only `decide` is consulted — auto-approval, session/saved rules and the interactive approver are bypassed and nothing is remembered. Allowed → audit `approval` `unattended-permitted`; refused → audit `denial` `unattended-refused` and error "<detail> isn't among this unattended run's permissions".
- **APR-11** Otherwise, in order: permission mode `bypass` → allowed, audit `mode-bypass`; `tools.auto_approve_commands` (commands only) → `auto-policy`; a session rule for the key → `session-rule`; a saved rule → `saved-rule`; mode `accept-edits` and a write or delete (paths were already resolved as writable) → `mode-accept-edits`; mode `dont-ask` → denied, `mode-dont-ask`; no approver → denied, audit `no-approver`, error suggesting a rule or a permission mode.
- **APR-15** Permission rules (`[permissions] allow/ask/deny`, `kind(pattern)` or a bare tool name; `tools.PermissionRules`, live): the gate evaluates them first by the request's kind and targets — deny if any target matches a deny rule (in every mode, audited `rule-deny`, also for unattended runs), else ask if any matches an ask rule (`MustAsk`: straight to the approver, skipping modes, allow rules and remembered approvals; refused in `dont-ask`, without an approver, and in unattended runs), else allow if every target matches an allow rule (`rule-allow`; never for unattended runs, which keep exactly their permissions). `shell` rules are applied by the command policy on every sub-command (deny joins deny, allow joins auto-approve, ask sets `MustAsk`; an unverifiable command sets `MustAsk` when ask rules exist). `web` deny/ask are also applied before `allow_domains` and fetch grants; `mcp` deny/ask apply to `auto_approve` servers; bare tool names, `skill` and `agent` deny rules refuse the call before it runs (engine `beforeTool`). `read` rules are deny-only and become blocked paths at start. Rules can be added and removed during a session (`read` rules only by saving; they apply from the next start); saving edits `[permissions]` in place, in the global settings or the workspace's own (`api.Scope`), whose rules add to the global ones; a saved rule applies as the file's, not also the session's. Rules are checked before they're saved (`config.ValidatePermissionRule`, set by the parser), also in the settings-file editor. `[permissions] read_only_defaults` (default on; a workspace may set it either way) adds built-in rules (source `built-in`): allow `config.ReadOnlyCommands` (`ls`, `pwd`, `cat`, `head`, `tail`, `wc`, `stat`, `du`, `df`, `which`, `grep`, `rg`, `diff`, `git status`, `git log`, `git show`, `git diff`, `git blame`, `git rev-parse`), and ask for `config.ReadOnlyGuards` (`git … --output`, `git … --ext-diff`, `rg … --pre`); a redirection to a file still asks (SH-11a). Sources as listed: `global`, `workspace`, `built-in`, `flag`, `session`. When the settings change, open workspaces replace their configured and built-in rules (`PermissionRules.ReplaceConfigured`, `Workspace.ReloadPermissions`). `tools.CheckPermissionRule` checks a rule without adding it and says how it matches (`prefix`, `glob`, `regex`, `path`, `name`) and whether it applies to a sample (an allow rule to every command in it, ask or deny to any), noting a redirection.
- **APR-15a** Rules follow their files, whoever edits them (the desktop app, `/permissions --save`, an editor, another process): the engine looks at the global settings, the workspace's own, the project's files (`.blitz/settings.toml`, `.blitz/settings.local.toml`) and the approvals file every 2 s (`settingswatch.go`), and applies a change to the live rules (`ReplaceConfigured`, `ReplaceProject`, `ApprovalStore.Reload`). Every tool call reads them, so a turn already running obeys an added or removed rule from its next action; background tasks share them. A project's loosening settings still wait for trust (spec_project_config_031); workers keep their own permissions. Watching clients hear of it: `WatchTasks` sends `settings_changed`, and the desktop app's Run settings and Settings › Permissions reload.
- **APR-14a** The `auto` mode (PAR-PERM-20, -21): after deny rules, the automatic allows (allow rules, remembered approvals, auto-approved commands) and `permission_request` hooks, what would ask the user goes to the reviewer (`Hooks.SetReviewer`, `Engine.review`): `[permissions.auto] model` (else the session's model) with the request (tool, kind, detail, targets, diff up to 8,000 characters), the last six messages, the workspace and `[permissions.auto] environment`, asked for `{"decision": "allow"|"deny", "reason": …}` (`parseVerdict` reads it in a code fence too). Allow proceeds (`auto-allow: <reason>` in the audit log), deny returns the reason to the agent (`auto-deny`); a failure or an answer that isn't a verdict asks the user (`auto-error`). An ask rule still asks the user. Its tokens count in the session's usage. `[permissions.auto]` comes from the user's settings only (a project may not set it); it follows accept-edits in Shift+Tab's cycle.
- **APR-14** Permission modes (per workspace, shared by every session and client of it): `default`, `accept-edits`, `auto` (APR-14a), `plan` (every prompt of `Workspace.Run` is planned — `PlanPrompt` and `WithPlanOnly` — while the transcript keeps the text as typed; worker runs and read-only/aside turns are unaffected), `dont-ask`, `bypass`. The starting mode is `[blitz] permission_mode`, or `bypass` for `auto_approve = true`. `bypass` requires the OS sandbox to be active, at start (otherwise `default` with a warning, `Registry.ModeNote`) and when switching (`ErrBypassNeedsSandbox`). `ParsePermissionMode` accepts Claude Code's spellings (`acceptEdits`, `dontAsk`, `bypassPermissions`).
- **APR-12** Otherwise the approver is asked inside an `approval` span (time waiting for the user is isolated from tool time; attributes tool, kind, decision). An approver error denies (`error`). `session`/`always` with a key add a session rule; `always` also saves it (a save failure is reported as "approved, but saving the rule failed"). Deny returns "the user declined this <kind>".
- **APR-13** Auto-approval and rules never override hard policy: deny rules, the file sandbox and the OS sandbox are checked before approval.

## 4. Saved rules

- **APR-20** `~/.blitz/approvals.json` (`tools.approvals_file`): `{"version":1,"rules":[{key,label,added}]}` sorted by key, written atomically (temp + rename) in a 0700 directory. A missing file is empty; a corrupt file is a startup error.
- **APR-21** Rules can be listed, revoked individually, or cleared (`/approvals`, [spec_workspace_018](spec_workspace_018.md) WS-62).

## 5. Interactive approver (terminal)

- **APR-30** Shows the detail and (for edits) a coloured diff truncated to `ui.diff_lines` (120); answers `y` once, `s` session, `a` always, `n` no. Ctrl+C at an approval prompt cancels the whole turn. With no terminal to ask, the request is denied unless covered by config.

## 6. `ask_user_question`, plans and the task list

- **APR-41** `exit_plan_mode(plan)`: only while the turn is planning (else an error). Unattended, `dont-ask` or no prompter → `approved: false` and "end your turn with the plan as your answer". Otherwise the plan goes to the user through the question prompter, with options *Yes, carry it out* / *Yes, and accept its file edits without asking* / *No, keep planning* (localized); any other answer is feedback (`approved: false, feedback`), and the agent revises. On approval the plan is saved to `.blitz/plans/<session>-<n>.md` (session ID made file-safe; `n` the first free number), the gate records the mode (`default` or `accept-edits`) and planning ends.
- **APR-42** `enter_plan_mode(reason?)` (registered only with `plan_review = agent-decides`): refused when no one can review (unattended, `dont-ask`, no prompter); otherwise the rest of the turn plans.
- **APR-43** `todo(items)`: the whole list, each `{content, status}` with status `pending|in_progress|done` (`todo`, `in-progress`, `completed` and the like are normalized); at most 50 items; empty content or an unknown status is an error; more than one `in_progress` adds a note. The result carries the normalized items, `done` and `total`; front ends show it as a checklist (`api.Event.Tasks`).

- **APR-40** Args `question`, `options?`. Empty question → error. Unattended → error "this run is unattended: no one can answer; decide yourself and say what you assumed". No prompter → "interactive input is not available; proceed with your best judgement". Otherwise returns the user's answer. The prompter shares the front end's input reader (no competing stdin readers).
- **APR-40a** Several questions and multi-select (PAR-TOOL-05): `multi_select: true` (it needs `options`) lets the user choose several; `questions: [{question, options?, multi_select?}]` (at most 8) asks them in turn, instead of `question`. A multi-select question is marked on the prompter's context (`api.WithMultiSelect`, `Question.multi_select`, `TaskRequest.MultiSelect`); its answer is the options chosen, one per line, returned as `selected` and joined in `answer` (", "). With `questions`, the result is `answers: [{question, answer, selected?}]`; an error stops at the question it happened on, with the answers so far. The REPL reads a multi-select answer as numbers or text separated by commas; the desktop app shows checkboxes and **Send**; stream-json input sends `multi_select` with the question.
