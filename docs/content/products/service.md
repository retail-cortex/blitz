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

`blitz-tray`, beside the desktop app (in the `.deb` and `Blitz.app`, and in the Windows zip, where it also starts the service and opens the [desktop page](desktop.md#on-windows)), shows the service in the system tray as a bolt: filled while it runs, outlined when it doesn't (on Linux, amber or grey, so it shows on light panels too), and a menu with **Start**, **Stop** and **Restart**, **Open Blitz** and **Open the logs**, in the desktop app's language. While the service runs, **MCP servers** turns each of your MCP servers on or off, and **Workers** shows the runs going on (click one to stop it) and each worker, to run it now, turn it off or on, or show it in Blitz: workers run while the window is closed, so the tray is where they're within reach. Turn it on in the desktop app (Settings › Service › **Show in the system tray**) or with `blitz-tray --install`, which starts it now and at every login (an entry an older install left, naming a program that's gone, is put right when the app starts); `--uninstall` stops that. On GNOME the icon needs the AppIndicator extension, which Ubuntu has on by default; KDE and most other desktops show it as they are.

A login item doesn't see your shell's environment, so keep API keys in the keychain (`blitz config set-key`) or in `~/.blitz/.env.toml`.

## The socket

The service listens only on a Unix socket that its user can open: `~/.blitz/run/blitz.sock`, or `$BLITZ_SOCKET`, or `--socket`. Its directory is created owner-only before the socket exists, so there is no window in which someone else could reach it. It is never on a network port, because it runs shell commands.

It speaks the API in `proto/blitz/v1` over [Connect](https://connectrpc.com): gRPC, gRPC-Web, or plain JSON over HTTP.

```bash
curl --unix-socket ~/.blitz/run/blitz.sock -H 'Content-Type: application/json' \
  -d '{"workspace": "/path/to/folder"}' http://localhost/blitz.v1.WorkspaceService/GetModel
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

A workspace has one owner at a time, held with an OS file lock: `blitz --local` runs in a private service, which the shared one hands an idle workspace to, and is refused while a turn runs there. A service the CLI starts on demand (no login item) stops after 15 minutes with nothing to do; `blitz service install` keeps one running, which workers need. Closing a workspace is refused while a turn runs in it.

Approvals and questions travel on the running turn's event stream: the client that started the turn answers them. An approval with no client to answer, such as from a worker, is refused.

## Background runs

`blitz --bg "<prompt>"` starts a turn in the service and returns. The run gets a session of its own and keeps going with no client attached; what it asks waits until someone answers. `blitz agents` lists the runs in every workspace, `blitz attach <id>` follows one in the REPL (answering its requests, then carrying on in its session), `blitz logs <id> -f` watches one, and `blitz stop <id>` stops it. The desktop app's inbox shows the same. Runs end when the service stops.

## Workers

Workers are prompts a workspace runs on a schedule, unattended (`.agents/workers/<name>/WORKER.md`): the service runs them, which is why they need it kept running (`blitz service install`). `blitz workers` lists them, `blitz workers enable <name>` shows what one may do and enables it, and `blitz workers run <name>` runs it now. The whole story, from the file and its schedules to permissions, runs, undo and notifications, is in [Workers](../guide/workers.md).

## Shutting down

On SIGINT or SIGTERM the service stops accepting calls, waits up to 10 seconds for the ones in progress, stops the workers' runs, closes every workspace, and removes the socket.
