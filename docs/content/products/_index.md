---
title: Products
weight: 20
---

Blitz ships as three programs over one engine.

| Program | What it is | Runs the engine |
|---|---|---|
| [`blitz`](cli.md) | The CLI: an interactive session (the REPL) or a one-shot run | No: it attaches to the service, starting it when it isn't running, or starts a private one for a run with settings of its own (`--local`) |
| [`blitzd`](service.md) | The per-user service: every workspace a client opens, the workers' schedules | Yes, for every client |
| [Blitz](desktop.md) | The desktop app: files, editor and chat, like an IDE | Through the service |

```mermaid
flowchart LR
    subgraph clients[Front ends]
        CLI["blitz (REPL, one-shot)"]
        Desk["Blitz desktop app"]
        Other["curl, gRPC clients"]
    end
    CLI -- "Connect over ~/.blitz/run/blitz.sock" --> D
    Desk -- "Connect over the socket" --> D
    Other --> D
    CLI -. "--local" .-> E2["a private blitzd, for the run"]
    subgraph D["blitzd"]
        W1["workspace A"]
        W2["workspace B"]
        S["worker scheduler"]
    end
```

A workspace (a folder opened in Blitz) has one owner at a time: the service, or the private service of one CLI running `--local`. When the service holds it, the terminal and the desktop app share its sessions, approvals and settings, and a session started in one can be continued in the other.

The CLI and the service ship together in each [release archive](../getting-started/_index.md#install); the desktop app is its own package, and carries its own copies of both.
