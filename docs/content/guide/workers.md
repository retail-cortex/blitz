---
title: Jobs
weight: 55
---

A **job** runs a prompt on a schedule, unattended: a nightly dependency check, a weekly report, a morning digest of new papers. It lives in the workspace as a Markdown file, runs in the Blitz service, and may only do what you allowed when you enabled it.

Spec: [jobs](../about/specs/spec_workers_023.md).

## Before you start

Jobs run in the **Blitz service** (`blitzd`), not in the window or the terminal session, so they need the service to keep running:

```sh
blitz service install     # starts the service at login (launchd or systemd)
```

The desktop app and `blitz service install` keep it running. A service the CLI started on its own stops after 15 idle minutes, and with it the schedules. Keys a job needs belong in `~/.blitz/.env.toml` (or the keychain): a service started at login doesn't see your shell's environment. In the desktop app, Jobs is with the advanced settings (**Show advanced** (in the Settings title bar)).

## Your first job

**Desktop:** **Studio › Jobs**, then **New job** (the calendar icon), and fill in:

| Field | What to put |
|---|---|
| Description | What it does; the name follows from it |
| Schedule | `Weekdays at 9:30`, `daily at 6 AM`, `every 2 hours`, or cron |
| Time zone | Pre-filled with yours; empty means the service machine's |
| Agent, Model | Leave empty for the workspace's |
| Workflow | The prompt, as you'd type it in the chat |
| Permissions | What it may do, one per line (below) |
| Max turns, Max cost, Timeout | Empty for the defaults: 50 model calls, $1, 30 minutes |
| Missed runs | Skip, or Run once |

**Create** saves it as `.agents/workers/<name>/WORKER.md`. It doesn't run yet: open it, read what it may do, and choose **Enable as shown**.

**By hand:** write the file yourself, then enable it.

```markdown
---
description: Report outdated Go modules
schedule: Weekdays at 9:30
timezone: America/Chicago
permissions: ["shell:go list -m -u all", "write:reports/"]
limits: { max_turns: 30, max_cost_usd: 0.50, timeout: 20m }
---
Check for outdated Go modules and write reports/deps.md, with the module,
the version we use and the newest.
```

```sh
blitz jobs enable deps     # shows what it may do, then asks
```

## The file

`.agents/workers/<name>/WORKER.md` (or `workers/<name>/WORKER.md`): the folder's name is the job's (lowercase letters, digits, `-` and `_`). Only `schedule` and the prompt (the text after the front matter, unless `workflow` names one to run) are required; an unknown key makes the job invalid, so typos show up at once.

| Key | Default | Meaning |
|---|---|---|
| `description` | | Shown in the lists |
| `schedule` | required | When it runs (below) |
| `timezone` | the service machine's | An IANA name, such as `Europe/Paris` |
| `agent` | the workspace's agent | Run as another agent, for its runs only |
| `model` | the workspace's model | `provider/model`, for its runs only |
| `workflow` | none | Run this [workflow](workflows.md) instead of a prompt; the text after the front matter, optional then, is its input |
| `permissions` | none: read only | What it may do (below) |
| `limits` | 50 calls, $1, `30m` | `max_turns` (model calls), `max_cost_usd`, `timeout` |
| `catch_up` | `none` | `once`: when the service starts or the job is enabled, run once if a scheduled time passed since its last run |
| `overlap` | `skip` | A run that's due while one is still going is skipped |

**Schedules** take cron (`30 9 * * 1-5`), descriptors (`@daily`, `@every 90m`) or plain phrases: `hourly`, `every 15 minutes`, `every 2 hours`, `daily at 6 AM`, `weekdays at 9:30`, `weekends at noon`, `every Monday at 8 PM`.

## What a job may do

Nobody is there to approve anything, so a job gets exactly the permissions it lists, and is refused the rest:

| Permission | Allows |
|---|---|
| `shell:go test ./...` | That command (a `*` glob matches any words) |
| `write:reports/` or `write:reports/**` | Writing files anywhere under `reports/`, in folders too (`*` alone stays in one folder: `write:reports/*` is only the files directly in it) |
| `delete:tmp/*.log` | Deleting matching files |
| `web:api.github.com` | Fetching from that host (or using that search provider) |
| `mcp:github:create_*` | That MCP server's matching tools |

Reading the workspace never needs permission. A command with `;`, `&`, `|`, `$`, backquotes or redirection is allowed only by a permission that spells it exactly. Your `deny` rules still apply, an `ask` rule refuses, and permission modes and remembered approvals don't apply. A job can't ask you questions or start background agents. Every refusal is recorded in its run.

`[workers.policy]` in your settings governs every job: which kinds of permission are allowed at all, the limits a job that sets none gets (50 calls, $1, 30 minutes), an optional cap on what one may ask for (`max_turns`, `max_cost_usd`, `max_timeout`; none by default since v0.8.4, so a job may set more than the defaults), and how many runs at once across workspaces (`max_concurrent`, 2).

