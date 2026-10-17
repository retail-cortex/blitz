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
- **SK-11** Script: `name` (letters, digits, `. _ -`, unique), `language` (`python`/`typescript` or proto enum names; required), exactly one of `inline_code`, `storage_uri` (https, or `gs://bucket/object`; with `storage_sha256`, SK-49), `relative_path` (must stay inside the skill directory), `entry_point`, `dependencies`, `timeout_seconds ≥ 0`, `environment_variables` (valid names).
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
- **SK-82** Every skill is a command `/<skill-name> <request>`, whose prompt carries the request and the skill's instructions (bundled and file commands of the same name win). Bundled commands: `/review` and `/security-review` (plan mode: read-only reviews of the uncommitted changes, a branch or files, verified findings with file and line), `/simplify` (behaviour-preserving cleanup of the changed files, then tests), `/verify` (find and run the project's build, lint and test commands and report), `/grill-me <task>` (plan mode: the agent asks clarifying questions with `ask_user_question`, one at a time, until what's left wouldn't change the approach, then proposes one; it writes nothing, and the transcript records `/grill-me <task>`; BL-CLI-02), `/setup` and `/spec` (the harness and the specification, [spec_memory_012](spec_memory_012.md) MEM-22, MEM-42), `/skills-update` and `/agents-update` (MEM-44; `/skills update` and `/agents update` are the same, `commands.Alias`). Built-in REPL commands win name clashes; `/help` lists the custom ones with their source; `ListCommands` serves them.

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
- **SK-48** A Python script's interpreter requirement (BL-SK-03): its definition's `requires_python`, else `requires-python` in its PEP 723 metadata (`# /// script` … `# ///`), a PEP 440 specifier set of release versions (`>=3.11,<3.14`, `~=3.11`, `==3.12.*`). The system `python3` runs it when it meets it. Otherwise, with `uv` (on `PATH` or in `~/.blitz/bin`), an interpreter uv finds for it (`uv python find`, never downloading) does; when there's none, one is installed after an approval rememberable per requirement (key `uvpython:<spec>`), with `uv python install` inside the script sandbox (network on, writes only to uv's Python directory and cache). Without uv the script is refused, saying which Python it needs and which the system has. Environments (SK-41) are per interpreter, so each gets its own.
- **SK-49** `storage_uri` scripts (BL-SK-04): `https://…` as it is, `gs://bucket/object` through `https://storage.googleapis.com/bucket/object` (public objects). The definition must pin the content with `storage_sha256` (hex; Blitz's field, since Castor's proto has none; SKILL.md's content hash, and so `trusted_hashes`, covers it). A run fetches it under the web rules, as `web_fetch` does: `[web]` enabled and the sandbox's network, public addresses only (unless `allow_private`), `deny_domains`, and approval per host (`web:<host>`) unless `allow_domains` or an allow rule covers it; at most 10 MiB, no redirect to another host. A wrong hash refuses the run. The content is kept by its hash in `~/.blitz/skill-cache`, so later runs don't fetch; then it runs as inline code would.
- **SK-34** `activate_skill` also lists the skill's Castor `resources` as `assets`: id, name, description, category (`document_text`, …), MIME type, size, a URL to fetch (the public URL, else the `gcs_uri` through Cloud Storage's endpoint), inline content, the interpretation's summary, and its text when `auto_inject_context` is set (each at most 16 KiB).
- **SK-44** `entry_point`: the named function is called (module not run as `__main__`) and its return value is the exit code.
- **SK-45** Stopped at its timeout or on Ctrl+C.

## 7. Script sandbox (`skills.policy.sandbox`)

- **SK-50** `gvisor` (Linux): each run in its own rootless gVisor sandbox (`runsc` from PATH, `~/.blitz/bin` or `RUNSC_PATH`, beside its `gvisor-bin/`); root is empty, only host binaries/libraries, `/etc` and the request's paths are mounted, `/tmp` is a private tmpfs, home directories don't exist, network off unless allowed. Probed once with `/bin/true`; teardown uses its own context so a cancelled turn still cleans up. State in `~/.blitz/sandboxes`.
- **SK-51** `os`: Seatbelt/bubblewrap built per request — writes only to writable paths and a private temp, blocked paths hidden, network as allowed; rest of the filesystem readable.
- **SK-52** `auto` (default): gVisor if its test run works, else the OS sandbox (the reason is shown by `doctor`). **There is no unsandboxed fallback**: without a sandbox (e.g. Windows) scripts don't run (`ErrNoScriptBox`).

## 8. Environments (`/envs`)
- **SK-60** `/envs` lists key, packages, skills that used it, size, last use, ready; `/envs remove <key>`; `/envs prune` removes incomplete environments and those no policy-allowed script needs.

