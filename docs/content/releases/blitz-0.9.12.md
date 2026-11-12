---
title: Blitz 0.9.12
weight: 1
---

When your company's gateway won't answer, Blitz now shows you exactly what it sent and what came back. Turn on detail for just the part you're looking at and leave the rest of the log quiet.

### Get started

1. Update Blitz. It updates itself, or run `sudo apt upgrade`, or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.12).
2. Add this to `~/.blitz/.env.toml`, then restart the service (**Restart the service** in the desktop app's settings or the tray's menu):
   ```toml
   [log.levels]
   gateway = "debug"
   ```
3. Send a message to a model on your gateway, and open **Settings › Logs**.

### New

- **See every call to your gateway.** Each request shows the address it went to, which API and model, whether it was a first try or a retry with a fresh token, how long the gateway took, and what it answered, including the start of any error. Your token never appears.
- **Detail where you want it.** `[log.levels]` sets the level for one part of Blitz at a time, by name, without filling the log with the rest. `gateway.token` follows the token script on its own, so you can watch sign-ins or leave them out.
- **Built to grow.** Every line names the part it came from, and more parts of Blitz will get their own names, so the same switch works for them too.

### Good to know

- For a single run from a shell, `BLITZ_LOG_LEVELS=gateway=debug blitz --local` does the same without changing your settings.
- The levels are `debug`, `info`, `warn`, `error` and `off`. Everything not named keeps `log.level`, `info` by default.
