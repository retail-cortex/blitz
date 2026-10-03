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
| [Agents and tools](agents.md) | The built-in agents and the tools they use |
| [Extending](extending.md) | Project memory, MCP servers, hooks, custom commands |
| [Skills](skills.md) | Agent Skills, the skills policy, sandboxed scripts |
| [Search](search.md) | Web search providers, `/search web` and `/search session` |
| [Images](images.md) | Attaching screenshots and pictures |
| [Language](language.md) | The interface and reply languages |
| [Logs and telemetry](telemetry.md) | The diagnostic log and OpenTelemetry |
| [GitHub Action](github-action.md) | Blitz in GitHub Actions: answering mentions, reviewing pull requests, issues into pull requests |
