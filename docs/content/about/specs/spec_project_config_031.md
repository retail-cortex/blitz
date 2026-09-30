---
title: "031 · Project configuration"
weight: 31
---

*Project configuration with a trust boundary* (`spec_project_config_031`)

| | |
|---|---|
| Status | **Draft for review** (2026-09-29). A design, not yet built. It details [spec_parity_027](spec_parity_027.md) §2.4 (PAR-CFG-01–06) under decision §12.3. |
| Depends on | [spec_config_002](spec_config_002.md) (CFG-01, the workspace overlay CFG-40/41), [spec_approvals_005](spec_approvals_005.md) (permission rules and scopes), [spec_workers_023](spec_workers_023.md) (hash-pinned review, WK-30), [spec_service_021](spec_service_021.md), [spec_desktop_024](spec_desktop_024.md) |
| Size | L: about two weeks, in the phases of §9 |

## 1. Purpose

A team should be able to commit what Blitz needs to work in its repository (hooks, MCP servers, slash commands, permission presets) so that everyone gets it by cloning. That is also how a hostile repository attacks the person who opens it. Claude Code's project settings led to three CVEs: hooks ran before the trust dialog (CVE-2025-59536), a repository redirected the API base URL and received the user's key (CVE-2026-21852), and a crafted repository reused another directory's trust (CVE-2026-40068).

This design lets a repository carry settings in three tiers:

- **Tighten-only settings** apply at once.
- **Settings that run code or loosen policy** apply only after the person trusts that exact content.
- **Credentials, endpoints and sandbox loosening** never apply from a repository.

## 2. How configuration works today

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
| **A: informs or tightens** | `permissions.deny`, `permissions.ask`; `sandbox.blocked_paths` (additions); `[skills.policy]` values stricter than the user's; `tools.max_parallel`, `tools.max_file_size_bytes` and other limits, only when lower; agent definitions and skills as prompt text (`./agents`, `./skills`, `.agents/skills`) | At once, with no question |
| **B: runs code or loosens** | `[[hooks.*]]`; `[mcp.servers.*]` (command and HTTP); `permissions.allow`; `sandbox.shell_writable_paths`; `[workers.policy]` values looser than the user's; scripts of the project's skills (`run_skill_script`) | Only after trust (§5) |
| **C: never** | API keys and every `*_api_key`; `base_url`, `search_url` and other endpoints; `[telemetry]`; `[log]`, `[audit]` and `[checkpoints]` directories; `session.storage_dir`; `blitz.auto_approve`; `blitz.permission_mode = "bypass"`; `sandbox.shell = "off"`; `sandbox.allow_network`; `web.allow_private`; removing blocked paths; `blitz.trust_workspace` | Ignored, with a warning |

- **PRJ-10 Tier A, how it merges.** Deny and ask rules are added, labelled with the scope `project`. Blocked paths are added. A policy value or limit applies only when it is stricter than the user's; otherwise it is ignored and `doctor` says so.
- **PRJ-11 Tier B, how it merges.** The user's settings keep precedence (PAR-CFG-01):
  - Project hooks run after the user's hooks for the same event.
  - A project MCP server is added unless the user has one with the same name, which wins.
  - Allow rules are added with the scope `project`.
  - Writable paths must lie inside the workspace; any outside it are ignored.
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

  A "don't trust" answer is recorded too (`trusted: false`), so the question isn't repeated until the content changes. The states are `none` (no tier B content), `new`, `changed` (the hash differs), `trusted` and `declined`.
- **PRJ-22 When tier B loads.** Only when the state is `trusted`. Otherwise the workspace opens without it: no hook, MCP server or allow rule from the project is loaded, started or parsed beyond TOML decoding (PAR-CFG-06). The trust check runs in `config.LoadWorkspace`, before `engine.Open` starts hooks and MCP servers.
- **PRJ-23 Trusting is for one hash.** It records that hash and reopens the workspace, as `/cd` will (§12.2). A trust answer names the hash it was shown, and one that no longer matches is refused (`HASH_MISMATCH`), as for workers.
- **PRJ-24 What people see before trusting.** The exact items, one line each, with what they do: "hook `pre_tool` runs `./scripts/check.sh` (in the repository)", "MCP server `db` runs `npx @acme/db-mcp`", "allow rule `Bash(make test)`". People trust the items shown, not a file name.

## 6. Where people are asked

