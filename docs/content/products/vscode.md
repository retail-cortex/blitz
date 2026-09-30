---
title: VS Code
weight: 35
---

The Blitz extension for VS Code shows the agent's chat beside your code. It attaches to the [service](service.md), as the [desktop app](desktop.md) does, so a session started in the terminal or the desktop app goes on here. Source: `apps/vscode`. Spec: [vscode](../about/specs/spec_vscode_034.md).

## Install

Each release has `blitz_<version>_vscode.vsix`: `code --install-extension blitz_<version>_vscode.vsix`. Or build it: `bazel build //apps/vscode:vsix`. It needs the service running (`blitz service install`, or `blitzd`); the setting **blitz.socket** points at another socket.

## Using it

- **Chat.** The bolt in the activity bar opens the chat for the folder of the file in front. It is the desktop app's chat: `@` mentions, slash commands, plans, background tasks and runs, rewinding.
- **Approvals.** **Show in the editor** opens a proposed change in VS Code's diff view, beside the file as it is now.
- **Context.** **Blitz: Add Selection to Chat** (⌘⌥B on macOS, Ctrl+Alt+B elsewhere, or the editor's menu) puts the selection in the composer, with where it's from. **Blitz: Add File to Chat** (the editor's and the Explorer's menus) mentions a file.
- **Links.** A file the chat names opens in VS Code at its line.
- **Theme.** The chat follows VS Code's light or dark theme.
