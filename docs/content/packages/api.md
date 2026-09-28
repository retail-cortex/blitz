---
title: "api"
weight: 1
---

The contract between front ends and the engine. Source: [`pkg/api`](https://github.com/retail-cortex/blitz/tree/main/pkg/api).

`pkg/api` defines `Backend`, the interface every front end drives, and the values, events and errors that cross it. The engine's `*engine.Workspace` implements it in process; `pkg/client`'s `*Remote` implements it over the service's socket. The REPL can't tell them apart.

It depends on nothing else in Blitz except `pkg/config` and `pkg/images`, and nothing in it reaches into the engine.

## API

| Name | |
|---|---|
| `Backend` | Everything a front end does: turns, sessions, agents and models, settings, context, changes, extensions, images and search ([workspace spec](../about/specs/spec_workspace_018.md) §3) |
| `Turn` | One prompt: its text, images, plan or read-only mode, limits (`MaxTurns`, `MaxCostUSD`, `Timeout`), pre-approved URLs, callbacks |
| `Event` | One part of the agent's output: text (partial, repeated, thought), a tool call, or a tool result |
| `ApprovalRequest`, `Approver`, `UserPromptFunc` | How the engine asks the front end before acting, and asks the user questions |
| Typed errors | `ErrNoActiveSession`, `ErrWorkspaceBusy`, `*BlockedError`, `ErrMaxTurns`, `ErrCostLimit`, `ErrTimeLimit`, … for front ends to word and translate |

## Used by

apps/cli, apps/cli/internal/tui, apps/service/internal/server, pkg/client, pkg/engine and its runtime, session, tools and workers packages.

Spec: [workspace](../about/specs/spec_workspace_018.md), [client](../about/specs/spec_client_022.md).
