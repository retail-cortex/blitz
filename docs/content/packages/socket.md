---
title: "socket"
weight: 3
---

Where the service listens, and how to reach it. Source: [`pkg/socket`](https://github.com/retail-cortex/blitz/tree/main/pkg/socket).

`pkg/socket` is the per-user Unix socket: its default path (`~/.blitz/run/blitz.sock`, or `$BLITZ_SOCKET`), listening safely (the directory created and restricted before the socket exists, a stale socket replaced, a running service refused), and an HTTP client that dials it. The service runs shell commands, so it is never on a network port.

## API

| Name | |
|---|---|
| `DefaultSocket` | The socket's path |
| `Listen(path)` | Listen, owner-only; `ErrRunning` if a service already answers |
| `Client(path)`, `BaseURL` | Reach the service |
| `Running(path)` | Whether a service answers |

## Used by

apps/cli, apps/desktop, apps/service, apps/service/internal/daemon, pkg/client.

Spec: [service](../about/specs/spec_service_021.md).
