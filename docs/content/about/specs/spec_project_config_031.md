---
title: "031 · Project configuration"
weight: 31
---

*Project configuration with a trust boundary* (`spec_project_config_031`)

| | |
|---|---|
| Status | **Implemented** (2026-09-29), from the design reviewed that day; §10 records the decisions taken. It details [spec_parity_027](spec_parity_027.md) §2.4 (PAR-CFG-01–06) under decision §12.3. |
| Source | `pkg/config/{project,trust}.go`; `pkg/engine/project.go`; `pkg/engine/skills/policy.go` (untrusted roots); `pkg/engine/tools/permrules.go` (`SourceProject`); `apps/service/internal/server/workspace.go`; `pkg/client/project.go`; `apps/cli/{setup,trust,doctor}.go`; `apps/cli/internal/tui/project.go`; `apps/desktop/web/src/{ProjectDialog.tsx,project.ts}` |
| Tests | `pkg/config/project_test.go`, `pkg/engine/project_test.go`, `pkg/client/project_test.go`, `apps/cli/trust_test.go`, `apps/cli/internal/tui/project_test.go`, `apps/desktop/web/src/project.test.ts` |
| Depends on | [spec_config_002](spec_config_002.md) (CFG-01, the workspace overlay CFG-40/41), [spec_approvals_005](spec_approvals_005.md) (permission rules and scopes), [spec_workers_023](spec_workers_023.md) (hash-pinned review, WK-30), [spec_service_021](spec_service_021.md), [spec_desktop_024](spec_desktop_024.md) |
| Size | L, built in the phases of §9 |

## 1. Purpose

A team should be able to commit what Blitz needs to work in its repository (hooks, MCP servers, slash commands, permission presets) so that everyone gets it by cloning. That is also how a hostile repository attacks the person who opens it. Claude Code's project settings led to three CVEs: hooks ran before the trust dialog (CVE-2025-59536), a repository redirected the API base URL and received the user's key (CVE-2026-21852), and a crafted repository reused another directory's trust (CVE-2026-40068).

This design lets a repository carry settings in three tiers:

- **Tighten-only settings** apply at once.
- **Settings that run code or loosen policy** apply only after the person trusts that exact content.
- **Credentials, endpoints and sandbox loosening** never apply from a repository.

## 2. How configuration worked before

