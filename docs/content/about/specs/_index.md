---
title: "Specifications"
weight: 40
geekdocCollapseSection: true
---

Reverse-engineered from the source at commit `53f8c53`. Specs 001–025 describe behaviour as implemented, with numbered requirements (`CLI-01`, `FS-30`, …) that can be traced to code and tests, plus known gaps; 026 and 027 collect what's missing — Blitz's own gaps, and gaps against Claude Code and Antigravity — as requirements to close before new features.

Files are named `spec_NNN_<name>.md`, where `NNN` is the order of execution (first, so the files list in order): each spec builds on the specs before it, so reading (or rebuilding) them in order works bottom-up, from project setup to release. Links to later specs are cross-references for context, not dependencies.

| # | Spec | Covers |
|---|---|---|
| 001 | [setup](spec_001_setup.md) | Identity, layout, toolchain, commands, conventions |
| 002 | [config](spec_002_config.md) | `.env.toml` location and trust, schema and defaults, in-place editing |
| 003 | [observability](spec_003_observability.md) | Redaction, audit log, diagnostic log, OpenTelemetry |
| 004 | [i18n](spec_004_i18n.md) | Catalogs, locale resolution, interface vs reply language |
| 005 | [approvals](spec_005_approvals.md) | Approval gate, remembered/saved rules, unattended runs, `ask_user_question` |
| 006 | [filetools](spec_006_filetools.md) | File sandbox, blocked paths, file tools, `apply_patch`, checkpoints and `/undo` |
| 007 | [shell](spec_007_shell.md) | `run_shell_command`, command policy, OS sandbox, process guard, background processes |
| 008 | [web](spec_008_web.md) | `web_fetch` (SSRF-safe), `web_search` providers, `/search` |
| 009 | [mcp](spec_009_mcp.md) | MCP servers: exposure, approval, timeouts, circuit breaker |
| 010 | [hooks](spec_010_hooks.md) | `pre_tool`, `post_tool`, `prompt_submit` hooks |
| 011 | [images](spec_011_images.md) | Attaching, preparing, storing and sending images |
| 012 | [memory](spec_012_memory.md) | Project instruction files |
| 013 | [skills](spec_013_skills.md) | Skill definitions, skills policy, script sandbox and environments |
| 014 | [agents](spec_014_agents.md) | Agent definitions, built-ins, delegation, Universal Constructor |
| 015 | [models](spec_015_models.md) | Providers, retries and stalls, fallback chain, per-model settings |
| 016 | [engine](spec_016_engine.md) | Turns, plan/read-only modes, steering, `/btw`, compaction, sub-agents, usage, tracing |
| 017 | [sessions](spec_017_sessions.md) | Transcripts, event log, resume, snapshots, session search |
| 018 | [workspace](spec_018_workspace.md) | `engine.Open`, `api.Backend`, turn lifecycle, typed operations |
| 019 | [tui](spec_019_tui.md) | REPL, slash commands, steering, exit handling |
| 020 | [cli](spec_020_cli.md) | Commands, flags, one-shot output formats, exit codes, `doctor`, `config` |
| 021 | [service](spec_021_service.md) | `blitzd`, socket, Connect API, approval broker, login item |
| 022 | [client](spec_022_client.md) | Attaching front ends to the service |
| 023 | [jobs](spec_023_workers.md) | `WORKER.md`, schedules, permissions, enabling, runs, scheduler |
| 024 | [desktop](spec_024_desktop.md) | The desktop app and its page (the Wails shell until v0.6.5; the Electron app since, spec 038) |
| 025 | [release](spec_025_release.md) | CI, reproducible and signed releases |
| 026 | [backlog](spec_026_backlog.md) | What's missing: unimplemented requirements, verification still owed, and deliberate limitations |
| 027 | [parity](spec_027_parity.md) | Gaps against Claude Code, Antigravity CLI and Antigravity, as requirements to close before new features |
| 028 | [monorepo](spec_028_monorepo.md) | Apps over shared packages, their dependency rules, and the Bazel build: generated protos, the page, packages, reproducibility |
| 029 | [files](spec_029_files.md) | The workspace's files in the desktop app: explorer, editor, diffs, language intelligence |
| 030 | [release readiness](spec_030_release_readiness.md) | Licensing, headers and notices; comments; specs, docs site, README, contributing and owners |
| 031 | [project configuration](spec_031_project_config.md) | `.blitz/settings.toml` in a workspace: tiers of what applies at once, after hash-pinned trust, or never |
| 032 | [background sub-agents](spec_032_background_agents.md) | `invoke_agent` in the background: tasks, their tools, results, the task view and approvals |
| 033 | [plugins](spec_033_plugins.md) | Bundles of skills, commands, agents, hooks and MCP servers: installing, pinning by hash, marketplaces, importing Claude Code and Gemini CLI plugins |
| 034 | [vscode](spec_034_vscode.md) | The VS Code extension: the desktop app's chat in a view, attached to the service through a local proxy, approvals in the diff view, the selection and files as context |
| 035 | [search](spec_035_search.md) | Workspace search: an index of the files, PDFs and notebooks, chats and notes, kept up by scans, searched by words (FTS5, BM25) from the desktop app, the CLI and the agent |
| 036 | [windows](spec_036_windows.md) | Blitz on Windows: the tray runs the service; until v0.7.1 it also served the desktop page to an Edge app window |
| 037 | [visual editor](spec_037_visual_editor.md) | Draft: Markdown edited in its rendered form (TipTap), code blocks as CodeMirror editors, and language servers (gopls first) for the editor through a `LanguageService` on the service |
| 038 | [electron](spec_038_electron.md) | The desktop app in Electron (one engine on every platform, a real app on Windows) over the page and `blitz-tray --serve` as its host; since v0.7.0 |
| 039 | [secrets](spec_039_secrets.md) | The user's secrets in the system's keychain, named in prompts and settings as `{secrets:key}`; since v0.7.1 |
| 040 | [studio](spec_040_studio.md) | What a workspace's .agents folder holds, each kind listed and edited in the desktop app; since v0.7.1 |
| 041 | [gates](spec_041_gates.md) | Rules that gate an agent's turn, checked before it runs and after it answers, and guardrails at every step of it; since v0.7.1 |
| 042 | [evals](spec_042_evals.md) | Cases that check an agent still answers as it should, run and scored exactly or by meaning; since v0.7.1 |
| 043 | [workflows](spec_043_workflows.md) | Steps joined into a graph, run as one turn (/name) and built on a canvas; since v0.7.1 |
| 044 | [health](spec_044_health.md) | A workspace's dashboard: what it holds, how its runs go, and each item's quality judged on the evidence, on request and nightly; since v0.7.1 |
| 045 | [composer](spec_045_composer.md) | A guided conversation that makes a component: asks what it needs, writes the file, checks it with the engine's rules and hands it to the editor as a draft; for workflows since v0.7.1, the other kinds next |
| 046 | [databases](spec_046_databases.md) | Databases the agents can query (PostgreSQL, AlloyDB, SQLite with sqlite-vec, BigQuery), served in-process by MCP Toolbox for Databases, read-only by default; since v0.8.0 |
| 047 | [connectors](spec_047_connectors.md) | The systems the agents reach, databases first: categories and kinds as descriptors, backends behind one interface, capabilities such as the query builder; since v0.8.0 |
| 048 | [harness learning](spec_048_harness_learning.md) | Draft: experience drawn from a workspace's runs (`.agents/EXPERIENCE.md`), run evidence in `/skills update` and `/agents update`, invariant and project experience apart, guardrails and rules in `/setup`; after Harness² |
| 049 | [browser profiles](spec_049_browser_profiles.md) | The sign-ins the agents browse with: Blitz's own Chromium profiles, one chosen per workspace from the person's settings, kept between runs; since v0.8.4 |
| 050 | [the agent's browser in the app](spec_050_agent_browser.md) | With the desktop app open, the agent browses in a view of the app's own, watched in a tab or silent, over a relay of the DevTools Protocol; since v0.8.4 |
| 051 | [secrets hook](spec_051_secret_hooks.md) | A pre-commit hook that stops commits adding API keys and other secrets (`blitz security scan-secrets`), put in when the harness or the window makes a repository; since v0.8.4 |
| 052 | [processes](spec_052_processes.md) | The agents' background processes: what each uses, flags for the busy, the growing and the left running, and one place to see and stop them (the window's status bar and view, `/processes`, the tray); since v0.8.5 |
| 053 | [enterprise foundations](spec_053_enterprise.md) | Draft: the service assembled from `pkg/` with extension points, and clients that connect to an organization's licensed service: its sign-in, its models, its signed catalog and its jobs |
| 054 | [active installs](spec_054_active_installs.md) | Once a day, on a day someone uses Blitz, a ping counts the install as active (front end, version, platform) with a random install ID: daily, weekly and monthly actives and retention. On by default, `[usage] count = false` turns it off |
| 055 | [resource names](spec_055_urns.md) | one URN for everything an organization's service keeps (`urn:blitz:runs:<id>`, `urn:blitz:artifacts:<workspace>:<job>:<run>/<path>`), made only of IDs, mapped to its store by a locator and asked about at `/api/v1/resources/{urn}` |

Background and history: [the roadmap](../roadmap.md), [where to pick up](../../development/next-steps.md), [development](../../development/_index.md).
