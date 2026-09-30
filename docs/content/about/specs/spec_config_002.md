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
- **CFG-05** Workspace-relative search paths (skills, agents) are resolved against the workspace, not the process CWD; the project's agents and skills load as prompt text, and its skills' scripts run only once the project's settings are trusted ([spec_project_config_031](spec_project_config_031.md)). Agent search paths are `~/.blitz/agents` and `./agents`. `blitz.trust_workspace` (only from the user's own settings) is deprecated: it trusts the project's settings for each run without recording it.

## 3. Schema and defaults

| Section | Key | Default | Notes |
|---|---|---|---|
| `[blitz]` | `default_agent` | `blitz` | |
| | `default_model` | `""` | overrides `llm.<provider>.model` |
| | `agency_level` | `high` | `low\|medium\|high\|extreme` |
| | `temperature` / `max_tokens` | `0.2` / `8192` | global generation settings |
| | `permission_mode` | `default` | `default\|accept-edits\|plan\|dont-ask\|bypass`; bypass needs the OS sandbox ([spec_approvals_005](spec_approvals_005.md) APR-14) |
| | `auto_approve` | `false` | older spelling of `permission_mode = "bypass"` |
| | `goal_max_continues` | 20 | how often a `/goal` sends the agent on before stopping ([spec_sessions_017](spec_sessions_017.md) SES-19) |
| | `plan_review` | `agent-decides` | `always` (every prompt is planned for approval first), `agent-decides` (the agent may call `enter_plan_mode`), `never` ([spec_workspace_018](spec_workspace_018.md) WS-45) |
| | `trust_workspace` | `false` | Deprecated: trusts the project's settings for each run (CFG-05) |
| `[llm]` | `provider` | `gemini` | `gemini\|anthropic\|openai\|ollama\|bedrock\|azure\|vertex-anthropic` |
| | `max_retries` | `3` | `0` disables |
| | `stall_timeout_seconds` | `600` | |
| | `fallback_models` | `[]` | `provider/model` or bare model |
| `[llm.gemini]` | `api_key`, `api_key_command`, `api_key_ttl` (5m), `model`, `auth` (`api_key` or `adc`), `project_id`, `location` | model `gemini-3.8-flash`; `auth` `api_key` | `adc`: [spec_models_015](spec_models_015.md) |
| `[llm.openai]` | `api_key`, `api_key_command`, `api_key_ttl`, `base_url`, `model` | `https://api.openai.com/v1`, `gpt-4o` | shared by `ollama` |
| `[llm.anthropic]` | `api_key`, `api_key_command`, `api_key_ttl`, `auth` (`api_key`, `oauth` or `adc`), `profile`, `project_id`, `location`, `model`, `base_url`, `fallbacks` | `claude-opus-5`, `default`; `auth` `api_key` | `oauth`, `adc`: [spec_models_015](spec_models_015.md) |
| `[llm.bedrock]` | `region`, `profile`, `model` | | Claude on Amazon Bedrock ([spec_models_015](spec_models_015.md) §3) |
| `[llm.azure]` | `resource`, `base_url`, `anthropic_base_url`, `api_key`, `auth` (`api_key` or `entra`), `model` | | OpenAI models and Claude on Azure ([spec_models_015](spec_models_015.md) §3) |
| `[skills]` | `enabled`, `paths`, `[skills.policy]` | on; `~/.blitz/skills`, `./skills`, `.agents/skills` | see [spec_skills_013](spec_skills_013.md) |
| `[workers]` | `enabled`, `paths`, `notify`, `notify_on`, `[workers.policy]` | on; `workers`; none; `failed`, `limited` | see [spec_workers_023](spec_workers_023.md) |
| `[tools]` | `shell_timeout_seconds` 120, `max_file_size_bytes` 10 MiB, `workspace_dir` `.`, `auto_approve_commands` false, `uc_tools_dir`, `approvals_file` `~/.blitz/approvals.json`, `max_parallel` 8, `max_background_agents` 4, `background_agent_timeout` `30m`, `background_agent_max_turns` 50, `background_agent_max_cost_usd` 0 (none) ([spec_background_agents_032](spec_background_agents_032.md)) | | |
| `[session]` | `storage_dir` `~/.blitz/sessions`, `auto_save` true | | |
| `[permissions]` | `allow`, `ask`, `deny` lists of rules; `read_only_defaults`; `[permissions.auto]` `model`, `environment` (the auto mode's reviewer, APR-14a) | `[]`; `true`; `""` | [spec_approvals_005](spec_approvals_005.md) APR-15; a workspace's lists add to the global ones (`PermissionsConfig.Merge`); not the same as `sandbox.commands.allow` (an allow-list) |
| `[sandbox]` | `allowed_paths`, `read_only_paths`, `blocked_paths` (defaults below), `shell_writable_paths` (`~/.cache`, `~/go/pkg/mod`, `~/.npm`), `shell` `auto`, `allow_network` true, `scrub_env` (defaults below) | | see [spec_filetools_006](spec_filetools_006.md), [spec_shell_007](spec_shell_007.md) |
| `[sandbox.commands]` | `allow`, `deny` (defaults below), `auto_approve` | | |
| `[ui]` | `markdown` true, `spinner` true, `terminal_title` true, `history_file` `~/.blitz/history`, `history_size` 1000, `diff_lines` 120, `theme` `auto`, `locale` `en-US`, `locales_dir` `~/.blitz/locales`, `style` (none), `notify_after` 30, `notify` `both`, `status_line` (none), `editor` `emacs`, `keybindings` `~/.blitz/keybindings.toml` | | `style`: [spec_memory_012](spec_memory_012.md) MEM-40 |
| `[images]` | `enabled` true, `dir` `~/.blitz/images`, `max_dimension` 1568, `max_input_mb` 20, `retain_days` 30 | | |
| `[memory]` | `enabled` true, `files` `["AGENTS.md","CLAUDE.md","GEMINI.md","BLITZ.md"]`, `local_files` `["CLAUDE.local.md","BLITZ.local.md"]`, `rule_dirs` `[".blitz/rules",".agents/rules",".claude/rules"]`, `global` `~/.blitz/BLITZ.md`, `global_rules` `~/.blitz/rules`, `max_bytes` 32 KiB, `auto` true (the agent's notes) | | see [spec_memory_012](spec_memory_012.md) |
| `[context]` | `compaction` true, `token_threshold` 120000, `retain_events` 20 | | |
| `[audit]` | `enabled` true, `dir` `~/.blitz/audit` | | |
| `[log]` | `level` `info`, `dir` `~/.blitz/logs`, `retain_days` 14 | | |
| `[telemetry]` | `enabled` false, `endpoint`, `capture_content` false | | |
| `[checkpoints]` | `enabled` true, `max_bytes` 64 MiB | | |
| `[web]` | `enabled` true, `allow_domains`, `deny_domains`, `allow_private` false, `max_bytes` 2 MiB, `timeout_seconds` 20, `search_provider`, `search_api_key`, `search_url`, `search_model`, `search_max_results` | | |
| `[browser]` | `enabled` true, `path` (found), `visible` false, `allow_local` false, `allow_scripts` false, `width` 1280, `height` 800 | | never from a project; see [spec_web_008](spec_web_008.md) §5 |
| `[plugins]` | `enable`, `disable` (installed plugins by name; disable wins) | | a project may disable, and enable once trusted; see [spec_plugins_033](spec_plugins_033.md) |
| `[network]` | `ca_file` (PEM certificate authorities to trust besides the system's) | | proxies from `HTTPS_PROXY`/`NO_PROXY`; never from a project ([spec_models_015](spec_models_015.md) MDL-54) |
| `[lsp.<language>]` | `command`, `extensions`, `disabled` | built in: go, typescript, python, rust | never from a project ([spec_filetools_006](spec_filetools_006.md) FS-61) |
| `[hooks]` | `pre_tool`, `post_tool`, `prompt_submit`, `session_start`, `session_end`, `stop`, `post_tool_failure`, `subagent_start`, `subagent_stop`, `pre_compact`, `post_compact`, `notification`, `permission_request` arrays of `{match, if, type, command, args, url, headers, allowed_env_vars, prompt, model, timeout_seconds, fail_closed}` | | [spec_hooks_010](spec_hooks_010.md) |
| `[[mcp.servers]]` | `name, command, args, env, url, tools, auto_approve, sandbox, prefix, agents, timeout_seconds` | | |
| `[pricing."<model>"]` | `input_per_mtok, output_per_mtok, cached_input_per_mtok, cache_write_per_mtok` | built-ins below | |
| `[search_pricing."<provider>"]` | `per_1k_queries` | `google` 14 | [spec_web_008](spec_web_008.md) WEB-15; not from a project |
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
- **CFG-12** `SkillPolicy.Problems()` reports: `min_hitl_tier` outside 0–3; unknown `sandbox`; unknown `network`; `network_allow` without `network = "allowlist"`; unknown languages (only `python`, `typescript`, both allowed by default); negative `max_timeout_seconds`.

## 4. Editing the file in place

Front ends change a few settings and persist them without rewriting the file.

- **CFG-20** `editConfigFile(dir, edit, verify)` reads `<dir>/.env.toml` (missing = empty), applies a line-based edit, refuses the result if it is not valid TOML or fails `verify` (re-decoded value must equal the intended one), then writes atomically (temp file + rename) preserving the existing mode (new files 0600, dir 0700).
- **CFG-21** Only the affected line changes: comments, ordering and encrypted values elsewhere are kept. Replacing a key preserves its trailing comment. A missing key is inserted after the table's last non-blank line; a missing table is appended.
- **CFG-22** Keys are written bare when they match `[A-Za-z0-9_-]+`, else quoted.
- **CFG-23** Editors: `SaveUILocale` (`[ui] locale`), `SaveAgentModel` (`[agent_models] <agent>`; empty ref removes the line), `UnpinAgentModel` (removes the line, or with masking writes `<agent> = ""`, which a workspace file uses to mask a global pin; the overlay merges `[agent_models]` agent by agent), `SaveModelSettings` (`[model_settings."<model>"]`: set keys written, unset removed, table header dropped when only blanks/comments remain, avoiding a double blank line).
- **CFG-24** Persisted edits always target the default config dir (`ConfigDir("")`), and the in-memory change applies even if saving fails (the failure is reported, not fatal).

## 5. Model settings values
- **CFG-30** Keys and ranges: `temperature` [0, 2]; `top_p` (0, 1]; `max_tokens` integer ≥ 1 and ≤ 2³¹−1; `seed` any int32. Empty value clears. Unknown keys are errors listing the known keys. Floats are always written with a decimal point.

## 6. Workspace settings and API keys

Added 2026-09-27 (owner's decisions: keys in the OS keychain; a workspace's own settings kept in `~/.blitz`, never in the workspace; the desktop app edits them through forms and as text).

- **CFG-40** A workspace's own settings are `<config dir>/workspaces/<base>-<8 hex>/.env.toml`, where `<base>` is the directory's name (characters outside `[A-Za-z0-9._-]` as `-`) and the hex the first 4 bytes of the SHA-256 of its absolute, symlink-free path (`WorkspaceSettingsDir`). CFG-01 stands: nothing is read from the workspace.
- **CFG-41** `LoadWorkspace(dir, workspace)` loads the global file, then the workspace's file over it (every key it sets wins; lists replace), then resolves secrets (CFG-43), then applies the environment (CFG-03). The service's opener and the CLI (`--dir`, else the current directory) use it; `Load` is the global scope alone.
- **CFG-42** Secrets are kept by `pkg/secrets`: the macOS Keychain (through `/usr/bin/security`, the secret passed on stdin, hex-encoded, never in arguments), the Secret Service on Linux (`secret-tool`, when one answers on the session bus), else an owner-only file (`<config dir>/secrets.toml`, 0600 in a 0700 directory), all under the service name `dev.blitz`. Names are `global/llm.<provider>.api_key` and `workspace/<settings dir name>/llm.<provider>.api_key`.
- **CFG-43** A settings value `keychain:<name>` refers to a stored secret. Loading replaces the providers' `api_key` references with the secrets (and decodes modenv's `xor:` values from a workspace file); a missing or unreadable secret leaves the key empty, so the environment, then the model's own error, takes over. `xor:` is obfuscation with a default key, not encryption.
- **CFG-44** `Describe(dir, workspace)` reports a scope without its keys: the file, `llm.provider` and `blitz.default_model` as the scope sets them, where keys are kept, and per provider (`gemini`, `anthropic`, `openai`) the key's source — `keychain`, `plain`, `obfuscated`, `environment`, `inherited` (a workspace using the global key) or `none` — whether a referenced secret is missing, `base_url` and `model`, and how it signs in as the scope sets it: `auth`, with `project_id` and `location` (`adc`) or `profile` (`oauth`).
- **CFG-45** Changes, each through CFG-20's editor, in the scope's file: `SetAPIKey` stores the key and writes the reference; `SecureAPIKey` moves a plain or obfuscated key from the file to the store; `RemoveAPIKey` removes the reference and the stored secret (a workspace then inherits); `SetValue` sets one of `llm.provider`, `blitz.default_model`, `llm.<provider>.model`, `llm.{anthropic,openai}.base_url`, `llm.gemini.{auth,project_id,location}`, `llm.anthropic.{auth,profile,project_id,location}` (`""` removes it, so a workspace follows the global setting; an `auth` the provider doesn't take is refused). Others are refused. `SetAuth` sets how a provider signs in (`ProviderAuth`: `api_key`; `adc`, Gemini and Anthropic, with its project and location; `oauth`, Anthropic only, with its profile), removing the fields left empty; back to `api_key` removes only `auth`. `SetProvider` saves the provider form as one change: `llm.provider`, `blitz.default_model` (`""` removes either) and, if given, how the provider signs in (`ProviderAuth`) and a new key for it (stored, and referred to), all checked before anything is written (an unknown provider, a key or sign-in for none or for `ollama`, or a key with a sign-in other than `api_key`, is refused) and the file written once.
- **CFG-46** `WriteSettingsFile` replaces a scope's file with text only if `CheckSettings` (CFG-48) finds no error; it returns its warnings, as "line N: …", for settings the configuration doesn't know (likely typos) and for API keys written as plain or obfuscated text. `ReadSettingsFile` returns the text (`""` when there's none).
- **CFG-47** The CLI: `blitz config permissions` (list; `allow|ask|deny <rule>`, `remove <rule>`, `defaults on|off|inherit`, `check <rule> [sample]`; `ScopePermissions`, `AddPermissionRule`, `RemovePermissionRule`, `SetReadOnlyDefaults`), `blitz config keys`, `set-key <provider>` (the key from stdin, without echo on a terminal), `remove-key <provider>`, `secure-key <provider>`, `set-auth <provider> <api_key|adc|oauth>` (`--project`, `--location`; `--profile`; `keys` shows the sign-in); `--workspace` (`-w`) works on the workspace's settings. The service's `ConfigService` ([spec_service_021](spec_service_021.md)) offers the same to the desktop app and reloads open workspaces.
- **CFG-48** `CheckSettings(text)` checks a settings file without saving it and returns its problems, by line, errors first on a line. Errors: TOML syntax (the parser's line and column; an error at the end of the last line is reported on it), a value of the wrong type (`<key>: incompatible types…`, at its line), an invalid permission rule (at the first line holding it). Warnings: settings the configuration doesn't know (at the line setting them, or their table's header, following `[table]`, `[[array]]` headers and dotted keys), API keys written as plain or obfuscated text, and skill policy settings it can't honour (`SkillPolicy.Problems`, at `[skills.policy]`). Line 0 means the file as a whole.
- **CFG-49** `Reference()` lists every setting: each table and each leaf of `Config`, in declaration order, keyed as written in a file (`llm.gemini.model`; `<agent>`, `<model>`, `<variable>`, else `<name>`, for map keys; `name[]` for arrays of tables), with its type, its default from `DefaultConfig` as TOML (the home directory as `~`; none for API keys, which may come from the environment, nor for maps and arrays of tables) and its description: the doc comment of its field, else the field's line comment, else the comment of the group of fields it is in (no blank line between; not for tables), with the table type's own doc comment added for tables. The descriptions are read from the declaring files, embedded in the binary (`config.go`, `features.go`, `modelsettings.go`), so they can't drift from the code; a test fails if any setting has none.
