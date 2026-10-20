---
title: Blitz 0.7.2
weight: 1
---

**No more frozen window, however many workspaces you have open.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.7.2).

**Open everything at once.** With a few workspaces open, a turn running and Studio up, the app could stop answering: menus, settings and files all waited, as if the service had crashed. It hadn't. The window could only hold six connections to Blitz at a time, and every live view (a running turn, a workspace's tasks, a workflow's progress) kept one busy. Now they all share one, so keep as many workspaces, chats and canvases open as you like.

**Who should update:** everyone, and especially if you work in several workspaces at once.

### What's changed

- **Evals keep running when you leave.** Start an eval, then close it or Studio and get on with something else: it carries on, and the list shows it running. Open it again to watch it finish, or stop it. Every case also has a time limit, so one that gets stuck is marked as an error and the run moves on.
- **Skills you can test.** Give a skill **trigger phrases**, examples of what it's for, and the agent picks it up for requests like them, however they're worded. Then, on the skill's new **Scenarios** tab, **Try it** with a prompt: you see straight away whether the agent used the skill and what it said. **Keep it** turns a good try into a scenario, and **Run all** checks them all in the background. Scenarios are saved in Castor's format, so a skill you share carries its tests, and `blitz eval --skill <name>` runs them in CI.
- **A tray that shows what's going on.** The bolt pulses while a workflow or a job runs, and the menu lists each run at the top, with **Open** and **Stop**. Every workspace has its own submenu with its jobs and workflows: each starts with its state (◉ on or ○ off), then **Run**, **Stop** and **Open**, with marks for what's running and what failed.
- **Studio is a menu now, like the others in the bar.** Jump straight to Agents, Evals, Skills or any other part from anywhere, and close Studio from the same menu. Inside Studio, the list down the left is icons only, so the page you're working on gets the room.

- **The window talks to Blitz over HTTP/2.** Every request and live update now shares a single connection, so there's no longer a limit of six.
- **The connection is encrypted, even on your own machine.** Each time the app starts, it makes a private certificate for that session only. The window trusts that one certificate and nothing else, so no other program on your computer can pose as Blitz, even if it grabs the same port.
- **On Windows, Open Blitz opens the app.** The tray's **Open Blitz** now opens the Blitz app instead of an Edge window.

### Good to know

- **Workers are now called jobs**, in the app, the tray and the CLI: `blitz jobs` (`blitz workers` still works). Nothing moves: a job is still saved as `.agents/workers/<name>/WORKER.md`, so existing ones carry on as they are.

- Help's **Set up the agent harness** is now **Set up the harness**.

- **The Edge window on Windows is gone.** Up to 0.7.1, the CLI's Windows zip had a tray that showed Blitz in an Edge window. The Blitz app has replaced it, so install it with `Blitz_0.7.2_windows_x64_setup.exe`. The CLI zip now holds `blitz.exe` and `blitzd.exe`, like the other platforms' archives.
- Your settings, workspaces and conversations carry over from 0.7.1. The app updates itself, or install the new one over the old.
