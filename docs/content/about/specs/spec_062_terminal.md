---
title: "062 · A terminal in the window"
weight: 62
---

*A terminal inside Blitz's window. It runs your own shell in your workspace, with your prompt, your oh-my-zsh theme and its glyphs drawn in a bundled Nerd Font. It opens from the file tree's **Open in terminal** and as a tab beside your files, and never in another app.*

| | |
|---|---|
| Status | **Built** (2026-11-08, for v0.9.7; the owner: "I'd actually like the open in terminal not to open an external app, but to open a blitz native terminal emulator. I like the idea of the oh-my-zsh decorations being supported and a native Meslo (nerd font) integration."). macOS and Linux. Windows (ConPTY) is TRM-50, not built. |
| Source | `pkg/terminal` (the PTY sessions: `Manager`, `Terminal`, their output kept as scrollback); `engine.Workspace.Terminals` (`pkg/engine/terminals.go`), closed with the workspace; `TerminalService` (`proto/blitz/v1/terminal.proto`, `pkg/service/server/terminal.go`); the page's terminal tab (`apps/desktop/web/src/terminal/`), MesloLGS Nerd Font (`apps/desktop/web/src/terminal/fonts/`), the tab in the editor area (`files/useEditor.ts`, `files/EditorPane.tsx`), **Open in terminal** in the file tree's menu (`files/FilesShelf.tsx`) |
| Depends on | [spec_029_files](spec_029_files.md) (the editor's tabs, the file tree's menu), [spec_021_service](spec_021_service.md) (the API, SVC-30's reasons) |

## 1. Purpose

A developer keeps a terminal open beside their files: to run a build, check git, start a server. Until now Blitz had only the agent's shell, which runs commands for the agent, sandboxed, one at a time. That's no substitute for a terminal of your own. A terminal in another app means switching windows, and losing the workspace's folder and Blitz's look. Blitz's own terminal is the person's: their login shell, their prompt and their theme, in the workspace, inside the window.

## 2. The service

- **TRM-01** **The person's shell in a PTY.** A terminal runs the person's login shell (`$SHELL`, else `/bin/zsh` on macOS and `/bin/bash` elsewhere, with `-l`) in a pseudo-terminal (`github.com/creack/pty`). It starts in the workspace's folder, or in a folder inside it (TRM-04). Its environment is the service's, plus:
  - `TERM=xterm-256color` and `COLORTERM=truecolor`;
  - `TERM_PROGRAM=Blitz`, with `TERM_PROGRAM_VERSION`;
  - `LANG=en_US.UTF-8` when `LANG` is unset, since the service started by launchd has none, and a prompt's glyphs need UTF-8.

  It starts at the size the page asks for (TRM-11).
- **TRM-02** **The person's terminal, not the agent's.** A terminal runs outside the agent's sandbox, as the person, like a terminal app: it's what they type, not what a model asked for. So no approval asks about it and no permission rule applies. The agent can't see it: no tool reads a terminal, nothing typed or printed reaches a model, and a terminal isn't in a conversation's context. Nothing of it is logged beyond its start and end (folder, shell, exit code), never its text. A later spec may let the person share a terminal's output with the agent, explicitly.
- **TRM-03** **Kept while the workspace is open.** Terminals belong to the workspace. Each is kept until:
  - its shell exits, and the page has seen the exit;
  - the person closes it; or
  - the workspace closes (the window closing a workspace, the service stopping).

  Closing sends the shell's process group SIGHUP, then SIGKILL after two seconds. A window that goes away (reloads, closes) leaves the terminals running, and a window watching again picks them up with their scrollback (TRM-12).
- **TRM-04** **Where it starts.** `dir` is a folder in the workspace, relative to it (`""` for the workspace itself). A path outside the workspace, or one that isn't a folder, is refused (`BAD_PATH`). A file's folder is the page's to pass (TRM-31).
- **TRM-05** **Limits.** At most 10 terminals a workspace: an 11th is refused (`TOO_MANY_TERMINALS`). Each keeps its last 2 MiB of output as scrollback, which a window replays when it opens the terminal. Input is at most 64 KiB a write.

## 3. The API

`TerminalService` (`terminal.proto`), every request naming its workspace by absolute directory. A browser can't send a stream (Connect over HTTP), so output comes on a server stream and input goes by unary calls. The same API works for the remote service (spec_053), and a later bidirectional stream would replace only the writes.

- **TRM-10** `OpenTerminal(workspace, dir, cols, rows)` starts a terminal and answers its `TerminalInfo`: id, folder, shell, when it started, and whether it has exited, with what code.
- **TRM-11** `ResizeTerminal(id, cols, rows)` sets its size (the PTY's window size, SIGWINCH to the shell). Zero or more than 1,000 is refused (`INVALID_ARGUMENT`).
- **TRM-12** `WatchTerminal(id, from)` streams output: each message has its bytes and the offset after them, counted from the terminal's start. `from` is where the window has read up to:
  - `0` replays what scrollback still holds;
  - output older than the scrollback is skipped, and the first message says how much (`skipped`).

  When the shell exits, the last message has `exited` and the exit code, and the stream ends. Several windows can watch one terminal.
- **TRM-13** `WriteTerminal(id, data)` writes to the shell's input: keys, pastes, as bytes.
- **TRM-14** `ListTerminals` answers the workspace's terminals, oldest first. `CloseTerminal(id)` ends one (TRM-03).
- **TRM-15** **Errors**, with SVC-30's ErrorInfo reasons:
  - `UNKNOWN_TERMINAL`: no such id, or it's been closed;
  - `TOO_MANY_TERMINALS`;
  - `BAD_PATH`;
  - `TERMINAL_UNSUPPORTED`: a platform without PTYs (Windows until TRM-50).

## 4. The page

- **TRM-20** **A tab beside your files.** A terminal opens as a tab in the editor area, like a web page's tab, titled with its folder's name (`shop`, `src`). It sits beside the files it works on and can be moved among them. The panel rules hold:
  - the editor area is the primary view and gets the room;
  - the side panels fold and resize around it;
  - the Drawer's views slide down over it, and closing one leaves the terminal running and visible.

  A bottom panel was weighed: it would take height from every file, and the window has no bottom panel today.
- **TRM-21** **xterm.js.** The tab draws with `@xterm/xterm`, plus:
  - `fit` to fill the tab and send its size on every resize (TRM-11);
  - `unicode11`, for the width of emoji and glyphs;
  - `web-links`, so a URL opens in the browser;
  - `search`, for **Find** (⌘F, Ctrl+Shift+F on Linux);
  - `webgl` when the window can, else xterm's own renderer.

  It handles truecolor and 256 colours, bold and italic. A path the shell prints (`src/main.go:42`) that names a file in the workspace opens it in the editor at that line, on ⌘-click (Ctrl-click on Linux).
- **TRM-22** **Glyphs that oh-my-zsh themes need.** MesloLGS Nerd Font (Regular, Bold, Italic, Bold Italic) is bundled with the page as woff2, so powerline separators and Nerd Font icons draw without installing anything. That covers agnoster, powerlevel10k (whose recommended font is MesloLGS NF) and starship. The font is the official nerd-fonts release's, with its licences in `THIRD_PARTY_NOTICES`: Meslo LG is Apache 2.0, the Nerd Fonts patches MIT, and their icon sets each under their own licence.
- **TRM-23** **Blitz's colours.** The terminal's background, foreground, cursor and selection come from the window's theme tokens. Its 16 ANSI colours are a palette tuned for each of light and dark, following the window when the theme changes.
- **TRM-24** **Copy and paste.**
  - ⌘C copies the selection on macOS. Ctrl+Shift+C does elsewhere, since Ctrl+C is the shell's interrupt.
  - ⌘V and Ctrl+Shift+V paste, bracketed when the shell asks.
  - Right-click offers Copy, Paste, Clear and Find.
- **TRM-25** **Settings.** **Settings › Appearance** sets the terminal's font (`[ui.terminal] font`, MesloLGS NF by default; any font installed, by name) and size (`[ui.terminal] font_size`, 13). They apply to open terminals at once.
- **TRM-26** **Coming back.** A tab watches from where it had read (TRM-12), so a window that reloads shows the terminal as it was. When the shell exits, the tab says so with the code and offers **Restart**, which opens a new terminal in the same folder.

## 5. Opening one

- **TRM-30** **New terminal** is in the editor area's tab menu (the **+**) and in the command menu (⌘K, "terminal"). It opens a terminal in the workspace's folder.
- **TRM-31** **Open in terminal** in the file tree's menu opens a terminal in that folder, or in a file's folder.

## 6. Later

- **TRM-50** Windows, through ConPTY (`creack/pty`'s Windows support, or `conpty` directly), with PowerShell as the default shell.
- **TRM-51** Sharing a terminal's output with the agent, by the person's choice, as a context item (TRM-02 stays the default).
- **TRM-52** Split terminals, and a terminal in a bottom panel for those who want one.

## 7. Tests

- **TRM-40** `pkg/terminal` with a real PTY (macOS and Linux), with short timeouts:
  - `echo` comes back on the stream;
  - a resize reaches the shell (`stty size`);
  - exit codes are reported;
  - scrollback is cut at its limit and replays from an offset;
  - closing kills the process group;
  - the 11th terminal is refused;
  - several watchers see the same output.
- **TRM-41** `TerminalService` through the server: open, write, watch, resize, list and close, and each error with its reason. Terminals close with the workspace.
- **TRM-42** The page:
  - the tab's state and its reconnecting offset, in unit tests;
  - the fake service's terminal echoes input;
  - Playwright WebKit screenshots in `?fake` show powerline glyphs and ANSI colours in light and dark;
  - the file tree's **Open in terminal**.
- **TRM-43** **Manual:** in the desktop app, with oh-my-zsh and powerlevel10k:
  - the prompt draws with its glyphs;
  - `ls --color`, `htop` and `vim` work;
  - resizing re-wraps;
  - a reload keeps the terminal;
  - closing the workspace ends its shells.
