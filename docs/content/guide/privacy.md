---
title: Privacy
weight: 105
---

Spec: [active installs](../about/specs/spec_active_installs_054.md).

Blitz runs on your machine and keeps your work there. What leaves it goes where you send it, plus one small check for updates.

## Where Blitz connects

- **The model providers you set up**: your messages, the files and tool results the agent reads, and images you attach, to the provider of the model you use (OpenAI, Anthropic, Google, a local model, …), under that provider's terms.
- **The web**: pages the agent fetches, searches through the search provider you choose, and sites you open in the desktop app's preview.
- **Connectors and MCP servers you add**: what the agent sends them.
- **Telemetry, if you turn it on**: traces and logs to the collector you name ([Logs and telemetry](telemetry.md)). It's off by default.
- **Updates**: the release's files from GitHub, and the trust root their signatures are checked with from Sigstore.
- **Counting active installs**: below.

Nothing else: no analytics, no crash reports, no usage of your workspaces, agents or models.

## Counting active installs

On a day you use Blitz, it tells Retail Cortex once that this install was in use, so Retail Cortex can count how many installs are active each day and month, and on which versions and platforms. It's sent the first time each day you open the desktop app's window, run a `blitz` command (not `--version` or help), or start a session in the VS Code extension: at most one message a day for each of those you use. If you don't use Blitz on a day, nothing is sent; the Blitz service running in the background isn't use. Each message is a request to `license.retailcortex.com`, with exactly this:

| | Example |
|---|---|
| A random ID for this install, made on this machine and kept in `~/.blitz/install-id` | `9f2c…e41a` |
| The day it was used | `2026-11-04` |
| Which front end: the desktop app, the CLI or VS Code | `desktop` |
| Blitz's version | `v0.9.0` |
| The operating system and processor | `darwin`, `arm64` |

Nothing about what you did: no prompts, files, workspaces, models, providers or account. The ID is random: it isn't made from your machine, your name, your account or your network address, and nothing else Blitz sends carries it. Delete the file and the next message makes a new one. If you're offline, the message waits in `~/.blitz/activity.json` and goes later, for up to three days. Retail Cortex keeps each message for 13 months after an install's last, uses only counts made from them (active installs by day, week and month, by front end, version and platform, and how many new installs come back), and doesn't keep your network address.

To turn it off, add this to `~/.blitz/.env.toml`:

```toml
[usage]
count = false
```

Blitz then sends nothing, and doesn't make or read the ID. Checking for updates is separate: the desktop app and `blitz update` ask GitHub for the latest release, with nothing attached.
