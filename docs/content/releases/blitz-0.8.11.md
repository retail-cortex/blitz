---
title: Blitz 0.8.11
weight: 1
---

**Counting who uses Blitz, in the open.** From this release, on a day you use Blitz, it tells Retail Cortex once that your install was in use: a random ID, the day, whether it was the desktop app, the CLI or VS Code, the version and your platform. Nothing about your work: no prompts, files, workspaces, models, providers or account. It's how we'll see whether Blitz is growing, which versions people run, and where to put our effort. One line in your settings turns it off.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.8.11).
2. There's nothing to set up. To see exactly what's sent, and to whom, read [Privacy](../guide/privacy.md). To turn it off, add this to `~/.blitz/.env.toml`:

   ```toml
   [usage]
   count = false
   ```

### What's new

**One small message a day, only when you use Blitz.** The first time each day you open the desktop app's window, run a `blitz` command, or start a session in the VS Code extension, Blitz sends one message to `license.retailcortex.com`. If you don't use Blitz on a day, nothing is sent. The service running in the background, scheduled workers and update checks don't count as use. [Privacy](../guide/privacy.md#counting-active-installs)

**A random ID, nothing more.** The ID is made on your machine from nothing about you, and kept in `~/.blitz/install-id`. Nothing else Blitz sends carries it, and deleting the file makes a new one. Retail Cortex keeps each message for 13 months, uses only counts made from them, and doesn't keep your network address.

**Updates ask GitHub, as always.** Checking for a new release asks GitHub directly, with nothing attached, whether or not counting is on.

### Good to know

- Offline? The day's message waits in `~/.blitz/activity.json` and goes later, for up to three days. It never holds anything up: a command or the window never waits on it.
- `[usage] count = false` is the only switch: off, Blitz sends nothing and doesn't make or read the ID. `blitz config` shows it in its template.