## Enabling, editing and running

- **Rewrite with AI:** in the job form, **Rewrite with AI** turns rough instructions into ones that run well unattended. It names the sources, says exactly what file to write and in what format, and says what to do when a step fails. It also suggests the narrowest permissions those instructions need. You review it first: **Use these** fills in the form, **Keep mine** leaves it as it was, and nothing is saved until you Save.
- **Enable** pins the job's content. Edit it (in the app, or by hand) and it pauses as **changed** until you enable it again, so what runs is always what you read.
- **States:** new (never enabled), enabled, disabled, changed, invalid (its problems are listed).
- **Run now:** **Run now** in the app, or `blitz jobs run <name>`, runs an enabled job at once and shows what it does.
- **Two at once?** The same job never runs twice at the same time; a scheduled tick that finds it running is recorded as skipped.
- **Stop a run:** **Studio › Jobs** lists the workspace's runs going now under **Running now**, and **Settings › Jobs** every workspace's. Click **Stop**, or use **Stop** in the job's run list or `blitz jobs stop <name>`. The run ends as **stopped**. Files it already changed stay changed, and `blitz jobs undo <run-id>` puts them back.

| Desktop | Terminal |
|---|---|
| **Studio › Jobs › New job** | Write `WORKER.md` |
| Open a job, **Enable as shown** | `blitz jobs enable <name>` |
| **Run now** | `blitz jobs run <name>` |
| **Disable** | `blitz jobs disable <name>` |
| **Stop** (Running now, or the run list) | `blitz jobs stop <name>` |
| The job's run list | `blitz jobs runs <name>` |
| **Edit**, **Delete** | Edit or remove the folder |
| | `blitz jobs undo <run-id>` |

## Global jobs

Some work belongs to no project: a morning news digest, tidying your downloads. A **global job** is saved in `~/.blitz/workers/<name>/WORKER.md` and runs in a folder of its own, `~/.blitz/home`, made the first time it's needed. Everything else is the same: review and enable, schedules, permissions, runs.

**Desktop:** **Settings › Jobs**, then **New job** in the Global jobs box. **Terminal:** write the file, then `blitz jobs enable <name> --dir ~/.blitz/home`.

## Limits for every job

**Settings › Jobs › Limits for every job** sets what jobs get and may do, in your global settings (`[workers.policy]`):

| Setting | What it does |
|---|---|
| Model calls, Cost, Time | What a job that sets no limit gets, and a cap on what one may ask for (empty: no limit) |
| Runs at once | How many runs go at the same time, across every workspace (applies when the service restarts) |
| Jobs may be given | The permission kinds a job may have: shell commands, writing files, deleting files, the web, MCP tools. All off: jobs may only read |

A change applies from each job's next run.

## Every workspace's jobs

The service runs a workspace's enabled jobs whether or not the app has that workspace open. **Settings › Jobs** lists every workspace with jobs on record, so none runs unseen:

- **Not in your workspaces:** **Add and open** brings it back into the app, on its jobs.
- **Folder missing:** the folder was moved or deleted, so its jobs don't run. **Forget** disables them and stops the service looking for the folder; nothing on disk is touched.

## Runs and their results

Each run is a conversation of its own, titled `⏰ <name>` and the time: open it from the run list, or with **History › Jobs**. A run ends as succeeded, failed, limited (it hit a turn, cost or time limit), stopped (you stopped it) or skipped. Its record keeps the files it changed, what was refused, the cost and the error.

A run's file changes are its own checkpoint, outside `/undo`: `blitz jobs undo <run-id>` puts them back (`--force` overwrites files changed since).

**From the tray:** each workspace's submenu lists its jobs, to run one now, stop it, switch it off or on, or show it in Blitz; the runs going on are at the top of the menu, and the icon pulses while one runs.

**When one fails:** the Studio menu gets a red dot, the status bar a failed-runs button, and a new chat's welcome screen offers **Find out why the job … failed**. To be told elsewhere, set a command in your settings:

```toml
[workers]
notify = 'osascript -e "display notification \"$BLITZ_WORKER $BLITZ_RUN_STATUS\" with title \"Blitz\""'
notify_on = ["failed", "limited"]
```

## Good to know

- **The service must be running** at the scheduled time. Missed times collapse into one run when it's back if `catch_up: once` is set, and are skipped otherwise; a laptop that's asleep may miss them.
- **Background runs are different:** `blitz --bg "<prompt>"` runs one prompt in the service now, and waits for you to answer what it asks ([the service](../products/service.md#background-runs)). Jobs run on a schedule and never wait for an approval; only a workflow's Ask a person step pauses one, until someone answers it.
- **A loop is lighter:** `/loop 30m <prompt>` repeats a prompt while the session or window is open, with your usual approvals.
- **Project jobs and trust:** a workspace's jobs are files in the project, so read them before enabling; project hooks don't run for jobs until you trust the project's settings ([Sharing settings with your team](configuration.md#sharing-settings-with-your-team)).
