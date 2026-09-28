---
title: Desktop app
weight: 30
---

Blitz's desktop app is a window onto the [service](service.md), laid out like an IDE: the workspace's files on the left, an editor in the middle, and the agent's chat on the right. It contains no engine. Every workspace, turn, approval, session and worker lives in the service, which keeps running, and keeps running workers, after the window closes. Source: `apps/desktop` (Go, Wails v2) and `apps/desktop/web` (React and TypeScript). Specs: [desktop](../about/specs/spec_desktop_024.md), [files](../about/specs/spec_files_029.md).

![The desktop app: the workspace's files, an editor, and the chat with an approval and the agent's task list](../../images/desktop.png)

## Install

`Blitz_<version>_macos_universal.dmg` for macOS 13 and later (signed with a Developer ID and notarized), or `blitz-desktop_<version>_<arch>.deb` for Ubuntu 24.04, Debian 13 and later. Each carries its own `blitz` and `blitzd`, so the app can install the service without a separate download. There is no Windows package, because the service doesn't install there.

From source: `bazel build //apps/desktop/packaging:Blitz.app` (macOS) or `:deb` (Linux); see [building](../development/building.md).

## The window

- **Top bar.** The Blitz mark, a dropdown of workspaces (open, name, colour, describe, close), and Settings at the far right.
- **Files.** The workspace as a tree with git status, hidden files toggled (hidden by default), ⌘P to go to a file. The shelf minimizes to a rail.
- **Editor.** Syntax highlighting and completion. Saving won't overwrite a change made meanwhile by someone else, such as the agent, and the agent is told what you edited.
- **Chat.** Markdown replies, the agent's task list, approvals and plans as cards, and rewind or edit from any earlier prompt. When no file is open, the chat takes centre stage.
- **Run settings.** Agent, model, reasoning effort, generation settings, permission mode and rules, per workspace.
- **Changes.** The session's diff beside the agent's summary.
- **Workers.** The workspace's [workers](service.md#workers), their schedules and runs.
- **Settings.** Providers and API keys (kept in the OS keychain), the settings file itself, appearance (System, Light or Dark) and the interface language (English, Spanish or Canadian French, from the same catalogs as the terminal). **About** shows the license and the third-party notices.

The window keeps only its own settings, in `~/.blitz/desktop.json`: its theme, layout, and the workspaces it knows. Everything else lives in the service, so the REPL and the app see the same sessions.

## How it talks to the service

A web view can't open a Unix socket, so the app's Go side forwards the page's API calls, and only those, to the service's socket. The page uses Connect's generated TypeScript client against its own origin; responses stream through as they arrive, so turn events appear as they happen. The app opens no listener of its own, so nothing new is reachable from the network.

Model and tool output is rendered as React elements (Markdown with raw HTML skipped), never as HTML. Links open in the system browser, and images in output are never fetched.

If the service isn't running, the app offers to install it (a login item, as `blitz service install` does) or to start it, and says when the running service is stale: older than the app, or its program removed or replaced.
