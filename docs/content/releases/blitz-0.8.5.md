---
title: Blitz 0.8.5
weight: 1
---

**Know what your agents left running.** Agents start dev servers, watchers and test runs, then move on. In Blitz 0.8.5 you can see every one of them from wherever you work: the app's status bar, the CLI and the tray. Blitz also points out the ones that look bad, whether they're spinning a core, eating memory, or still running hours after anyone used them, so you can stop them in a click.

### Get started in three steps

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.8.5).
2. Ask the agent to "start the dev server". The status bar shows **1 process**.
3. Click it. You'll see the server, how long it has run and what it uses. Open it to read its output, or **Stop** it.

### What's new

**Processes in the app.** While your agents' processes run, the status bar shows how many. Click it for the **Processes** view: each one with its workspace, running time, CPU and memory. Open one to read its last lines of output. Stop one, or every flagged one at once. See this workspace's processes, or every workspace's. [Background processes](../guide/safety.md#background-processes)

**The bad ones, flagged.** A process that keeps a core busy for two minutes is marked **Busy**. One that's over 2 GB or keeps growing is marked **Memory**. One whose chat was deleted or has gone quiet for two hours is marked **Left running**. Blitz never stops anything without you asking. Change the limits under `[processes]` in your settings.

**`/processes` in the CLI.** List what's running here or everywhere, read a process's output with `/processes log 3`, and clean up with `/processes stop flagged`.

**A smarter tray.** The tray's **Processes** menu now shows what each process uses and why it's flagged, counts the flagged ones in its title, and has **Show in Blitz** to open the process in the app.

**A tray that keeps working.** A workspace folder you deleted no longer empties the tray's menus. Its jobs are just left out.
