# Blitz specifications

Reverse-engineered from the source at commit `53f8c53`. Specs 001–025 describe behaviour as implemented, with numbered requirements (`CLI-01`, `FS-30`, …) that can be traced to code and tests, plus known gaps; 026 and 027 collect what's missing — Blitz's own gaps, and gaps against Claude Code and Antigravity — as requirements to close before new features.

Files are named `spec_<name>_NNN.md`, where `NNN` is the order of execution: each spec builds on the specs before it, so reading (or rebuilding) them in order works bottom-up, from project setup to release. Links to later specs are cross-references for context, not dependencies.

| # | Spec | Covers |
|---|---|---|
| 001 | [setup](spec_setup_001.md) | Identity, layout, toolchain, commands, conventions |
| 002 | [config](spec_config_002.md) | `.env.toml` location and trust, schema and defaults, in-place editing |
| 003 | [observability](spec_observability_003.md) | Redaction, audit log, diagnostic log, OpenTelemetry |
| 004 | [i18n](spec_i18n_004.md) | Catalogs, locale resolution, interface vs reply language |
| 005 | [approvals](spec_approvals_005.md) | Approval gate, remembered/saved rules, unattended runs, `ask_user_question` |
| 006 | [filetools](spec_filetools_006.md) | File sandbox, blocked paths, file tools, `apply_patch`, checkpoints and `/undo` |
| 007 | [shell](spec_shell_007.md) | `run_shell_command`, command policy, OS sandbox, process guard, background processes |
| 008 | [web](spec_web_008.md) | `web_fetch` (SSRF-safe), `web_search` providers, `/search` |
| 009 | [mcp](spec_mcp_009.md) | MCP servers: exposure, approval, timeouts, circuit breaker |
| 010 | [hooks](spec_hooks_010.md) | `pre_tool`, `post_tool`, `prompt_submit` hooks |
| 011 | [images](spec_images_011.md) | Attaching, preparing, storing and sending images |
| 012 | [memory](spec_memory_012.md) | Project instruction files |
| 013 | [skills](spec_skills_013.md) | Skill definitions, skills policy, script sandbox and environments |
| 014 | [agents](spec_agents_014.md) | Agent definitions, built-ins, delegation, Universal Constructor |
| 015 | [models](spec_models_015.md) | Providers, retries and stalls, fallback chain, per-model settings |
| 016 | [engine](spec_engine_016.md) | Turns, plan/read-only modes, steering, `/btw`, compaction, sub-agents, usage, tracing |
| 017 | [sessions](spec_sessions_017.md) | Transcripts, event log, resume, snapshots, session search |
| 018 | [workspace](spec_workspace_018.md) | `engine.Open`, `api.Backend`, turn lifecycle, typed operations |
| 019 | [tui](spec_tui_019.md) | REPL, slash commands, steering, exit handling |
| 020 | [cli](spec_cli_020.md) | Commands, flags, one-shot output formats, exit codes, `doctor`, `config` |
| 021 | [service](spec_service_021.md) | `blitzd`, socket, Connect API, approval broker, login item |
| 022 | [client](spec_client_022.md) | Attaching front ends to the service |
| 023 | [workers](spec_workers_023.md) | `WORKER.md`, schedules, permissions, enabling, runs, scheduler |
| 024 | [desktop](spec_desktop_024.md) | Wails desktop app |
| 025 | [release](spec_release_025.md) | CI, reproducible and signed releases |
| 026 | [backlog](spec_backlog_026.md) | What's missing: unimplemented requirements, verification still owed, and deliberate limitations |
| 027 | [parity](spec_parity_027.md) | Gaps against Claude Code, Antigravity CLI and Antigravity, as requirements to close before new features |
| 028 | [monorepo](spec_monorepo_028.md) | Apps over shared packages, their dependency rules, and the Bazel build: generated protos, the page, packages, reproducibility |
| 029 | [files](spec_files_029.md) | The workspace's files in the desktop app: explorer, editor, diffs, language intelligence |

Background and history: [../ROADMAP.md](../ROADMAP.md), [../NEXT_STEPS.md](../NEXT_STEPS.md), [../AGENTS.md](../AGENTS.md).
