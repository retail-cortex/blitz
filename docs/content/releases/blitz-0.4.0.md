---
title: Blitz 0.4.0
weight: 6
---

**Blitz comes to Windows.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.4.0).

Until now, Windows users only got the command line. **v0.4.0 brings the full Blitz app to Windows**: the editor, the chat, approvals, workers, workspace search, all of it. No installer, no admin rights.

**Getting started takes a minute:**

1. Download `blitz_0.4.0_windows_amd64.zip` and unzip it anywhere.
2. Double-click **`blitz-tray.exe`**. A bolt appears in your notification area and the Blitz service starts by itself.
3. Click the bolt → **Open Blitz**. Blitz opens in its own window, with no tabs and no address bar, so it looks and feels like an app.

Tick **Start Blitz at login** in the bolt's menu, and Blitz is ready every time you sign in.

### What you get

- **The same app as macOS and Linux.** One codebase, one look: everything you'd do in the desktop app on a Mac, you can do on your PC.
- **It fits into Windows.** **Show in File Explorer** opens the folder with the file selected, images and PDFs open in your usual viewer, and the bolt turns amber while the service runs and grey when it's stopped. Start, Stop and Restart are one click away.
- **It stays private.** Blitz runs on your machine only. The window is served from `127.0.0.1` behind a one-time link, and the service itself never opens a network port. Other people on your network can't reach it, and neither can other websites in your browser.
- **It's light.** The whole thing is three small programs in a zip: `blitz.exe`, `blitzd.exe` and `blitz-tray.exe`. It uses Microsoft Edge, which every Windows PC already has (or your default browser if Edge has been removed).

### Also in this release

- **Stopping the service is cleaner everywhere.** The tray now asks the service to stop, so turns in progress finish their last steps before it exits instead of being cut off. This applies on macOS and Linux too.

### Good to know

This is Blitz's **first release on Windows** (Windows 10 version 1803 or later, or Windows 11, 64-bit Intel/AMD). A few things are still on the way:

- Choosing a workspace asks you to type or paste its path. A folder picker is coming.
- Export as PDF and installing the `blitz` command from the app aren't available on Windows yet.
- The agent's shell commands need Git Bash or WSL, and Windows has no OS sandbox for them yet.

Try it and tell us how it goes. Issues and ideas are very welcome. 
