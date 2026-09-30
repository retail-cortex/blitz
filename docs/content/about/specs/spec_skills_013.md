---
title: "013 · Skills"
weight: 13
---

*Skills, the skills policy and skill scripts* (`spec_skills_013`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/skills/{skill,definition,policy,provider}.go`, `pkg/engine/skills/builtin/*`; `pkg/engine/tools/{skills_tools,skillscript,pyenv,scriptbox,scriptbox_gvisor_linux}.go` |
| Tests | `pkg/engine/skills/*_test.go`; `pkg/engine/tools/skillscript_test.go`, `pyenv_test.go`, `scriptbox_test.go`, `scriptbox_gvisor_linux_test.go`; `apps/cli/internal/tui/skills_test.go`, `envs_test.go` |
| Depends on | [spec_config_002](spec_config_002.md), [spec_approvals_005](spec_approvals_005.md), [spec_shell_007](spec_shell_007.md) (OS sandbox) |

## 1. Purpose

Skills are reusable instructions (`SKILL.md`) an agent loads on demand, optionally shipping scripts. Frontmatter follows the Agent Skills fields plus Castor's `castor.skills.v1.SkillDefinition` (proto field names). A host **skills policy** caps what any skill may do; the stricter of the skill's request and the policy always applies. Scripts run only sandboxed, read the workspace but never write it.

## 2. Discovery

- **SK-01** Built-in skills are embedded (`code-review`, `git-workflow`, `testing-tdd`). External skills are `SKILL.md` files under `skills.paths`: `~/.blitz/skills`, and the project's `./skills` and `.agents/skills`, whose scripts run only once the project's settings are trusted ([spec_project_config_031](spec_project_config_031.md)).
- **SK-02** External skills cannot replace built-ins (reported). Parse errors are reported (a typo must not make a skill vanish silently) while other skills still load. Neighbouring resource files are indexed.
- **SK-03** `List` is sorted by name; `Search` matches name, description or tags.

## 3. Definition

- **SK-10** Agent Skills fields: `name`, `description`, `license`, `compatibility`, `allowed-tools`, `metadata`. Castor fields: `tags`, `version`, `author(s)`, `allowed_tools` (legacy), `tool_requirements[{name, scopes, description}]`, `category`, `trigger_phrases`, `execution_hints{preferred_model, requires_human_approval, environment_variables, timeout_seconds, custom_hints, hitl_tier, allow_hitl_bypass}`, `compiled_reference`, `scripts[]`, `skill_id`, `uri`, `source_uri`. Unknown keys are ignored.
- **SK-11** Script: `name` (letters, digits, `. _ -`, unique), `language` (`python`/`typescript` or proto enum names; required), exactly one of `inline_code`, `storage_uri` (not supported yet), `relative_path` (must stay inside the skill directory), `entry_point`, `dependencies`, `timeout_seconds ≥ 0`, `environment_variables` (valid names).
- **SK-12** HITL tiers by **name only** (`HITL_POLICY_TIER_2_AUDITED_WRITE`, `TIER_2_AUDITED_WRITE`, `tier_2`); bare numbers are refused (the proto enum is offset by one). Declared tier = `hitl_tier`, else the compiled reference's, and at least tier 3 when `requires_human_approval`.
- **SK-13** Network need: `custom_hints.network` = `true`/`required` (no proto field yet).
- **SK-14** A skill with definition problems still loads (instructions are useful) but its scripts don't run.
- **SK-15** Content hash: SHA-256 over every regular file in the skill directory in path order (relative path, size, content), symlinks skipped, format `sha256:<hex>`, bounded size.

## 4. Policy evaluation (`[skills.policy]`)

- **SK-20** Blocks for all scripts: definition problems; unhashable content; `trusted_hashes` set and the hash not listed; a tool requirement `name` or `name:scope` matching `deny_tools` globs; network needed but not granted (granted only with `network = "allowlist"` and the skill in `network_allow`).
- **SK-21** Tier: stricter of declared and `min_hitl_tier` (0 or 1 → tier 1; capped at 3; default 2). Tier 0 (bypass) requires both the skill's `allow_hitl_bypass` and the policy's; otherwise it becomes tier 3.
- **SK-22** Environment: variables the skill asks for pass only if they match `env_passthrough` globs; the rest are withheld (reported).
- **SK-23** Per script: language must be in `languages` (default `python`, `typescript`); timeout = script's, else hints', capped by `max_timeout_seconds` (300) which also applies when none is set; each dependency must be a plain PEP 508 requirement (no URLs, paths or pip options), not in `packages.deny`, in `packages.allow` if set (PEP 503-normalised names, globs), and pinned `==` when `require_hashes`.
- **SK-24** `Problems()` of the policy are warned at startup and by `doctor`.

## 4b. Slash commands

- **SK-80** Custom slash commands (`pkg/engine/commands`): Markdown files in `~/.blitz/commands/` (user) and the workspace's `.blitz/commands/`, `.claude/commands/`, `.agents/workflows/` (project, which win; prompt text, so no trust is needed — spec_parity_027 §12.3), read on each use. The name is the path without `.md`, subdirectories as `:` namespaces, lower case (`[a-z0-9][a-z0-9._-]*`). Optional frontmatter: `description` (else the body's first line), `argument-hint`, `agent`, `model`, `allowed-tools` (a list or a comma/space-separated string; `Bash(git *)` stays one entry), `mode: plan`; unknown modes and empty bodies are errors, reported once.
- **SK-81** Expansion: `$ARGUMENTS` is everything typed after the name, `$1`–`$9` its words; without either, the arguments are appended. The turn (`Turn.Command`) sends the expanded prompt and records the line as typed; `agent`/`model` apply to that turn only (ENG-16), `allowed-tools` limits it (`runtime.WithAllowedTools`; Claude Code names map to Blitz tools, `Bash(…)` counts as the shell tool), `mode: plan` plans it. `ErrUnknownCommand` / `UNKNOWN_COMMAND` otherwise.
- **SK-82** Every skill is a command `/<skill-name> <request>`, whose prompt carries the request and the skill's instructions (bundled and file commands of the same name win). Bundled commands: `/review` and `/security-review` (plan mode: read-only reviews of the uncommitted changes, a branch or files, verified findings with file and line), `/simplify` (behaviour-preserving cleanup of the changed files, then tests), `/verify` (find and run the project's build, lint and test commands and report). Built-in REPL commands win name clashes; `/help` lists the custom ones with their source; `ListCommands` serves them.

## 5. Agent tools

- **SK-30** `list_or_search_skills` (query optional) → name, description, tags.
- **SK-31** `activate_skill(skill_name)` → the full instructions and resources, plus each script with description, whether the policy allows it, and why not.
- **SK-32** `run_skill_script(skill, script, args?)` → `exit_code, stdout, stderr, timed_out, sandbox, tier, output_dir, output_files, error`. Registered only when skills are enabled.
- **SK-33** Engine: when skills exist, the root agent's instructions list them and suggest `activate_skill` ([spec_engine_016](spec_engine_016.md) ENG-02).

## 6. Running a script

- **SK-40** The script must be allowed by the policy. Approval by tier: tier ≥ 3 asks **every time** with no key (not rememberable); tiers 0–2 run without asking and are audited (`auto-tier_n`).
- **SK-41** Dependencies: an isolated environment per distinct (python, normalised deps, index, wheels-only) in `~/.blitz/envs/<key>`. Installing needs its own approval showing the exact `pip install --index-url … [--only-binary :all:] -- deps` command, rememberable (key `pyenv:<key>`) for exactly that list. Built inside the script sandbox with the network on and writes only to the environment and package cache, with `uv` if installed else `venv` + `pip`, from `packages.index` (default PyPI), wheels only by default. The environment is used only after its marker file is written (an interrupted build is rebuilt). The system Python is never modified. Build time is bounded.
- **SK-42** Environment passed: `PYTHONDONTWRITEBYTECODE=1`, `PYTHONNOUSERSITE=1`, `SKILL_DIR`, the policy-allowed variables, and at tier ≥ 2 (or bypass) `SKILL_OUTPUT`; plus a fixed PATH, private HOME and TMPDIR, LANG. Nothing else is inherited.
- **SK-43** Writes: tier 1 — nothing but its private `/tmp`; tier ≥ 2 and bypass — only `.blitz/skill-output/<skill>/<run>/` in the workspace. The workspace is readable. The agent reads the results there and applies changes with the file tools (so diffs, approvals, checkpoints and `/undo` work). Output files are listed in the result.
- **SK-46** TypeScript (BL-SK-01): the script runs on the system `node`, 22.6 or later (`SystemNode`; else refused with the reason), with its types stripped (`--experimental-strip-types --no-warnings`), from a temporary copy of the skill (or the inline code as `<name>.ts`) so it can import its neighbours. Its npm dependencies go into an environment per distinct set in `~/.blitz/node-envs/<key>` (`NodeEnvs`), built as Python's are (SK-41) with `npm install --ignore-scripts` (no package's code runs at install) from `[skills.policy.packages.npm] registry`, approved once per exact list (key `nodeenv:<key>`); a `node_modules` link beside the copy finds them. Each dependency must be a plain `name` or `name@version` (scoped names too; no tarballs, git, file or alias specs), in `npm.allow` when set, not in `npm.deny`, and with `npm.require_exact` an exact version. `entry_point` imports the module and calls the named export; a number it returns (or resolves to) is the exit code. The Python-only variables aren't set; the sandbox, writes, variables and timeout are as for Python. `/envs` lists Python's environments only.
- **SK-47** `execution_hints.writes_workspace: true` (BL-SK-02): the skill's scripts run in a copy of the workspace (its working directory; not `.git`, blocked paths, symbolic links or files over the size limit; refused past 512 MiB or 50,000 files). When the script succeeds, what it changed, added or deleted in the copy is shown as one diff in an approval asked every time and never remembered, as for tier 3 (`MustAsk`, kind `write`); approved, it is written through the workspace (`WriteFileAtomic`, `RemoveFile`), so it joins the turn's checkpoint and `/undo` reverts it, and `changed_files` lists it. Refused, or when the script fails or times out, nothing reaches the workspace. `activate_skill` says `writes_workspace`. The script's tier approval (SK-40) still applies before it runs.
- **SK-44** `entry_point`: the named function is called (module not run as `__main__`) and its return value is the exit code.
- **SK-45** Stopped at its timeout or on Ctrl+C.

## 7. Script sandbox (`skills.policy.sandbox`)

- **SK-50** `gvisor` (Linux): each run in its own rootless gVisor sandbox (`runsc` from PATH, `~/.blitz/bin` or `RUNSC_PATH`, beside its `gvisor-bin/`); root is empty, only host binaries/libraries, `/etc` and the request's paths are mounted, `/tmp` is a private tmpfs, home directories don't exist, network off unless allowed. Probed once with `/bin/true`; teardown uses its own context so a cancelled turn still cleans up. State in `~/.blitz/sandboxes`.
- **SK-51** `os`: Seatbelt/bubblewrap built per request — writes only to writable paths and a private temp, blocked paths hidden, network as allowed; rest of the filesystem readable.
- **SK-52** `auto` (default): gVisor if its test run works, else the OS sandbox (the reason is shown by `doctor`). **There is no unsandboxed fallback**: without a sandbox (e.g. Windows) scripts don't run (`ErrNoScriptBox`).

## 8. Environments (`/envs`)
- **SK-60** `/envs` lists key, packages, skills that used it, size, last use, ready; `/envs remove <key>`; `/envs prune` removes incomplete environments and those no policy-allowed script needs.

## 9. Diagnostics
- **SK-70** `/skills list|show <name>|search <q>`; `show` gives scripts, dependencies, tier, what the policy allows, and the content hash (for `trusted_hashes`). `doctor` reports unparseable skills, blocked scripts and the script sandbox.

## 10. Known gaps
`requires-python` via uv-managed interpreters; `storage_uri` and resources; a network field in Castor's proto.
