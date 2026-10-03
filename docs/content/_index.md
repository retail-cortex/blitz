---
title: Blitz
geekdocNav: true
geekdocBreadcrumb: false
geekdocAlign: left
---

**The zero-gimmick, high-performance Go coding agent.**

Blitz reads your workspace, makes the change, checks it, and gets out of the way: no persona, no filler, no chatter. It is written in Go on Google's Agent Development Kit and runs Gemini, Claude, OpenAI-compatible or Ollama models.

```bash
blitz "add unit tests for user_service.go"   # one prompt, then exit
blz -d ~/src/api                             # a session in another project
```

## Three programs, one engine

| Program | |
|---|---|
| [**`blitz`**, the CLI](products/cli.md) | An interactive session in the terminal, or a one-shot run for scripts and CI. A single static binary that starts in about 20 ms. |
| [**`blitzd`**, the service](products/service.md) | A per-user service holding every workspace you open, reached over a Unix socket only you can open. It runs scheduled workers and lets the CLI and the desktop app share sessions. |
| [**Blitz**, the desktop app](products/desktop.md) | A window onto the service, laid out like an IDE: the workspace's files, an editor, and the agent's chat. |

All three run the same engine, so a session started in the terminal can be picked up in the desktop app. The [architecture](architecture/_index.md) explains how they fit together.

## Why Blitz

- **Compiled Go.** Instant start, low memory, no runtime to install. See [performance](about/performance.md).
- **Safe by default.** Commands run in an OS sandbox, edits need your approval unless you've allowed them, and every turn can be undone. See [the safety model](guide/safety.md).
- **Works with what you have.** `AGENTS.md`, `CLAUDE.md` and `GEMINI.md` load as project instructions; MCP servers, hooks, custom commands and Agent Skills all work.
- **Scriptable.** JSON and streaming JSON output, limits on turns, cost and time, and distinct exit codes.
- **Open.** Apache License 2.0. Every requirement is written down in the [specifications](about/specs/_index.md).

## Where to go next

- New to Blitz: [Getting started](getting-started/_index.md), then a [tutorial](tutorials/_index.md) from an empty folder to working software.
- Using it: [the products](products/_index.md) and the [guide](guide/_index.md).
- Changing it: [architecture](architecture/_index.md), [shared packages](packages/_index.md) and [development](development/_index.md).
