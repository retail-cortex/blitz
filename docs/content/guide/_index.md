---
title: Guide
weight: 30
---

How Blitz behaves, whichever program you drive it from. The CLI, the service and the desktop app share one engine and one set of settings, so everything here applies to all three unless a page says otherwise.

## Words

- A **workspace** is a folder you opened in Blitz, and what Blitz keeps for it: its chats, notes, search index and your private settings for it, all in `~/.blitz`, never in the folder. One owner holds it at a time (the service, or a CLI with `--local`).
- A **project** is what's in the repository: the code, and what's committed with it for agents, such as `.blitz/settings.toml` (project settings, which need your trust), `.agents/AGENTS.md`, and the project's agents, skills and commands.
- A **repository** is git's. A workspace is usually a repository's top folder, but can be a folder inside one.

So "workspace settings" are yours and private; "project settings" travel with the code and are trusted before they apply.

| Page | |
|---|---|
| [Configuration](configuration.md) | Where settings live, providers, API keys, per-workspace settings |
| [Safety](safety.md) | Approvals, permission modes and rules, the sandboxes, checkpoints, the audit log |
| [Models](models.md) | Retries, fallback models, per-agent models, per-model settings, cost |
| [Agents and tools](agents.md) | Using and switching agents, agency, a model per agent, writing your own, delegation, the tools |
| [MCP servers](mcp.md) | Adding a server's tools, its approvals, prompts and resources, and what to do when one misbehaves |
| [Databases](databases.md) | PostgreSQL, AlloyDB, SQLite (with vectors) and BigQuery for your agents, read-only unless you say otherwise |
| [Extending](extending.md) | Project memory, hooks, custom commands, plugins |
| [Jobs](workers.md) | Prompts on a schedule, unattended, with only the permissions you give them |
| [Workflows](workflows.md) | Steps run in order, each one's output the next one's input: `/name` in the chat, or built on a canvas |
| [Workspace health](health.md) | Studio's Overview: what the workspace holds, how its runs go, and each item's grade, judged on the evidence |
| [Evals](evals.md) | Cases that check your agents still answer as they should, from Studio or `blitz eval` in CI |
| [Skills](skills.md) | Agent Skills, the skills policy, sandboxed scripts |
| [Search](search.md) | Web search providers, `/search web` and `/search session` |
| [Images](images.md) | Attaching screenshots and pictures |
| [Language](language.md) | The interface and reply languages |
| [Logs and telemetry](telemetry.md) | The diagnostic log and OpenTelemetry |
| [GitHub Action](github-action.md) | Blitz in GitHub Actions: answering mentions, reviewing pull requests, issues into pull requests |
