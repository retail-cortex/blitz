---
title: Blitz 0.9.12
weight: 2
---

When your company's gateway won't answer, Blitz now shows you exactly what it sent and what came back. Turn on detail for just the part you're looking at and leave the rest of the log quiet. And the HTML in your documents finally looks the way you wrote it.

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

### Also new

- **Deleted files go to your Trash.** Delete in the file tree, and the agent's deletions, now land in your system's Trash (Finder's on a Mac, your desktop's on Linux), so you can empty them or put them back the way you always do. **Undo** in the message still works. Prefer the old way? Set `[tools] trash = "workspace"`.
- **A terminal that recovers.** If the connection drops, the terminal reconnects by itself and picks up where it left off. When something running in it stops answering, such as an `ssh` whose network went away, a bar offers **Restart terminal**: a fresh shell in the same folder and tab. It's in the terminal's right-click menu too.
- **Room on your disk.** Blitz keeps only the three newest downloaded updates, and clears out the rest the next time it opens. **Settings › About** shows what they take, with **Clean up**.
- **Your gateway's model list can live on another server.** Give `models_path` a full URL, and `[llm.api.models_headers]` the headers that server needs, the token too. Your gateway's own headers are never sent anywhere else. In the desktop app, it's **Model list headers**, under where the gateway lists its models.

### Fixed

- **Signing in again takes one click.** When your organization's sign-in expires, **Sign in** on the banner now opens the sign-in page straight away, instead of taking you to Settings first.
- **Typing in the terminal can't freeze it.** A program that stopped reading its input used to block every key after it. Now the keys still flow, and the terminal tells you what's wrong.
- **HTML in your documents shows as HTML.** A `<br />`, `H<sub>2</sub>O`, `<kbd>⌘</kbd>`, a centred `<div>` or a `<details>` is drawn in Visual and Preview instead of showing as code (or not at all). Scripts, styles and anything else stay as their source, so nothing in a file can run, and saving writes your HTML back exactly as it was.
- **Folders that hold one folder take your files again.** With compact folders, a new folder's parent shared its row (`docs/notes`), and you couldn't drop anything into the parent. Now each name on the row is a folder of its own: drop on it, click it, or right-click it.

### Good to know

- For a single run from a shell, `BLITZ_LOG_LEVELS=gateway=debug blitz --local` does the same without changing your settings.
- The levels are `debug`, `info`, `warn`, `error` and `off`. Everything not named keeps `log.level`, `info` by default.
