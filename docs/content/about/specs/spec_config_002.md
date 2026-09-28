---
title: "002 · Config"
weight: 2
---

*Configuration* (`spec_config_002`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/config/config.go`, `features.go`, `locale.go`, `agentmodels.go`, `modelsettings.go`, `providers.go`; `pkg/secrets`; template and key commands in `apps/cli/configcmd.go`, `configkeys.go` |
| Tests | `pkg/config/*_test.go`, `pkg/secrets/secrets_test.go`, `apps/cli/configkeys_test.go`, `apps/service/internal/server/config_test.go` |
| Related | [spec_cli_020](spec_cli_020.md), [spec_models_015](spec_models_015.md) |

## 1. Purpose

One TOML file, `.env.toml`, loaded through `github.com/rrmcguinness/modenv` (hierarchical load plus secret decryption), configures everything. The file is trusted input: it can set API keys, base URLs and auto-approval, so only trusted locations are read.

## 2. Location and trust

- **CFG-01** The config directory is, in order: `--config DIR`, `$MODENV_PREFIX`, `~/.blitz`. The current working directory and the workspace are **never** consulted implicitly; a project `.env.toml` is ignored unless the user passes `--config .`.
  *Planned (decision 2026-09-26):* a restricted project layer, `.blitz/settings.toml`, with trust pinned to a content hash and never carrying credentials or endpoints — [spec_parity_027](spec_parity_027.md) §2.4. User configuration keeps this rule.
- **CFG-02** `Load` sets `MODENV_PREFIX` to the chosen directory and calls `modenv.Load` only when `<dir>/.env.toml` exists; otherwise defaults apply.
- **CFG-03** Environment fallbacks are applied after the file: `LLM_PROVIDER`; `GEMINI_API_KEY` then `GOOGLE_API_KEY` (only if no Gemini key); `OPENAI_API_KEY` (if empty); `OPENAI_BASE_URL`, `OPENAI_MODEL` (override); `ANTHROPIC_API_KEY` (if empty); `BLITZ_MODEL`, `BLITZ_AGENT`, `BLITZ_AGENCY` (override); `BLITZ_LOG_LEVEL`; `BLITZ_TELEMETRY` (`1/true/yes/on` enables, `0/false/no/off` disables).
- **CFG-04** `ExpandHome` expands only `~` and `~/…`; `~user` is left unchanged.
- **CFG-05** Workspace-relative search paths (skills, agents) are dropped unless `blitz.trust_workspace` is true (config or `--trust-workspace`), and are otherwise resolved against the workspace, not the process CWD. Agent search paths are `~/.blitz/agents` and `./agents`.

## 3. Schema and defaults

| Section | Key | Default | Notes |
|---|---|---|---|
| `[blitz]` | `default_agent` | `blitz` | |
| | `default_model` | `""` | overrides `llm.<provider>.model` |
| | `agency_level` | `high` | `low\|medium\|high\|extreme` |
| | `temperature` / `max_tokens` | `0.2` / `8192` | global generation settings |
| | `permission_mode` | `default` | `default\|accept-edits\|plan\|dont-ask\|bypass`; bypass needs the OS sandbox ([spec_approvals_005](spec_approvals_005.md) APR-14) |
| | `auto_approve` | `false` | older spelling of `permission_mode = "bypass"` |
| | `plan_review` | `agent-decides` | `always` (every prompt is planned for approval first), `agent-decides` (the agent may call `enter_plan_mode`), `never` ([spec_workspace_018](spec_workspace_018.md) WS-45) |
| | `trust_workspace` | `false` | |
| `[llm]` | `provider` | `gemini` | `gemini\|anthropic\|openai\|ollama` |
| | `max_retries` | `3` | `0` disables |
| | `stall_timeout_seconds` | `600` | |
| | `fallback_models` | `[]` | `provider/model` or bare model |
| `[llm.gemini]` | `api_key`, `model`, `project_id`, `location` | model `gemini-3.8-flash` | |
| `[llm.openai]` | `api_key`, `base_url`, `model` | `https://api.openai.com/v1`, `gpt-4o` | shared by `ollama` |
| `[llm.anthropic]` | `api_key`, `model`, `base_url`, `fallbacks` | `claude-opus-5`, `default` | |
| `[skills]` | `enabled`, `paths`, `[skills.policy]` | on; `~/.blitz/skills`, `./skills`, `.agents/skills` | see [spec_skills_013](spec_skills_013.md) |
| `[workers]` | `enabled`, `paths`, `[workers.policy]` | on; `workers` | see [spec_workers_023](spec_workers_023.md) |
| `[tools]` | `shell_timeout_seconds` 120, `max_file_size_bytes` 10 MiB, `workspace_dir` `.`, `auto_approve_commands` false, `uc_tools_dir`, `approvals_file` `~/.blitz/approvals.json`, `max_parallel` 8 | | |
| `[session]` | `storage_dir` `~/.blitz/sessions`, `auto_save` true | | |
| `[permissions]` | `allow`, `ask`, `deny` lists of rules | `[]` | [spec_approvals_005](spec_approvals_005.md) APR-15; not the same as `sandbox.commands.allow` (an allow-list) |
| `[sandbox]` | `allowed_paths`, `read_only_paths`, `blocked_paths` (defaults below), `shell_writable_paths` (`~/.cache`, `~/go/pkg/mod`, `~/.npm`), `shell` `auto`, `allow_network` true, `scrub_env` (defaults below) | | see [spec_filetools_006](spec_filetools_006.md), [spec_shell_007](spec_shell_007.md) |
| `[sandbox.commands]` | `allow`, `deny` (defaults below), `auto_approve` | | |
| `[ui]` | `markdown` true, `spinner` true, `terminal_title` true, `history_file` `~/.blitz/history`, `history_size` 1000, `diff_lines` 120, `theme` `auto`, `locale` `en-US`, `locales_dir` `~/.blitz/locales` | | |
| `[images]` | `enabled` true, `dir` `~/.blitz/images`, `max_dimension` 1568, `max_input_mb` 20, `retain_days` 30 | | |
| `[memory]` | `enabled` true, `files` `["AGENTS.md","CLAUDE.md","GEMINI.md","BLITZ.md"]`, `local_files` `["CLAUDE.local.md","BLITZ.local.md"]`, `rule_dirs` `[".blitz/rules",".agents/rules",".claude/rules"]`, `global` `~/.blitz/BLITZ.md`, `global_rules` `~/.blitz/rules`, `max_bytes` 32 KiB | | see [spec_memory_012](spec_memory_012.md) |
| `[context]` | `compaction` true, `token_threshold` 120000, `retain_events` 20 | | |
| `[audit]` | `enabled` true, `dir` `~/.blitz/audit` | | |
| `[log]` | `level` `info`, `dir` `~/.blitz/logs`, `retain_days` 14 | | |
| `[telemetry]` | `enabled` false, `endpoint`, `capture_content` false | | |
| `[checkpoints]` | `enabled` true, `max_bytes` 64 MiB | | |
| `[web]` | `enabled` true, `allow_domains`, `deny_domains`, `allow_private` false, `max_bytes` 2 MiB, `timeout_seconds` 20, `search_provider`, `search_api_key`, `search_url`, `search_model`, `search_max_results` | | |
| `[hooks]` | `pre_tool`, `post_tool`, `prompt_submit`, `session_start`, `session_end`, `stop`, `post_tool_failure`, `subagent_start`, `subagent_stop`, `pre_compact`, `post_compact`, `notification`, `permission_request` arrays of `{match, command, timeout_seconds, fail_closed}` | | [spec_hooks_010](spec_hooks_010.md) |
| `[[mcp.servers]]` | `name, command, args, env, url, tools, auto_approve, sandbox, prefix, agents, timeout_seconds` | | |
| `[pricing."<model>"]` | `input_per_mtok, output_per_mtok, cached_input_per_mtok, cache_write_per_mtok` | built-ins below | |
| `[agent_models]` | `agent = "provider/model"` | | |
| `[model_settings."<model>"]` | `temperature, max_tokens, top_p, seed, reasoning_effort, thinking_budget` | | `reasoning_effort`: `minimal\|low\|medium\|high\|max`; `thinking_budget` ≥ 0 |

Built-in lists:
- Blocked paths: `.env`, `.env.local`, `.env.*.local`, `.env.toml`, `.env.*.toml`, `*.pem`, `*.key`, `*.p12`, `id_rsa*`, `id_ecdsa*`, `id_ed25519*`, `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.config/gcloud`, `~/.azure`, `~/.kube`, `~/.docker/config.json`, `~/.netrc`.
- Scrubbed env: `*_API_KEY`, `*_API_TOKEN`, `*_SECRET`, `*_SECRET_KEY`, `*_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `GOOGLE_APPLICATION_CREDENTIALS`, `MODENV_*`.
- Denied commands: `sudo *`, `su *`, `doas *`, `shutdown *`, `reboot *`, `halt *`, `mkfs *`, `mkfs.*`, `diskutil erase*`.
- Pricing (USD per 1M tokens, input/output/cached/cache-write): `gemini-3.8-flash` 0.75/3.75/0.075 (introductory until 2026-12-31; 1.50/7.50/0.15 from 2027-01-01, see CFG-13), `gemini-2.5-pro` 1.25/10/0.31, `gpt-4o` 2.50/10/1.25, `claude-opus-5` 5/25/0.50/6.25, `claude-sonnet-5` 2/10/0.20/2.50, `claude-haiku-4-5` 1/5/0.10/1.25.

- **CFG-10** `Config.ModelName()` returns `blitz.default_model` if set, else the active provider's `model` (`openai` and `ollama` share `[llm.openai]`).
- **CFG-11** Setting a list (e.g. `blocked_paths`, `commands.deny`) **replaces** the default list.
- **CFG-13** Announced price changes are data, not reminders: `PriceChanges` lists `{model, from (UTC), price}`, and `DefaultPricingAt(t)` applies every change in effect at `t` (the latest per model) to a copy of `DefaultPricing`. `DefaultConfig` uses the prices in effect when the configuration loads, so a long-running service picks up a change at its next restart. `[pricing]` entries still override.
- **CFG-12** `SkillPolicy.Problems()` reports: `min_hitl_tier` outside 0–3; unknown `sandbox`; unknown `network`; `network_allow` without `network = "allowlist"`; unknown languages (only `python`, `typescript`); negative `max_timeout_seconds`.

## 4. Editing the file in place

Front ends change a few settings and persist them without rewriting the file.

- **CFG-20** `editConfigFile(dir, edit, verify)` reads `<dir>/.env.toml` (missing = empty), applies a line-based edit, refuses the result if it is not valid TOML or fails `verify` (re-decoded value must equal the intended one), then writes atomically (temp file + rename) preserving the existing mode (new files 0600, dir 0700).
- **CFG-21** Only the affected line changes: comments, ordering and encrypted values elsewhere are kept. Replacing a key preserves its trailing comment. A missing key is inserted after the table's last non-blank line; a missing table is appended.
- **CFG-22** Keys are written bare when they match `[A-Za-z0-9_-]+`, else quoted.
- **CFG-23** Editors: `SaveUILocale` (`[ui] locale`), `SaveAgentModel` (`[agent_models] <agent>`; empty ref removes the line), `SaveModelSettings` (`[model_settings."<model>"]`: set keys written, unset removed, table header dropped when only blanks/comments remain, avoiding a double blank line).
- **CFG-24** Persisted edits always target the default config dir (`ConfigDir("")`), and the in-memory change applies even if saving fails (the failure is reported, not fatal).

## 5. Model settings values
- **CFG-30** Keys and ranges: `temperature` [0, 2]; `top_p` (0, 1]; `max_tokens` integer ≥ 1 and ≤ 2³¹−1; `seed` any int32. Empty value clears. Unknown keys are errors listing the known keys. Floats are always written with a decimal point.

## 6. Workspace settings and API keys

Added 2026-09-27 (owner's decisions: keys in the OS keychain; a workspace's own settings kept in `~/.blitz`, never in the workspace; the desktop app edits them through forms and as text).

- **CFG-40** A workspace's own settings are `<config dir>/workspaces/<base>-<8 hex>/.env.toml`, where `<base>` is the directory's name (characters outside `[A-Za-z0-9._-]` as `-`) and the hex the first 4 bytes of the SHA-256 of its absolute, symlink-free path (`WorkspaceSettingsDir`). CFG-01 stands: nothing is read from the workspace.
- **CFG-41** `LoadWorkspace(dir, workspace)` loads the global file, then the workspace's file over it (every key it sets wins; lists replace), then resolves secrets (CFG-43), then applies the environment (CFG-03). The service's opener and the CLI (`--dir`, else the current directory) use it; `Load` is the global scope alone.
- **CFG-42** Secrets are kept by `pkg/secrets`: the macOS Keychain (through `/usr/bin/security`, the secret passed on stdin, hex-encoded, never in arguments), the Secret Service on Linux (`secret-tool`, when one answers on the session bus), else an owner-only file (`<config dir>/secrets.toml`, 0600 in a 0700 directory), all under the service name `dev.blitz`. Names are `global/llm.<provider>.api_key` and `workspace/<settings dir name>/llm.<provider>.api_key`.
- **CFG-43** A settings value `keychain:<name>` refers to a stored secret. Loading replaces the providers' `api_key` references with the secrets (and decodes modenv's `xor:` values from a workspace file); a missing or unreadable secret leaves the key empty, so the environment, then the model's own error, takes over. `xor:` is obfuscation with a default key, not encryption.
- **CFG-44** `Describe(dir, workspace)` reports a scope without its keys: the file, `llm.provider` and `blitz.default_model` as the scope sets them, where keys are kept, and per provider (`gemini`, `anthropic`, `openai`) the key's source — `keychain`, `plain`, `obfuscated`, `environment`, `inherited` (a workspace using the global key) or `none` — whether a referenced secret is missing, `base_url` and `model`.
- **CFG-45** Changes, each through CFG-20's editor, in the scope's file: `SetAPIKey` stores the key and writes the reference; `SecureAPIKey` moves a plain or obfuscated key from the file to the store; `RemoveAPIKey` removes the reference and the stored secret (a workspace then inherits); `SetValue` sets one of `llm.provider`, `blitz.default_model`, `llm.<provider>.model`, `llm.{anthropic,openai}.base_url`, `llm.gemini.{project_id,location}` (`""` removes it, so a workspace follows the global setting). Others are refused.
- **CFG-46** `WriteSettingsFile` replaces a scope's file with text only if it decodes into the configuration; it returns warnings for settings the configuration doesn't know (likely typos) and for API keys written as plain or obfuscated text. `ReadSettingsFile` returns the text (`""` when there's none).
- **CFG-47** The CLI: `blitz config keys`, `set-key <provider>` (the key from stdin, without echo on a terminal), `remove-key <provider>`, `secure-key <provider>`; `--workspace` (`-w`) works on the workspace's settings. The service's `ConfigService` ([spec_service_021](spec_service_021.md)) offers the same to the desktop app and reloads open workspaces.