- **Settings files.** Settings come only from the user's configuration directory (CFG-01): `~/.blitz/.env.toml`, then the per-workspace overlay `~/.blitz/workspaces/<name>-<hash>/.env.toml`, then environment variables, then flags. Both files are the user's own, so either may set any key. The overlay's permission rules are added to the global ones, and every other key replaces the global value.
- **Read from the repository without trust:**
  - instruction files (`AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `BLITZ.md`) and rule directories;
  - slash commands (`.blitz/commands`, `.claude/commands`, `.agents/workflows`);
  - worker definitions, which are hash-pinned (WK-30).
- **Read from the repository only with `blitz.trust_workspace` or `--trust-workspace`:** agents (`./agents`) and skills (`./skills`, `.agents/skills`). Attached to the service, `--trust-workspace` is lost (`client.Attach` sends only the directory).
- **Nothing asks the user outside a turn.** The service's broker only asks during a running turn. The closest pattern is the worker review: list the item with its hash, then enable that hash; the service refuses a hash that has changed (`HASH_MISMATCH`).

## 3. Files

- **PRJ-01** A workspace may have `.blitz/settings.toml` (shared, committed) and `.blitz/settings.local.toml` (personal). The loader warns when the local file is tracked by git, as it does for local memory files (`memory.TrackedByGit`).
- **PRJ-02** Both files must be regular files: a symlink is ignored with a warning. Each may be at most 256 KB, and is decoded as TOML and nothing else. No include, `keychain:` or `xor:` resolution and no environment expansion is applied to their values.
- **PRJ-03** A project file uses the same key names as `.env.toml`, but only the keys in §4 are accepted. Any other key is ignored with a warning that names the file and the key. The list says what a project may set; nothing is inferred.

## 4. Tiers

| Tier | Keys | Applied |
|---|---|---|
| **A: informs or tightens** | `permissions.deny`, `permissions.ask`; `sandbox.blocked_paths` (additions); `tools.max_parallel`, `tools.shell_timeout_seconds`, `tools.max_file_size_bytes`, `skills.policy.max_timeout_seconds`, `workers.policy.max_turns`, `workers.policy.max_cost_usd`, each only when lower; `skills.policy.min_hitl_tier` only when higher; `skills.policy.deny_tools` (additions); `plugins.disable` (installed plugins off, [spec_plugins_033](spec_plugins_033.md)); agent definitions and skills as prompt text (`./agents`, `./skills`, `.agents/skills`) | At once, with no question |
| **B: runs code or loosens** | `[[hooks.*]]`; `[[mcp.servers]]` (command and HTTP, without `auto_approve` or `sandbox`); `permissions.allow`; `sandbox.shell_writable_paths`; `blitz.default_model` and `[agent_models]`; `workers.policy.allow` (added kinds); `plugins.enable` (installed plugins on); scripts of the project's skills (`run_skill_script`) | Only after trust (§5) |
| **C: never** | Everything else. With the reason "never from a project": `[llm]` (keys, endpoints, providers), `[web]`, `[browser]`, `[telemetry]`, `[log]`, `[audit]`, `[checkpoints]`, `[session]`, `images.dir`, `[pricing]`, `ui.status_line`, `blitz.auto_approve`, `blitz.permission_mode`, `blitz.trust_workspace`, `[permissions.auto]`, `sandbox.shell`, `sandbox.allow_network`, `sandbox.allowed_paths`, `sandbox.read_only_paths`, `sandbox.scrub_env`, `sandbox.commands`, `tools.auto_approve_commands` and the tools' directories, `mcp.servers.auto_approve`, `mcp.servers.sandbox`, the looser `[skills.policy]` keys, `workers.policy.max_concurrent`, `workers.paths`, `skills.paths`; any other key as "not a project setting" | Ignored, with a warning |

- **PRJ-10 Tier A, how it merges.** Deny and ask rules are added, labelled with the scope `project`. Blocked paths are added. A policy value or limit applies only when it is stricter than the user's; otherwise it is ignored and `doctor` says so.
- **PRJ-11 Tier B, how it merges.** The user's settings keep precedence (PAR-CFG-01):
  - Project hooks run after the user's hooks for the same event.
  - A project MCP server is added unless the user has one with the same name, which wins. It is never auto-approved and always sandboxed.
  - Allow rules are added with the scope `project`.
  - Writable paths must lie inside the workspace; any outside it are ignored ("outside the workspace").
  - `blitz.default_model` applies only when the user's settings don't set one, and an agent's model only when the user hasn't pinned that agent ("your own settings set it"). Either applies only for the configured provider or one with an API key ("a provider you haven't set up").
- **PRJ-12 Precedence.** From lowest to highest: defaults, `~/.blitz/.env.toml`, `.blitz/settings.toml`, `.blitz/settings.local.toml`, the user's workspace overlay, environment variables, flags. Project deny rules are always added, whatever the precedence.

## 5. Trust

- **PRJ-20 What is hashed.** Trust is keyed by the workspace's canonical path (absolute, symlinks resolved, with no repository-controlled indirection such as git's `commondir` or worktree pointers) **and** a hash of the tier B content only. The hash covers:
  - the tier B keys of both files, normalised to canonical JSON with sorted keys, each tagged with its file;
  - the content of every workspace file that a tier B command names as its program (for example `./scripts/lint-hook.sh`), so that editing the script asks again;
  - the content hashes (`ContentHash`) of the project's skills that have scripts.

  Editing comments or tier A keys doesn't ask again.
- **PRJ-21 Where decisions are kept.** In `~/.blitz/trust.json`, written atomically by the service or by the local CLI, like `workers.json`:

  ```json
  {"version": 1, "workspaces": {"/Users/me/src/app": {"hash": "sha256:…", "trusted": true, "at": "2026-09-29T10:00:00Z"}}}
  ```

  A "don't trust" answer is recorded too (`trusted: false`), so the question isn't repeated until the content changes. The states are `none` (no tier B content), `new`, `changed` (the hash differs), `trusted` and `declined`. A damaged file counts as no decisions.
- **PRJ-22 When tier B loads.** Only when the state is `trusted`. Otherwise the workspace opens without it: no hook, MCP server or allow rule from the project is loaded, started or parsed beyond TOML decoding (PAR-CFG-06). The trust check runs in `config.LoadWorkspace`, before `engine.Open` starts hooks and MCP servers.
- **PRJ-23 Trusting is for one hash.** It records that hash; the content is read again from disk and an answer about a hash that no longer matches is refused (`api.ErrProjectChanged`, `PROJECT_CHANGED` over the API), as workers refuse a stale review. In the service the workspace then reopens (`TrustProject` returns `reopened`); a workspace opened by the REPL itself picks the decision up when it next opens.
- **PRJ-24 What people see before trusting.** The exact items, one line each, with what they do: "hook `pre_tool` runs `./scripts/check.sh` (in the repository)", "MCP server `db` runs `npx @acme/db-mcp`", "allow rule `Bash(make test)`". People trust the items shown, not a file name.

## 6. Where people are asked

- **PRJ-30 The REPL, run locally.** Configuration is loaded before `engine.Open`. When the state is `new` or `changed` and stdin is a terminal, the REPL lists the items (`engine.ReviewProject`) and asks `Trust them? [t]rust, [d]on't trust, [s]how the files`, records the answer, then opens the workspace, so nothing starts twice. The question uses a plain line reader, before the line editor exists. Unanswered (Ctrl+C, end of input) records nothing.
- **PRJ-31 The REPL attached to the service.** Right after attaching, `GetProjectSettings` returns the state, the items and the hash. If trust is needed, the REPL asks the same question and calls `TrustProject(hash, trusted)`, and the service reopens the workspace. If a turn is running in it (another client's), the workspace isn't reopened: the decision applies when it next opens.
- **PRJ-32 No terminal** (`blitz exec`, pipes). The project is treated as untrusted and one line on stderr says so. `--trust-project` trusts the content for that run only, without recording anything, in a workspace opened by the CLI; attached to the service it only warns (the service's workspace is shared: `blitz trust` records a decision). `--trust-workspace` is a hidden alias of `--trust-project`.
- **PRJ-32a At start.** The REPL prints one line when a project has settings: how many apply, and how many wait for trust (a warning then).
- **PRJ-33 The desktop app.** Opening a workspace calls `GetProjectSettings` with its settings. For `new` or `changed`, a dialog (`ProjectDialog.tsx`) lists the files, what needs trust and its state, what applies, and what was ignored with why, with **Trust** and **Don't trust**; it opens once per content. While settings wait, a bar above the conversation says how many ("Project settings not loaded: 3 settings wait for your trust.") with **Review**. Settings › Workspaces has **Project settings** for each open workspace, opening the same dialog.
- **PRJ-34 Commands.**
  - `blitz trust [--revoke] [dir]` and `/trust` show the state and the items, and change the decision.
  - `blitz doctor` lists the project files, the tier A settings applied, tier B's state, and each ignored key with its reason.
- **PRJ-35 Workers.** A worker's run uses the workspace as opened. If tier B isn't trusted, project hooks don't run for workers either.

## 7. API

- **PRJ-40** `api.Backend` has two methods:
  - `ProjectSettings() api.ProjectSettings`: files, applied items, pending items, ignored keys, state and hash;
  - `TrustProject(hash string, trusted bool) error`.

  In the service these are `WorkspaceService.GetProjectSettings` and `TrustProject`. The client implements them, so the REPL works the same attached or local.
- **PRJ-41** Permission listings (`ListPermissionRules`, the run settings panel) show the scope `project` for rules from project files. They can't be removed from Blitz: they're edited in the file.

## 8. What changed for existing users

- **Agents and skills.** `./agents` and `./skills` load as prompt text without `trust_workspace` (tier A), because PAR-CFG-02 says agent definitions and skills as prompt text load without asking. Their scripts need trust (tier B): until then `skills.Evaluate` blocks them ("it's the project's (…), whose settings aren't trusted: blitz trust").
- **`blitz.trust_workspace`.** Deprecated, with a warning at open. Set in the user's own settings, it trusts the project's content for each run without recording anything (like `--trust-project`); a project can't set it.
- **Commands and instructions.** They already loaded without trust and stay that way (tier A).

## 9. Phases (as built)

1. **Loading** (`pkg/config`): the key table and tiers, merging, the report of applied, pending and ignored items, the hash. Table-driven tests for every key in §4 and for each loosening that must be refused.
2. **Trust store** (`~/.blitz/trust.json`) and the reopen: `ProjectSettings`/`TrustProject` in the engine, the service and the client, with the `HASH_MISMATCH` contract.
3. **CLI:** the prompt before open, `--trust-project`, `blitz trust`, `/trust` and doctor.
4. **Desktop app:** the dialog, the bar, and Settings › Workspaces › Project settings.
5. **Docs:** this spec as built, the configuration guide ("Sharing settings with your team"), manual checks, including the three CVE scenarios as tests (a hook that would run before trust, a `base_url` in a project file, the same content at another path).

## 10. Decisions (2026-09-29)

The review's open questions were settled on their recommendations:

1. **Models in project files.** `blitz.default_model` and `[agent_models]` are tier B, only for providers the user has set up, never over the user's own choice; `[model_settings]` and every provider setting are never taken from a project.
2. **`trust_workspace`.** Retired: deprecated, with the §8 meaning.
3. **Tier A at open.** No prompt; one line in the REPL (PRJ-32a), the full list in `/trust`, `doctor` and the desktop's Project settings.
4. **`.blitz/` stays writable by the agent.** Any tier B change it makes asks for trust again.
