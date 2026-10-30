---
title: Blitz 0.7.0
weight: 10
---

**A new desktop app for Mac, Windows and Linux: it updates itself, opens your web app beside the chat, and comes in the colours you like.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.7.0).

**One app everywhere, Windows included.** The desktop app is rebuilt on Electron, the same on every system. Windows gets the full app for the first time, not just a tray with a browser page: the folder picker, Export as PDF, notifications, and installing the `blitz` command. On Linux it no longer depends on WebKitGTK, so the graphics-card problems some NVIDIA systems had are gone.

**It keeps itself up to date.** When the app opens it looks for a new release (every few days at most), downloads it, checks its signature, and offers **Restart**. Install 0.7.0 by hand once; after that, there’s nothing to download by hand again.

**See your web app, and show it to the agent.** Click the globe in the top bar, or a `localhost` link in the chat, and the page opens in a tab beside the conversation. **Add to chat** hands the agent the page, your selection, a screenshot or the console's errors, and the agent reads them as content, never as instructions. **Save to workspace** keeps a page as Markdown (with its pictures, ready for the agent to read later), HTML or PDF.

**Who should update:** everyone on the desktop app, and anyone on Windows who has been using the tray's page.

**See it in a minute:**

1. Install the app for your system from the release, and open a workspace.
2. Start your web app (for example on `localhost:3000`) and click the globe beside Settings.
3. Type the address, then choose **Add to chat › Console errors** and ask the agent what's wrong.

### What's new

- **Windows desktop app.** `Blitz_<version>_windows_x64.zip`: unzip it and run `Blitz.exe`. It has Blitz's icon, no console windows popping up, and installs the `blitz` command by putting its folder on your PATH.
- **Updates in the app.** **Settings › About** shows where the update is, with **Check now**, **Download**, **Restart to update** and the release notes; **Update automatically** turns it off. Copies installed by a package manager (the `.deb`, the Arch package) say to update there instead.
- **Web pages in the editor.** Back, forward, reload, the address, **Open in browser**, and sign-ins that stay apart from the app. Only web addresses open.
- **Add to chat** from a web page: the page, the selection, a screenshot or the console's errors, each as a chip in the composer.
- **Save to workspace:** Markdown (its pictures saved beside it), HTML or PDF, in any folder of the workspace.
- **Colour themes.** **Settings › Appearance** has a **Light theme** and a **Dark theme**: Blitz's own, or One Light and One Dark Pro, as in your editor.
- **Expand the chat, or the editor.** With files open, the expand button in the chat's header lays it over the editor to read a long answer at full width; the editor's does the same for a file. The other pane waits on a rail at the side, so you can see it's there, and comes back with a click.

### What's fixed

- **Export as PDF** waits for fonts and pictures before printing, so pictures in documents with maths no longer come out blank.

### Good to know

- Your settings and workspaces carry over from 0.6.x: install the new app over the old one.
- On macOS there's one download for Apple silicon and one for Intel.
- The Windows app isn't signed yet, so Windows asks before it first runs it.
- The One themes are solid; the glass look is part of Blitz's own theme.
- Web pages open only in the desktop app, not in the VS Code extension or the Windows tray's page.
