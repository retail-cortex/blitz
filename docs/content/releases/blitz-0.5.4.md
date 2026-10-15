---
title: Blitz 0.5.4
weight: 4
---

**Spec-driven, for every kind of project.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.5.4).

**v0.5.4 takes `/spec` beyond software.** Researchers, data analysts and writers now get a specification and a workspace built for their kind of work, not a developer's. The desktop app gains every slash command the terminal has, the user guide now lives inside the app, and MCP servers and agents got safer and clearer.

**See it in a minute:**

1. Open a folder with your study, report or manuscript in Blitz and type `/spec`.
2. Pick what kind of project it is: Software, Research or science, Data analysis, Writing or documentation, or Something else.
3. Answer the questions that kind needs. For a study, those are the question, data, methods, compute, ethics and outputs.
4. Run `/setup`: it offers helpers that fit, such as a literature reviewer and a statistics critic for research.

### Not only software

- **`/spec` asks what you're making first**, then the questions that kind needs: for a study, the research question, data, methods, compute, ethics and outputs, not deployment and sign-in. Mixed projects are fine.
- **Setup follows the kind.** `/setup`, `/skills update` and `/agents update` offer helpers that fit: experiments, figures and citations for research; a literature reviewer and a statistics critic instead of a code reviewer.

See [Start any project: spec, then setup](../tutorials/spec-then-setup.md), with a walkthrough for software, research, data analysis and writing.

### Every command, in the app too

- **The desktop app has the terminal's slash commands.** Type `/` in the chat, or press ⌘K, for the same commands in the same groups: `/status`, `/goal`, `/loop`, `/memory`, `/permissions`, `/approvals`, `/skills`, `/mcp`, `/tools`, `/export`, `/fork`, `/rewind`, `/copy` and more, 31 of them new to the app.
- **`/loop` repeats a prompt** while the window is open. For schedules that keep going after you close it, use a [worker](../guide/workers.md).
- **`/config`** opens the settings file; `/config key=value --save` changes it.
- **Terminal-only:** `/clear`, `/exit`, `/paste`, `/cd` and `!command`. `/help` in the app tells you what to do in the window instead.

### The user guide, in the app

- **Help opens the guide for the version you run**, with search. Each section has its own entry in the Help menu, `/docs <topic>` finds a page, and the guide remembers where you left off. It reads well in a small window too.
- **New pages:** the spec-then-setup tutorial, and guides to [workers](../guide/workers.md), [MCP servers](../guide/mcp.md) and [agents](../guide/agents.md).

### Safer, clearer MCP servers and agents

- **Secrets stay secret.** An MCP server's header values (an access token, often) are now masked in logs, telemetry and `blitz config show`, as its environment values were.
- **Your rules always apply.** An auto-approved server's resources now follow your `deny` and `ask` rules, as its tools do.
- **Problems show where you are.** When an MCP server goes down, is paused or comes back, the conversation says so, instead of only the service's log.
- **What's off looks off.** `/mcp` marks disabled servers, and `/tools` no longer offers their tools.
- **No silent skips.** A project's SSE server entry, which Blitz can't run, is reported rather than tried and failed.
- **Agent files mean what they say.** `permission_mode: acceptEdits` and other alias spellings now work, and can't loosen an untrusted project's agent.
- **Worker costs show** in the desktop app's run list.

### Good to know

- **Coming from 0.5.3?** Everything in [Blitz 0.5.3](blitz-0.5.3.md) is here too: `/spec` and `/setup`, Gemini 4 Argon by default, the idle CPU fix and the Mac menus.
- **The website** still has the newest guide; **Open on the web** in the app's guide takes you to the same page there.
