---
title: Blitz 0.6.3
weight: 1
---

**Start a project with one command, and keep going when the network hiccups.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.6.3).

**`/init` now asks about your project first and sets up the workspace in the same go.** Before, `/init` told you to run `/spec`, then come back and run `/init` again. Now it asks the specification's questions right away, writes the specs, and then fits the agents, skills and instructions to them, all in one conversation.

**And a dropped connection no longer freezes the chat.** If the network drops the connection to the model, as a VPN, a sleeping router or a Wi-Fi change can, Blitz notices within a minute, tells you it's reconnecting, and carries on. Before, the chat could sit at "Working…" until you pressed Stop.

**Who should update:** everyone. Starting a new project is much smoother, and anyone on a VPN or a patchy connection gets their turns back.

**See it in a minute:**

1. Make an empty folder and open it in the desktop app, or run `blitz` in it.
2. Type `/init`. Answer the questions about what the project is for.
3. Watch it write the specification in `.agents/specs/`, then carry on to set up the workspace for it, without being asked twice.

### What's new

- **`/init` leads with the specification.** In a workspace without specs, `/setup` (and `/init`) asks the specification's questions itself, writes the specs, and then sets up the workspace from them. Running `/spec` again later still revises the specs in place.
- **Reconnecting, said out loud.** When the connection to the model fails and Blitz retries, a message at the top of the window says so ("Lost the connection to the model… Reconnecting…"). It passes once the model answers, and isn't kept in the chat.
- **Versions read as they're tagged.** Settings › About, the status bar, the tray and `blitz status` show Blitz's version as `v0.6.3`, the way releases and the docs name it.

### What's fixed

- **No more frozen chats after a network error.** A connection that dies without a word used to hold each request for up to 10 minutes, and again for every retry. Now a quiet connection is checked every 30 seconds, and a dead one is replaced within a minute.
- **Answers you can read.** When the agent asks a question with long answers, each answer now wraps onto more lines instead of being cut off at the edge of the card.
- **Messages that match the rest of the window.** The message at the top of the window is now a rounded pane of glass, like the panes and menus, instead of a flat dark bar.
- **The docs name the latest release.** Install commands in the docs and the in-app guide show the newest version, filled in when the docs are built, rather than one that had to be updated by hand.

### Good to know

- Your settings and workspaces carry over: install the update over 0.6.x.
- `/init` in unattended runs (`blitz -p`, workers) doesn't stop to ask questions. Write the specs with `/spec` first there.
- While `/init` writes the specs, the chat shows the questions and approvals, but not each file being written. The specs are all there when it moves on to setting up.
- If every retry fails, the turn ends with the error as before, and you can send your message again. Retries follow `llm.max_retries` (default 3), and with retries off there's no "Reconnecting…" message.
