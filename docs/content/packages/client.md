---
title: "client"
weight: 2
---

`api.Backend` over the service's socket. Source: [`pkg/client`](https://github.com/retail-cortex/blitz/tree/main/pkg/client).

`pkg/client` is a workspace held by the service, reached over its Unix socket with Connect. `Attach` opens the workspace in the service and returns a `*Remote` that front ends drive exactly as they drive a local `*engine.Workspace`.

- Turns stream `RunTurn` events and convert them to `api.Event`s; approval and question events are answered through the approver the front end set.
- Service errors (`ErrorInfo` reasons) map back to `pkg/api`'s sentinel and typed errors, so `errors.Is` matches.
- `Close` leaves the workspace open in the service for other clients. Background processes belong to the service and survive the client.
- `AttachWorkers` does the same for the CLI's worker commands over `WorkerService`.

## API

| Name | |
|---|---|
| `Attach(ctx, socket, dir, warn)` | A `*Remote` for the workspace in `dir` |
| `AttachHTTP` | The same over any HTTP client (tests) |
| `AttachWorkers` | The workspace's workers, over `WorkerService` |

## Used by

apps/cli.

Spec: [client](../about/specs/spec_client_022.md).
