---
title: "052 · Processes"
weight: 52
---

*The agents' background processes: what each uses, flags for the ones that look bad, and one place to see and stop them, in the window, the CLI and the tray* (`spec_processes_052`)

| | |
|---|---|
| Status | **Built** (v0.8.5, the owner's request, 2026-10-25: "Should we also add a processes icon in the status bar with an expandable panel instead of trying to keep it in the system tray? … Do we have a /processes in the CLI for process clean up there? Too often agents go 'bad'") |
| Source | `pkg/engine/tools/processhealth.go` (the limits, the monitor, the flags), `procusage.go`, `procusage_unix.go`, `procusage_other.go` (the sampling), `background.go` (`ProcessManager`), `pkg/engine/turn.go` and `sessions.go` (a session's turns and deletion), `pkg/engine/backend.go` (`AnyProcessOutput`), `pkg/config/config.go` (`ProcessesConfig`), `WorkspaceService.WatchProcesses`, `GetProcessOutput` `any_session` (`apps/service/internal/server/workspace.go`), `pkg/client/processes.go` (`RunningProcesses`, `StopProcessIn`, `ProcessOutputIn`), `apps/cli/internal/tui/processes.go` (`/processes`), `apps/tray/internal/tray/processes.go`, `apps/electron/src/logic.ts` (`blitz://processes`), the desktop's status bar and Processes view (`apps/desktop/web/src/`) |
| Tests | `pkg/engine/tools/processhealth_test.go` (`TestProcessHealth`, `TestProcessHealthEnds`, `TestParseCPUTime`, `TestParseGroups`, `TestProcessLimits`), `procusage_unix_test.go` (`TestSampleGroups`), `apps/service/internal/server/handlers_test.go` (`TestProcessesOverTheAPI`), `pkg/client/processes_test.go` (`TestRemoteBackgroundProcesses`), `apps/cli/internal/tui/processes_test.go`, `apps/tray/internal/tray/processes_test.go`, `extras_test.go`, `apps/electron/src/logic.test.ts`, the desktop's tests |
| Depends on | [spec_shell_007](spec_shell_007.md) (background processes, their process groups), [spec_electron_038](spec_electron_038.md) EL-112 (the tray's Processes menu), [spec_service_021](spec_service_021.md) (one service, many clients) |

## 1. Purpose

An agent starts dev servers, watchers and test runners in the background and moves on. Some spin a core, some leak memory, and many outlive the work that needed them: the chat that started them is long over, and nobody remembers they run. The tray lists them (EL-112), but a menu can't show their output or say which ones look bad. The person needs one view of them in each place they work, with the bad ones marked and a quick way to stop them.

## 2. What's listed

- **PR-01** Only what the agents start: the background processes of the agent's shell tool (`run_shell_command` with `background`), in every open workspace, each with the session whose turn started it. Commands the person runs (`!cmd`) and other programs aren't.

## 3. Health

- **PR-10** While any process runs, the workspace samples what each uses every 5 s: one `ps -axo pgid=,time=,rss=` for all of them, added up by process group (each background process leads a group of its own, so its group is it and its children). CPU is the CPU time used since the last sample over the time between, as a percent of one core (never below 0, when a child exits); memory is the group's resident memory. The sampler stops when none runs and on shutdown. On Windows nothing is sampled (`measured` false); only PR-20 applies.
- **PR-11** **Busy** (`cpu`): at or above `cpu_percent` (90) of a core at every sample for `busy_for` (2 minutes).
- **PR-12** **Memory** (`memory`): resident memory at or above `memory_mb` (2048), or over 256 MB and up by half on a sample a whole `growth_window` (10 minutes) old.
- **PR-20** **Left running** (`left`): its session was deleted, or has had no turn for `left_after` (2 hours) and has none running now. `idle_ms` says for how long.
- **PR-21** A session's turns are noted as they start and end (`ProcessManager.TurnStarted`, from the engine's every turn: a chat's, a background run's, a job's); a deletion with `SessionGone`. Sessions with no process and no turn running are forgotten as finished processes are.

## 4. The service

- **PR-30** `Process` carries `session_id`, `cpu_percent`, `memory_bytes`, `measured`, `flags` and `idle_ms`. `WorkspaceService.WatchProcesses` streams the running processes of every open workspace (or one): the list at once, then again whenever one starts or ends, its flags change, or its use moves by a percent or a megabyte (looked at every 2 s); not for its running time, which a client counts.
- **PR-31** `GetProcessOutput` with `any_session` reads one's output whichever session started it, as `KillProcess` with `any_session` stops it (EL-112): the person's own look and stop. A background process keeps the first 128 KB of its output and the latest 128 KB (`newHeadTailBuffer`), so what a long-running server says now is there.

## 5. The CLI

- **PR-40** `/processes` (`/ps`) lists this workspace's, with what each uses and why it looks bad (flagged lines in yellow), and says how many run in other workspaces; `/processes all` lists every workspace's, under each workspace. `/processes log <id>` shows a process's last 40 lines of output; `/processes stop <id>…` stops some, `/processes stop flagged` the flagged ones, `/processes stop all` every one, in this workspace, whichever session started them.

## 6. The window

- **PR-50** First on the status bar's left, a board icon (`mdiDeveloperBoard`): green while processes run in any open workspace (amber when one is flagged), greyed when none do, with how many (and how many are flagged) in its hover text, beside the service's icon (`mdiVectorPolyline`, green or red, its state in its hover text). A click opens the **Processes** menu, like the agent's and the permission mode's: every workspace's processes under a heading each (this workspace's first), each with its command, a dot (amber when flagged) and a line with how long it has run, CPU and memory (when measured) and its flags (**Busy**, **Memory**, **Left running · idle 2h 5m**), and **Stop** at the row's end (on hover; Delete presses it). Below them, **Stop flagged (n)** (asked first) and **Output and details…**. Picking a process, or Output and details, opens the drawer (`ProcessesDrawer.tsx`; the desktop's `/processes` too): the same processes under their workspaces, Stop and Stop flagged, and a row's last 40 lines of output (the picked one's open), with a refresh. One `WatchProcesses` stream feeds the menu, the icon and the drawer (`processes.ts`), reconnecting after 10 s when it breaks; its messages carry `listed`, so the page server's keepalives (empty messages) aren't taken for an empty list.
- **PR-51** `blitz://processes?dir=<folder>[&id=<n>]` opens the view on a workspace, and on one process's output.

## 7. The tray

- **PR-60** The Processes menu (EL-112) counts the flagged ones in its title (*Processes (3 running, 1 flagged)*); a flagged process has ⚠ before it, and under it a line with why and what it uses (*Busy, Left running · 100% CPU · 3.0 GB*; for one that isn't flagged, what it uses), then **Show in Blitz** (PR-51) and **Stop**. The menu is still asked every 2 s with the rest; one list the service can't give (a workspace's trouble) leaves that list empty and the rest shown.

## 8. Settings

- **PR-70** `[processes]`: `cpu_percent`, `busy_for`, `memory_mb`, `growth_window`, `left_after` (Go durations); unset, 0 or not a duration keeps the default.

## 9. Not in scope

- Stopping what looks bad by itself: a flag only says so. A server that's busy compiling or a test run that takes memory may be just what the person wants.
- Processes the person started, and other programs.
- Sampling on Windows (a job object's accounting could give it).
