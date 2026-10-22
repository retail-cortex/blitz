---
title: Blitz 0.6.2
weight: 7
---

**Approve everything the agent asks, all at once.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.6.2).

**v0.6.2 fixes a turn that could hang after you approved the agent's first request.** When the agent asks for several things at once, such as writing four files in one go, Blitz used to show you only the last request. The others waited for an answer you never had a chance to give, and the turn sat at "Working…" until you stopped it. Now every request gets its own card, one after another, and the turn carries on once you've answered them all.

This release also adds a few fixes from using 0.6:
- On the Mac, the system tray stays out of the Dock.
- Linux software centres name Retail Cortex as Blitz's author.
- Messages at the top of the window no longer cover the editor's tabs.

**Who should update:** everyone using the desktop app, especially if you approve the agent's actions yourself rather than letting the auto mode decide. The command-line tools already handled several requests correctly.

**See it in a minute:**

1. Open a workspace in the desktop app, with the permission mode on **Default** (Blitz asks before writing).
2. Ask the agent for several files at once: *"Create AGENTS.md, CLAUDE.md and GEMINI.md, each pointing at .agents/AGENT.md."*
3. An approval card appears for the first file. Allow it, and the card for the next file takes its place. After the last one, the agent finishes its turn.

### What's fixed

- **No more hanging turns.** Requests and questions that arrive together queue up in order, and the next one shows as soon as you answer. Before, each new request replaced the one on screen. The tool calls you never saw kept waiting, and with more than eight they held up the rest of the turn too.
- **The tray stays in the menu bar (macOS).** Started from Blitz.app, the tray showed a second Blitz icon in the Dock and the app switcher. Now it lives only in the menu bar, where it belongs.
- **"Retail Cortex", not "Unknown Author" (Linux).** The `.deb` and the Arch Linux package now carry the details software centres read: GNOME Software, Ubuntu's App Center and KDE Discover show Blitz's author, summary and links.
- **Messages stay clear of the editor's tabs.** Notices such as "Saved README.docx" now show below the top bar and the panes' tabs and toolbars, not on top of them.

### Good to know

- Your settings and workspaces carry over: install the update over 0.6.x.
- In the auto mode, a reviewer model answers most requests for you, so you may never have seen the hang. Requests that an ask rule says you must see still queue up for you, in order.
- If a turn is already stuck waiting in 0.6.1, stop it before updating. The update doesn't bring back requests that were lost.
- The new software centre details take effect when you install the package. A software centre may show the old "Unknown Author" until it refreshes its listings.
