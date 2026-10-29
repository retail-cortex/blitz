---
title: Blitz 0.5.3
weight: 16
---

**Start with what you're building.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.5.3).

**v0.5.3 lets Blitz understand your project before it sets it up.** A new `/spec` command interviews you about what you're making, who it's for, how fast and big it must be, where it runs and how people sign in, then writes a real specification your agents follow. Gemini 4 Argon is the new default model. An idle window no longer keeps your processor busy, and both help screens are finally easy to scan.

**See it in a minute:**

1. Open an empty folder in Blitz and type `/spec a recipe-sharing web app`.
2. Answer a few rounds of questions: intent and value, users, performance, platform, security.
3. Confirm the list of specs it proposes. You get `.agents/specs/001-overview.md`, one spec per piece of work in the order you'd build it, and an index.
4. Run `/setup`: it reads the specification and sets up the instructions, skills and agents to match.

### Specifications that drive your agents

- **`/spec`** writes numbered specs in `.agents/specs/` (001 to 999, in build order). Each has goals, numbered requirements, acceptance criteria and open questions. Run it again, or `/spec <what changed>`, to revise them.
- **`/setup` asks first.** With no specification yet, it offers to write one before anything else, because the spec should shape the setup.
- **`/skills update` and `/agents update`** check your skills and agents against the specs, propose what to add, change or retire, and change only what you pick.
- **Blitz notices when a spec changes.** The welcome screen offers "The specification changed: update the skills and agents".

### Gemini 4 Argon by default

- New installs, and anyone who never picked a model, now use **`gemini-4-argon`**, for chat and for web search.
- A model you've chosen in your settings stays as it is.

### Lighter on your computer

- **An idle window no longer burns CPU.** The glowing ring around the chat box now moves only while the agent works. On Linux this ends the window sitting at 100% CPU while idle, even with the display off.
- **Linux uses your graphics card.** The window now draws with the GPU where it can. If it flickers or goes blank, turn off **Use the graphics card** in Settings › Appearance (with the advanced settings shown). If Blitz won't start at all, launch it once with `BLITZ_GPU=off`.
- **Linux dock icon:** the window now groups under the Blitz icon in your dock or taskbar.

### Feels like a Mac app

- **Blitz › About Blitz** and **Settings… (⌘,)** are in the menu bar, along with Hide, Hide Others, Show All and Quit, in your language.
- **Escape in full screen** closes a dialog without also leaving full screen.

### Help you can actually read

- **`blitz --help`** groups its commands (runs and sessions, the workspace, models and extensions, setup) and its flags (session, agent and model, one-shot runs, permissions, instructions), each alphabetical.
- **`/help`** in a session does the same for its slash commands, then lists your custom commands by where they come from.

### Good to know

- **More in 0.5.4:** `/spec` for research, data analysis and writing, every terminal command in the desktop app, and the user guide in the app are in [Blitz 0.5.4](blitz-0.5.4.md).
- **Placeholder price:** Gemini 4 Argon is priced at Gemini 3.8 Flash's rate until Google publishes its own, so costs and `--max-cost-usd` still work. If you know the real price, set it under `[pricing."gemini-4-argon"]`.
- **Retiring skills and agents:** `/skills update` and `/agents update` can't delete files. A retired skill or agent says why it was retired, so you can remove the file.
- **`/help` and `update`:** `/help` mentions `update` in the descriptions of `/skills` and `/agents`, not yet in their usage columns.
- **Testing so far:** the menus and specifications were tested on macOS. Linux GPU drawing is new, so please tell us if your window misbehaves.