CI's Linux build runs the tests that install for real, which need the network (`BLITZ_PYENV_TESTS=1`, `BLITZ_UV_PYTHON`), with Node 22 and uv set up, and fails if any skips (BL-SK-06).

## 9. Diagnostics
- **SK-70** `/skills list|show <name>|search <q>` (and `install`, `registry`: SK-92, SK-96); `show` gives scripts, dependencies, tier, what the policy allows, and the content hash (for `trusted_hashes`). `doctor` reports unparseable skills, blocked scripts and the script sandbox.

## 9b. Registries (`blitz skills`, Settings › Skills)
*2026-10-14 (#36).* Skills installed from registries such as Castor (`pkg/skillreg`). A registry is an index anyone can serve; Castor serves it from its registered, hashed skills (its `CompiledReference` hash is an index entry's `sha256`).
- **SK-90** **The index** (version 1, JSON): `{"version": 1, "name": "...", "skills": [{"name", "description", "version", "archive_url", "sha256", "castor_id?"}]}`. `archive_url` is absolute or relative to the index; `sha256` is the archive's hash, hex. Another version is refused, naming it.
- **SK-91** **Registries** are an https URL (http only for localhost) or a local file, added with `blitz skills registry add <url> [--name n] [--token-stdin]` or Settings › Skills, checked by reading the index first, named after it unless named. They're kept in `~/.blitz/skill-registries.toml` (0600), a token in the system keychain (`skill-registry:<name>`), never in a file. The token goes as `Authorization: Bearer` to the registry's own host only, never to an archive elsewhere; errors name URLs without their query. Removing a registry forgets its token; its skills stay.
- **SK-92** **Install** (`blitz skills install <registry>/<name>|<name> [--project]`, `/skills install … [--project]`, **Install** in Settings): the archive (a .zip, at most 32 MB) must match `sha256`; it may hold at most 1,000 regular files and 64 MB unpacked, nothing outside it, no links; `SKILL.md` at its root or in its one folder must name the skill. It's unpacked beside the skills and swapped in, into `~/.blitz/skills/<name>` or `<workspace>/.agents/skills/<name>`, and recorded with its registry, version and hash. A folder of that name Blitz didn't install is never replaced (`ErrExists`); a name two registries offer must be qualified. `skills.policy` applies when it loads, as to any skill.
- **SK-93** **Update** (`blitz skills update [name…]`, **Update all**) reinstalls those whose registry now gives another hash, in place; **uninstall** removes only a skill it installed; **list** shows them.
- **SK-94** **Search** (`blitz skills search [words]`; `/skills search` adds "In your registries" when there are any; Settings) matches every word in the name or description across registries; one that can't be read is reported while the others answer.
- **SK-95** **Live**: the skills provider remembers a stamp of each folder and searches them again when one changes, so a skill installed, updated, edited or removed (from a registry, by `/skills update`, by the agent, in an editor) is seen by open sessions at its next look, without a restart. *2026-10-16:* the stamp is every `SKILL.md`'s path, size and modification time and its folder's time (it was the top folder's time alone, so a `SKILL.md` edited in place was missed until a restart); the look skips `.git`, `node_modules`, `.venv`, `venv` and `__pycache__`, which a skill may keep an environment in. Each turn looks (`Provider.Refresh`), and a change rebuilds the agent, whose prompt lists the skills; `/reload` searches again regardless (`Provider.Reload`) (spec_memory_012 MEM-25).
- **SK-96** **The desktop app**: Settings › Skills (advanced) lists the registries (with a key when one has a token; remove), adds one (index address and an optional token), finds skills and installs them for the user or the active workspace, and lists those installed with **Update all** and uninstall, through `ConfigService` (`ListSkillRegistries`, `AddSkillRegistry`, `RemoveSkillRegistry`, `SearchRegistrySkills`, `InstallRegistrySkill`, `UpdateRegistrySkills`, `UninstallRegistrySkill`; reasons `NO_REGISTRY`, `NOT_OFFERED`, `AMBIGUOUS`, `HASH_MISMATCH`, `SKILL_EXISTS`, `NOT_INSTALLED`, `REGISTRY_FAILED`). `/skills registry` opens it.
- **SK-96a** *Since v0.7.1 (the owner's request).* Settings is global, so Settings › Skills holds no workspace's skills: the registries come first (SK-96), then **Built in**, the skills Blitz ships with, by name, with their version and description (`ConfigService.ListBuiltinSkills`, from `skills.Builtins`, no workspace needed). A workspace's own skills are that workspace's (next).

## 10. Known gaps
A network field in Castor's proto.
