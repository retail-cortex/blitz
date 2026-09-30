# Blitz for VS Code

Blitz, the coding agent, beside your code. The extension attaches to the Blitz service, as the desktop app does, and shows the conversation for the window's workspace folder.

- **Chat** in the Blitz view of the activity bar: everything the desktop app's chat does, from `@` mentions to plans, background tasks and runs.
- **Approvals**: **Show in the editor** opens the proposed change in VS Code's diff view, beside the file as it is.
- **Context**: **Blitz: Add Selection to Chat** (⌘⌥B, or Ctrl+Alt+B) puts the selection in the composer with where it's from; **Blitz: Add File to Chat**, in the editor's and the Explorer's menus, mentions a file.
- **Links** to files in the chat open them here.

It needs the Blitz service running (`blitz service install`, or `blitzd`). The setting `blitz.socket` points at another socket.