- **PRJ-30 The REPL, run locally.** Configuration is loaded before `engine.Open`. When the state is `new` or `changed` and stdin is a terminal, the REPL lists the items and asks `Trust these project settings? [t]rust, [d]on't trust, [s]how the files`, then opens the workspace. The prompt uses the line editor, created before the workspace opens.
- **PRJ-31 The REPL attached to the service.** Right after attaching, `GetProjectSettings` returns the state, the items and the hash. If trust is needed, the REPL asks the same question and calls `TrustProject(hash, trusted)`, and the service reopens the workspace. If a turn is running in another client, the reopen waits for it to end, and the REPL says so.
- **PRJ-32 No terminal** (`blitz exec`, pipes). The project is treated as untrusted and one line on stderr says so. `--trust-project` trusts the content for that run only, without recording anything. `--trust-workspace` becomes an alias of `--trust-project`.
- **PRJ-33 The desktop app.** Opening a workspace calls `GetProjectSettings`. For `new` or `changed`, a dialog lists the items, with **Trust** and **Don't trust**. A declined or pending workspace shows a thin bar above the conversation ("Project settings not loaded: hooks, 2 MCP servers. Review"). Settings › Workspaces gets **Project settings** for each workspace: its files, what was applied, what is waiting for trust, what was ignored, and **Trust** / **Revoke**.
- **PRJ-34 Commands.**
  - `blitz trust [--revoke] [dir]` and `/trust` show the state and the items, and change the decision.
  - `blitz doctor` lists the project files, the tier A settings applied, tier B's state, and each ignored key with its reason.
- **PRJ-35 Workers.** A worker's run uses the workspace as opened. If tier B isn't trusted, project hooks don't run for workers either.

## 7. API

- **PRJ-40** `api.Backend` gains two methods:
  - `ProjectSettings() api.ProjectSettings`: files, applied items, pending items, ignored keys, state and hash;
  - `TrustProject(hash string, trusted bool) error`.

  In the service these are `WorkspaceService.GetProjectSettings` and `TrustProject`. The client implements them, so the REPL works the same attached or local.
- **PRJ-41** Permission listings (`ListPermissionRules`, the run settings panel) show the scope `project` for rules from project files. They can't be removed from Blitz: they're edited in the file.

## 8. What changes for existing users

- **Agents and skills.** `./agents` and `./skills` load as prompt text without `trust_workspace` (tier A), because PAR-CFG-02 says agent definitions and skills as prompt text load without asking. Their scripts need trust (tier B).
- **`blitz.trust_workspace`.** In the user's own files it becomes a deprecated alias: `true` trusts the project's current content once, and is then recorded like any trust decision (open question 2).
- **Commands and instructions.** They already load without trust and stay that way (tier A).

## 9. Phases

1. **Loading** (`pkg/config`): the key table and tiers, merging, the report of applied, pending and ignored items, the hash. Table-driven tests for every key in §4 and for each loosening that must be refused.
2. **Trust store** (`~/.blitz/trust.json`) and the reopen: `ProjectSettings`/`TrustProject` in the engine, the service and the client, with the `HASH_MISMATCH` contract.
3. **CLI:** the prompt before open, `--trust-project`, `blitz trust`, `/trust` and doctor.
4. **Desktop app:** the dialog, the bar, and Settings › Workspaces › Project settings.
5. **Docs:** this spec as built, the configuration guide ("Sharing settings with your team"), manual checks, including the three CVE scenarios as tests (a hook that would run before trust, a `base_url` in a project file, the same content at another path).

## 10. Open questions for the owner

1. **Models in project files.** Should a project be able to choose the default model, `[agent_models]` and `[model_settings]`? A model choice can send the code to another provider and change cost. *Recommendation:* allow it in tier B, only for providers the user has already configured, and never with provider settings.
2. **`trust_workspace`.** Retire it (with a warning and the §8 alias), or keep it as "always trust this workspace, whatever it contains", which is a hole in hash pinning. *Recommendation:* retire it.
3. **Tier A shown at open?** *Recommendation:* no prompt, but one line in the REPL ("project settings: 3 deny rules") and the full list in `doctor` and the desktop's Project settings.
4. **Protecting `.blitz/` from the agent.** An agent can edit `.blitz/settings.toml`, but any tier B change needs trust again, so the file needs no special protection. *Recommendation:* leave it writable; a changed hash asks again.
