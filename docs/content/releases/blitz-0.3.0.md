---
title: Blitz 0.3.0
weight: 10
---

**A coding agent that does the work and gets out of the way.** Blitz reads your project, makes the change, checks it and stops. It has no persona and no chatter. It's a compiled Go program that runs the model you choose, on your machine, with your keys. It's open source under the Apache-2.0 license.

## Where to get it

Everything is on the [GitHub releases page](https://github.com/retail-cortex/blitz/releases/tag/v0.3.0), signed so you can verify each download. On macOS or Linux, one line installs the command-line tools:

```bash
curl -fsSL https://github.com/retail-cortex/blitz/releases/latest/download/install.sh | sh
```

- **Desktop app:** `.dmg` for macOS 13 and later; `.deb` for Ubuntu 24.04, Debian 13 and later.
- **VS Code:** the `.vsix` from the same release puts the Blitz chat beside your code.
- **Windows:** a `.zip` with the command-line tools.

> **Windows desktop app: coming soon.** Today, Windows gets the command-line tools. They run without the OS sandbox that macOS and Linux use, and shell commands need Git Bash or WSL. The desktop app for Windows is the next platform on the way.

## How to use it

```bash
# once
blitz config set-key gemini       # or anthropic, openai; stored in your OS keychain
blitz doctor                      # checks the key, the settings and the sandbox

# then, in any project
blitz                             # an interactive session
blitz "why does the build fail?"  # one question, then exit
```

Ask for what you want. Blitz shows a diff before every edit and asks before running commands. You can allow an action once, for the session, or always. `/undo` reverts the last turn. Run `blitz service install` to use the desktop app, the VS Code extension and scheduled jobs. They all share one engine, so a session you start in the terminal carries on in the app.

### What you get

- **Your choice of model:** Gemini, Claude, any OpenAI-compatible API, or local models through Ollama. You pay your provider directly, or nothing for local models.
- **Safe by default:** commands run in an OS sandbox on macOS and Linux, edits wait for your approval, and every turn can be undone.
- **Workspace search:** new in 0.3. Search your code, PDFs, notebooks, past chats and notes by their words, or by meaning if you set an embedding model. The index stays on your machine. See [Workspace search](../guide/search.md#workspace-search).
- **Built for data work:** CSV files of any size open as a table you can search, filter by column and sort. With summaries turned on, a model notes what each column holds.
- **Find in file:** ⌘F (Ctrl+F) finds in the open file, in the Markdown preview or the source.
- **Works with what you have:** `AGENTS.md`, `CLAUDE.md` and `GEMINI.md` load as project instructions. MCP servers, hooks, custom commands and Agent Skills work too.
- **Runs unattended:** scheduled workers, background tasks in their own git worktrees, and JSON output with limits on turns, cost and time for scripts and CI.

## How it compares

Most AI coding tools now fit one of three shapes: an editor built around AI, an extension for the editor you already use, or an agent that runs in the terminal. Blitz is closest to the third, with a desktop app and a VS Code extension on top of the same engine.

| Tool | What it is | Models | How you pay | Source |
|---|---|---|---|---|
| **Blitz** | Terminal agent, background service, desktop app and VS Code extension, all on one engine | Gemini, Claude, OpenAI-compatible, local through Ollama | Free. You use your own API key, or a local model. | Open (Apache-2.0) |
| **Google Antigravity** | An AI-first editor built on VS Code, managing several agents in parallel, with a built-in browser; a CLI and extensions for other editors since 2026 | Gemini, plus some Claude models | Free plan with weekly limits; higher limits with a Google AI subscription | Closed |
| **Cursor** | An AI-first editor built on VS Code | Several providers | Subscription, with a free tier | Closed |
| **GitHub Copilot** | An extension for many editors, plus a coding agent that works from GitHub issues | Several providers | Subscription, with a free tier | Closed |
| **Claude Code** | A terminal agent, with editor and desktop versions | Claude | Claude subscription or API | Closed |
| **Gemini CLI** | A terminal agent | Gemini | Free tier, or your API key | Open (Apache-2.0) |

Plans and features change often. This reflects public information as of October 2026.

### Where Blitz is the better fit

- **No lock-in to one provider:** switch models per task, including a model running on your own machine, with no subscription.
- **Safety you can see:** sandboxed commands, approvals and undo are on from the start, not something you add later.
- **One agent in every place you work:** the terminal, a desktop app and VS Code share sessions instead of being separate products.
- **Open source:** read the code, build it yourself, and check each release's signature.

### Where another tool may suit you better

- **Suggestions as you type:** Copilot, Cursor and Antigravity complete code inline in the editor. Blitz works by conversation and doesn't do this.
- **Bundled model access:** a subscription can be simpler than managing API keys. Antigravity's free plan includes model use within its limits.
- **Maturity:** Blitz is at version 0.3. The large tools have bigger teams, longer track records and larger communities.
- **Windows today:** until the Windows app arrives, Windows users get the CLI only, without the sandbox.

## Try it in five minutes

Install it, set a key, run `blitz doctor`, and ask it about your build. [Getting started](../getting-started/_index.md) covers the rest, and the [repository](https://github.com/retail-cortex/blitz) welcomes issues and contributions.
