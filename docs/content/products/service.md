---
title: Service (blitzd)
weight: 20
---

`blitzd` runs Blitz as a per-user service. It holds every workspace a client opens, runs the workers' schedules, and serves the desktop app, the CLI when it attaches, and any other client. Source: `apps/service`. Specs: [service](../about/specs/spec_service_021.md), [client](../about/specs/spec_client_022.md), [workers](../about/specs/spec_workers_023.md).

## Running it

It ships beside `blitz` in every archive and inside the desktop app. Start it at every login with:

```bash
blitz service install           # a launchd agent on macOS, a systemd user unit on Linux
blitz service status            # installed? answering?
blitz service uninstall
```

The desktop app installs, restarts and stops it too, without the CLI. By hand: `blitzd [--socket PATH] [--config FILE]`. `blitzd --license` shows the license terms.

### In the system tray

`blitz-tray`, beside the desktop app (in the `.deb` and `Blitz.app`), shows the service in the system tray: coloured while it runs, grey when it doesn't, and a menu with **Start**, **Stop** and **Restart**, **Open Blitz** and **Open the logs**. Turn it on in the desktop app (Settings › Service › **Show in the system tray**) or with `blitz-tray --install`, which starts it now and at every login; `--uninstall` stops that. On GNOME the icon needs the AppIndicator extension, which Ubuntu has on by default; KDE and most other desktops show it as they are.

A login item doesn't see your shell's environment, so keep API keys in the keychain (`blitz config set-key`) or in `~/.blitz/.env.toml`.

## The socket

The service listens only on a Unix socket that its user can open: `~/.blitz/run/blitz.sock`, or `$BLITZ_SOCKET`, or `--socket`. Its directory is created owner-only before the socket exists, so there is no window in which someone else could reach it. It is never on a network port, because it runs shell commands.

It speaks the API in `proto/blitz/v1` over [Connect](https://connectrpc.com): gRPC, gRPC-Web, or plain JSON over HTTP.

```bash
curl --unix-socket ~/.blitz/run/blitz.sock -H 'Content-Type: application/json' \
  -d '{"workspace": "/path/to/project"}' http://localhost/blitz.v1.WorkspaceService/GetModel
```

| Service | What it does |
|---|---|
| `SessionService` | Sessions, turns (a stream of events), steering, approvals and answers |
| `WorkspaceService` | Everything else a front end does: agents, models, settings, context, changes, skills, MCP, images, search |
| `FileService` | The workspace's files for the desktop app: listing, finding, reading, writing, renaming and deleting them, confined to the workspace directory |
| `ConfigService` | The global and per-workspace settings files, and API keys (never returned, only where each comes from) |
| `WorkerService` | Workers: listing, enabling, running, their history |

[Service API architecture](../architecture/service-api.md) covers the design.

## Workspaces

Every request names its workspace by absolute path. The service opens a workspace on first use, each with its own copy of the settings, and keeps it open for other clients. Opening is shared: two clients asking for the same workspace wait on one open, and other workspaces don't wait at all.

A workspace has one owner at a time, held with an OS file lock, so `blitz --local` on a workspace the service holds is refused. Closing a workspace is refused while a turn runs in it.

Approvals and questions travel on the running turn's event stream: the client that started the turn answers them. An approval with no client to answer, such as from a worker, is refused.

## Workers

Workers are workflows a workspace defines in `.agents/workers/<name>/WORKER.md` (or `workers/<name>/WORKER.md`), which the desktop app's **New worker** dialog writes for you and the service runs on a schedule, unattended:

```markdown
---
description: Report outdated Go modules
schedule: Weekdays at 9:30            # or cron "30 9 * * 1-5", or "@every 2h"
permissions: ["shell:go list -m -u all", "write:reports/"]
limits: { max_turns: 30, max_cost_usd: 0.50, timeout: 20m }
---
Check for outdated Go modules and write reports/deps.md.
```

- `agent:` and `model:` run a worker as another agent or on another model, for its runs only.
- `blitz workers` lists them. `blitz workers enable <name>` shows exactly what you're approving and enables that content; an edit disables it again.
- `blitz workers run <name>` runs one now; `blitz workers runs <name>` shows its history. Each run is a session you can open with `/resume`.
- A worker may only do what its `permissions` allow (`shell:`, `write:`, `delete:`, `web:`, `mcp:`), capped by `[workers.policy]`. Anything else is refused and recorded, and it can't ask questions. Permission modes don't apply to workers.

## Shutting down

On SIGINT or SIGTERM the service stops accepting calls, waits up to 10 seconds for the ones in progress, stops the workers' runs, closes every workspace, and removes the socket.
