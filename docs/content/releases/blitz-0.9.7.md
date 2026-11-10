---
title: Blitz 0.9.7
weight: 2
---

A terminal built into Blitz, a file tree that keeps up with your work, pictures and data from the terminal, and Gemini searching the web for you again.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.7).
2. Press ⌃` (Ctrl+` on Linux): your shell drops down from the top of the window. Press it again to tuck it away.
3. In **Files**, select a few files with ⌘-click, press ⌘C, pick a folder and press ⌘V. Delete one by mistake? Press **Undo**.
4. In the terminal, try `blitz picture "a lighthouse at dusk" -n 2` or `blitz data query recipes "select title from recipes"`.

### What's new

**A terminal, built in.**

- Your terminal drops down from under the top menu, Yakuake-style, with ⌃` and tucks away again, still running. Drag its edge to the height you like; Blitz remembers it.
- Keep several terminals as tabs in the drawer. The terminal button beside Processes lists them, so you can jump to the one you want, or start a new one.
- **Open in terminal** on any folder, or **New terminal** from ⌘K, opens a shell right there.
- oh-my-zsh and powerlevel10k prompts look right: full colour, powerline segments and icons, with the MesloLGS Nerd Font included.
- Copy, paste and find on the usual keys; ⌘-click a file path the terminal prints to open it at that line.
- It's your terminal, not the agent's: the agents can't read it.
- Choose the terminal's font and size in **Settings › Appearance**.

**A file tree that keeps up, and works the way you expect.**

- Files appear, change and disappear in the tree as soon as they do on disk: no more waiting for it to catch up.
- **Find in folder** on a folder's menu searches just that folder.
- Select several files with ⌘-click, Shift-click or Shift+arrows, then copy, cut, paste, duplicate, drag or delete them together.
- Delete no longer asks first: deleted files go to a trash for a day, with **Undo** right there.
- **Filter files** finds a file anywhere in the workspace, and typing in the tree jumps to a match.
- Files with errors or warnings show it, and so do their folders.
- The tree follows the tab you're on, and **Reveal in Files** finds the file from its tab.
- `a/b/c.md` creates its folders, and long single-folder chains fit on one row.
- Sort folders first, mixed or by type, and nest tests under their code and lock files under their manifest.
- Drag files onto the chat to add them to your message, and big folders end with **Show more** instead of stopping silently.

**Pictures, everywhere.**

- **Try again** and **Vary** redo a picture, the same or with a fresh seed.
- **Open in Pictures** takes a picture the agent drew in the chat into the Pictures panel, to keep working on it.
- The agent's picture tool now uses each model's full set of shapes, sizes and reference pictures, and can make up to four pictures at once.
- `blitz picture` makes pictures from the terminal, with the model, shape, size, count, reference pictures and output folder you choose, and `blitz picture show` prints a session's turns, prompts, settings and costs.

**Your data from the terminal.**

- `blitz data list` shows your database connections and their saved queries.
- `blitz data query` runs SQL or a saved query (always read-only) and prints a table, JSON or CSV.
- In the terminal's chat, `/data` lists them, and Tab completes `@db:` mentions.

### Fixes

- **Gemini searches the web for you again.** On the Gemini API (a Gemini key), a conversation that used Gemini's own search, page reading or code running alongside Blitz's tools could stop with "Error 400 … Please enable tool_config.include_server_side_tool_invocations". Blitz now sends the setting Google asks for, and keeps Gemini's search steps for the next turn. Gemini on Vertex AI wasn't affected.
- An open file that you delete and then restore with **Undo** no longer says "Deleted on disk".
- When OpenAI won't make a picture (its safety system, say), you see its reason instead of a bare "400 Bad Request".

### Good to know

- The built-in terminal works on macOS and Linux; Windows comes later.
- Copy and paste in the tree use Blitz's own clipboard, not your computer's.
- Streaming query results to workflows is still on the way.
