---
title: The service API
weight: 20
---

Specs: [service](../about/specs/spec_service_021.md), [client](../about/specs/spec_client_022.md).

## The protos

The API is defined in `proto/blitz/v1` (package `blitz.v1`) and served with [Connect](https://connectrpc.com), so one handler speaks gRPC, gRPC-Web, and JSON over HTTP.

| Proto | Service | RPCs |
|---|---|---|
| `session.proto`, `turn.proto` | `SessionService` | 16 |
| `workspace.proto` | `WorkspaceService` | 39 |
| `file.proto` | `FileService` | 8 |
| `config.proto` | `ConfigService` | 7 |
| `worker.proto` | `WorkerService` | 7 |

The generated code is never committed. Bazel generates one Go package (`blitzv1`: the messages, and Connect's clients and handlers) and a TypeScript client for the desktop page. buf lints the protos (`STANDARD`, with a comment on every service, RPC, message and enum) and CI checks each push for breaking changes against the previous commit (a pull request, against its base); a deliberate break is declared with a `Breaking-API: <why>` line in the commit message.

## Handlers translate, nothing more

`apps/service/internal/server` maps each RPC to a `Workspace` operation and back. It holds no logic of its own. Errors cross as Connect errors carrying an `ErrorInfo{reason, metadata}` detail (`WORKSPACE_BUSY`, `PROMPT_BLOCKED`, `COST_LIMIT`, …), which `pkg/client` maps back to the same typed errors `pkg/api` defines, so `errors.Is` works on either side of the socket.

## Workspaces by path

Every request names its workspace by absolute path. The server resolves symlinks, keys workspaces by canonical path, and opens one on first use. Opening is asynchronous and shared: concurrent callers for one workspace wait on the same open, and other workspaces aren't held up.

## Streaming a turn

`RunTurn` is a server stream of `TurnEvent`s, one of:

| Event | |
|---|---|
| `accepted` | The prompt was recorded; the client may now `Steer` |
| `text`, `tool_call`, `tool_result` | What the agent does, as it happens |
| `approval_request`, `question` | The agent is waiting on the client |
| `tasks` | The agent's task list changed |
| `finished` | Always last: the output, usage, leftover steer messages, or the error |

## Approvals across processes

In process, the engine asks an approver function and waits. In the service, a broker turns each request into an `approval_request` (with the diff and what "always" would remember) on the running turn's stream, and the agent waits until the client calls `Approve(request_id, decision)`. The client that started the turn answers it. An approval with no client attached, such as from a worker, is refused rather than left waiting.

## Why a Unix socket

The service runs shell commands for you, so it must be reachable only by you. It listens on `~/.blitz/run/blitz.sock` in an owner-only directory, created and restricted before the socket exists, and never on a network port. The desktop app's page, which can't open a socket, reaches it through a proxy in the app's own process, not a listener.
