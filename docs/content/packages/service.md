---
title: "service"
weight: 4
---

The Blitz service itself, so another program can run it. Source: [`pkg/service`](https://github.com/retail-cortex/blitz/tree/main/pkg/service).

`pkg/service` is what `blitzd` runs: `service.Run` loads the settings, opens each workspace a client names (its own settings over the global ones), runs the jobs' schedules, and serves Blitz's Connect API on the per-user socket until its context ends, then lets turns in progress finish. `blitzd` is its flags and that call; the enterprise service builds on it ([spec_053_enterprise](../about/specs/spec_053_enterprise.md)).

## API

| Name | |
|---|---|
| `Run(ctx, Options)` | Serve until `ctx` ends; an error wrapping `socket.ErrRunning` when a service already answers |
| `Options` | The socket, the settings file, the version, idle exit, a private service's `RunOverrides`, and `Network` |
| `Network` | A second listener, over TLS, for a program that serves many people: every request needs a `Caller` its `Authenticator` accepts, and its `Authorizer`'s yes. blitzd never sets it |
| `CallerFrom(ctx)` | Who a network request is from |
| `server` | The Connect handlers over `engine.Workspace`, and the approval and question broker |
| `servicetest` | A service inside a test, on a socket of its own |

## Used by

apps/service; pkg/client, pkg/pageserver and apps/cli in their tests (`servicetest`).

Spec: [service](../about/specs/spec_021_service.md), [enterprise foundations](../about/specs/spec_053_enterprise.md).
