---
title: Desktop app
weight: 30
---

Blitz's desktop app is a window onto the [service](service.md), laid out like an IDE: the workspace's files on the left, an editor in the middle, and the agent's chat on the right. It contains no engine. Every workspace, turn, approval, session and worker lives in the service, which keeps running, and keeps running workers, after the window closes. Source: `apps/desktop` (Go, Wails v2) and `apps/desktop/web` (React and TypeScript). Specs: [desktop](../about/specs/spec_desktop_024.md), [files](../about/specs/spec_files_029.md).

![The desktop app: the workspace's files, an editor, and the chat with an approval and the agent's task list](../../images/desktop.png)

## Install

`Blitz_<version>_macos_universal.dmg` for macOS 13 and later (signed with a Developer ID and notarized), or `blitz-desktop_<version>_<arch>.deb` for Ubuntu 24.04, Debian 13 and later. Each carries its own `blitz` and `blitzd`, so the app can install the service without a separate download. There is no Windows package, because the service doesn't install there.

From source: `bazel build //apps/desktop/packaging:Blitz.app` (macOS) or `:deb` (Linux); see [building](../development/building.md).

### Chromebooks (ChromeOS's Linux development environment)

The Linux `.deb` runs in ChromeOS's Linux container (Crostini): `amd64` on Intel Chromebooks such as the Pixelbook, `arm64` on ARM ones. It hasn't been tested there yet; reports are welcome.

- **Debian version.** The container is Debian 12 or 13 (`cat /etc/debian_version`). The app needs WebKitGTK 4.1 at 2.40 or later, which current Debian 12 has; the package refuses an older one. A Chromebook past its last ChromeOS update keeps its container's Debian updates, but may never move to a newer Debian.
- **Install.** `sudo apt install ./blitz-desktop_<version>_amd64.deb bubblewrap`. bubblewrap is the shell sandbox, which Blitz requires on Linux: `blitz doctor` says whether it works in the container, and if it can't, `[sandbox] shell = "auto"` in `~/.blitz/.env.toml` runs commands without it. gVisor is unlikely to run there; skill scripts use bubblewrap instead.
- **A blank or garbled window.** Some Chromebooks can't pass WebKitGTK's rendering through the VM: start the app with `WEBKIT_DISABLE_DMABUF_RENDERER=1 blitz-desktop`.
- **Projects.** Linux sees its home directory and the folders shared with it in the Files app (under `/mnt/chromeos/`); open workspaces from there.
- **Keys.** The container has no keyring, so API keys are kept in `~/.blitz/secrets.toml`, readable only by you.
- **The service** runs as a `systemctl --user` service in the container. ChromeOS stops the container when you shut down or leave Linux idle, so scheduled workers only run while it's up.

## The window

- **Top bar.** The Blitz mark, a dropdown of workspaces (open, name, colour, describe, close), the inbox of background runs (`blitz --bg`; opening one follows it in its chat, where you answer what it asks), and Settings at the far right.
- **Files.** The workspace as a tree with git status, hidden files toggled (hidden by default), ⌘P to go to a file. The shelf minimizes to a rail; drag its edge to widen it.
- **Editor.** Syntax highlighting and completion. Saving won't overwrite a change made meanwhile by someone else, such as the agent, and the agent is told what you edited.
- **Chat.** `@` to mention a file or folder (completed as you type), whose content goes with the prompt, or add one from the Files view (the file bar, or right-click in the tree), or start a conversation about it; Markdown rendered beside its source, and images and PDFs shown; Markdown replies, the agent's task list, approvals and plans as cards, rewind or edit from any earlier prompt, and copy any answer as shown (with its formatting, for a rich editor) or as Markdown, or a whole turn's answer when it came in parts. When no file is open, the chat takes centre stage.
- **Run settings.** Agent, model, reasoning effort, generation settings, permission mode and rules, per workspace, in cards that say where each is kept and summarize what's set when folded; drag the panel's edge to widen it. Panels keep to the window as it's resized or moved to another display.
- **Changes.** The session's diff beside the agent's summary.
- **Workers.** A red dot on a workspace, and a count on its Workers button, say a worker failed since you last looked. The workspace's [workers](service.md#workers), their schedules and runs, and **New worker**, a form for each of a worker's settings and its workflow, saved as `.agents/workers/<name>/WORKER.md` once it's valid.
- **Status bar.** Along the window's foot, on every view: the service, the workspace, its git branch and changed files (click for Changes), whether a turn runs or waits for you, background runs (click for the inbox) and failed worker runs (click for Workers), and on the right the editor's line and column, the agent and model (click for Run settings), the permission mode (click to change it), the context against the compaction threshold and the session's cost. Narrow windows keep the icons and numbers.
- **Settings** (⌘,). Providers and API keys (kept in the OS keychain), one form for the provider you use, Gemini unless you choose another; the settings file itself (highlighted, completing tables, settings and values from the settings reference, with **Validate**, which marks problems at their lines and in the gutter, the setting at the cursor described beside the editor, every setting described on hover and in a searchable list), the service's log (by day, level and text), the service in the system tray, on Linux the OS sandbox (**Allow bubblewrap** when AppArmor stops it, with your password), **Install** for the `blitz` command (a link on your PATH, adding `~/.local/bin` to it in your shell's startup file when needed), appearance (System, Light or Dark) and the interface language (English, Spanish or Canadian French, from the same catalogs as the terminal). **About** shows the license and the third-party notices.

The window keeps only its own settings, in `~/.blitz/desktop.json`: its theme, layout, and the workspaces it knows. Everything else lives in the service, so the REPL and the app see the same sessions.

## How it talks to the service

A web view can't open a Unix socket, so the app's Go side forwards the page's API calls, and only those, to the service's socket. The page uses Connect's generated TypeScript client against its own origin; responses stream through as they arrive, so turn events appear as they happen. The app opens no listener of its own, so nothing new is reachable from the network.

Model and tool output is rendered as React elements (Markdown with raw HTML skipped), never as HTML. Links open in the system browser, and images in output are never fetched.

If the service isn't running, the app offers to install it (a login item, as `blitz service install` does) or to start it, and says when the running service is stale: older than the app, or its program removed or replaced.
