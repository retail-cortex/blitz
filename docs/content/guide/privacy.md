---
title: Privacy
weight: 105
---

Spec: [update checks](../about/specs/spec_update_checks_054.md).

Blitz runs on your machine and keeps your work there. What leaves it goes where you send it, plus one small check for updates.

## Where Blitz connects

- **The model providers you set up**: your messages, the files and tool results the agent reads, and images you attach, to the provider of the model you use (OpenAI, Anthropic, Google, a local model, …), under that provider's terms.
- **The web**: pages the agent fetches, searches through the search provider you choose, and sites you open in the desktop app's preview.
- **Connectors and MCP servers you add**: what the agent sends them.
- **Telemetry, if you turn it on**: traces and logs to the collector you name ([Logs and telemetry](telemetry.md)). It's off by default.
- **Updates**: the release's files from GitHub, and the trust root their signatures are checked with from Sigstore.
- **The update check**: below.

Nothing else: no analytics, no crash reports, no usage of your workspaces, agents or models.

## The update check

When the desktop app opens (at most every five days, unless **Update automatically** is off), when you press **Check now**, and when you run `blitz update`, Blitz asks Retail Cortex for the latest release at `license.retailcortex.com`. That check is how Retail Cortex counts how many installs are in use. It sends exactly this:

| | Example |
|---|---|
| A random ID for this install, made on this machine and kept in `~/.blitz/install-id` | `9f2c…e41a` |
| Blitz's version | `v0.8.11` |
| The operating system and processor | `darwin`, `arm64` |
| What's asking: the desktop app or the CLI | `desktop` |

The ID is random: it isn't made from your machine, your name, your account or your network address, and nothing else Blitz sends carries it. Delete the file and the next check makes a new one. Retail Cortex keeps each check for 13 months, uses only counts made from them (installs in use, by version and platform), and doesn't keep your network address.

If Retail Cortex doesn't answer, Blitz asks GitHub instead. What's downloaded is always the release on GitHub, checked against its signature.

To stop counting, add this to `~/.blitz/.env.toml`:

```toml
[updates]
count = false
```

Blitz then asks GitHub directly, with nothing but the request for the latest release, and doesn't make or read the ID.
