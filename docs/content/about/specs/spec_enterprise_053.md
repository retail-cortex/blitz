---
title: "053 · Enterprise foundations"
weight: 53
---

*What the open-source Blitz gives a licensed enterprise service to build on: the service assembled from `pkg/` with extension points, and clients that connect to an organization's service, sign in through it, use its models and its catalog, and run jobs on it* (`spec_enterprise_053`)

| | |
|---|---|
| Status | **Draft** (the owner's request, 2026-10-27: "licensable closed source features that would include a configurable oAuth provider interface, … the ability for users to create and share workspaces, run jobs on schedules in the cloud and still access all of the current features") |
| Source | to come: pkg/service (the assembly, from `apps/service/internal/daemon`), `pkg/config` (`EnterpriseConfig`), pkg/enterprise (discovery, sign-in, the catalog cache), `pkg/engine/workers.go` (`runs_on`), `pkg/engine/gates` (checks on model messages), the desktop's Settings and model menu, `apps/cli` (`blitz enterprise`) |
| Tests | to come |
| Depends on | [spec_service_021](spec_service_021.md) (one service, many clients), [spec_workers_023](spec_workers_023.md) (jobs and their schedules), [spec_secrets_039](spec_secrets_039.md) (the keychain), [spec_gates_041](spec_gates_041.md) (rules and guardrails), [spec_studio_040](spec_studio_040.md) (what a workspace's `.agents` folder holds) |

## 1. Purpose

Companies want Blitz's agents with their own sign-in, their own approved models, rules every employee works under, workspaces they share, and jobs that run when a laptop is shut. That service is licensed and closed source, in its own repository (`blitz-enterprise`), and the customer runs it in their own Google Cloud project. This spec is the open-source half: the seams that service plugs into, and what the desktop app, the CLI and the tray do when they're pointed at one. Without an enterprise service set, nothing here changes how Blitz works.

The line: open source holds the interfaces, their open-source defaults and the clients; the enterprise repository holds the implementations (OIDC, RBAC, SCIM, licensing, the model gateway, the catalog's authoring, Git sync, sharing, cloud jobs, Terraform). Nothing public imports anything private.

## 2. The service, assembled from `pkg/`

- **EN-01** The service's assembly moves from `apps/service/internal/daemon` to pkg/service: `service.Run(ctx, service.Options)` builds and runs what `daemon.Run` does today. `apps/service/main.go` becomes flags and that call. Another Go module can't import internal/, so this is what lets the enterprise service reuse the whole service rather than copy it.
- **EN-02** `service.Options` takes the extension points, each an interface in `pkg/` with an open-source default that keeps today's behaviour:

  | Point | What it decides | Open-source default |
  |---|---|---|
  | `Authenticator` | who a request is from, from its credentials | the Unix socket's owner (only they can open it) |
  | `Authorizer` | whether that caller may do an action on a workspace or item | allow (one user) |
  | `Catalog` | the organization's agents, rules, skills, guardrails, MCP servers, connectors and policies (§5) | none |
  | `ModelRouter` | where a model's requests go and which models a caller may use | the person's own settings (`[llm.*]`) |
  | `JobRunner` | where a job runs (§6) | this service |
  | `Telemetry` | usage counts sent elsewhere | none |
  | `WorkspaceStore` | where a workspace's files come from and go back to | the local folder |

- **EN-03** A TCP listener with TLS (the page server's HTTP/2 stack), beside the Unix socket, used only when an `Authenticator` other than the default is given: every request then needs a bearer token the `Authenticator` accepts. The open-source build never listens on TCP for the API.
- **EN-04** `//tools:check_deps` gains a rule: nothing in this repository imports `github.com/retail-cortex/blitz-enterprise/...`.

## 3. Connecting to an organization's service

- **EN-10** `[enterprise]` in the settings: `url` (the service's address). Set by the person (Settings › **Organization**, `blitz enterprise connect <url>`) or by the company's device management in the machine-wide settings; a workspace's settings can't set it.
- **EN-11** Discovery: `GET <url>/api/v1/info` gives the service's name, version, its OIDC issuer and client ID, the token lifetime, and its capabilities (`models`, `catalog`, `jobs`, `sharing`, `workspaces`). Clients show only what's listed.
- **EN-12** Sign-in goes straight to the company's identity provider: the desktop app and the tray with authorization code and PKCE on a loopback redirect, the CLI with the device-code flow (`blitz enterprise login`). The refresh token is kept in the keychain (spec_secrets_039); the access token in memory. A token is renewed before it expires (the service sets how often, in hours); a refused renewal signs the person out and says so.
- **EN-13** Signed out, or the service unreachable: the window says which, with **Sign in** or **Retry**. Work that needs the service (its models, cloud jobs, shared workspaces) waits; local workspaces work as before, under the last catalog (EN-33).
- **EN-14** `blitz doctor` shows the service, whether it's reached, who is signed in and when the token renews.

## 4. The organization's models

- **EN-20** With `models` listed, the service is the model provider: the `api` provider (`[llm.api]`) is filled from the service (its base URL), with the access token in place of a token script, and the person's own providers are off unless the catalog's policy allows them.
- **EN-21** The model menu lists `GET <url>/api/v1/models` (sent with `If-None-Match`): each model's ID, display name, vendor, the API it speaks (Anthropic's Messages or OpenAI's Responses; the service translates for the models that speak neither), capabilities (tools, images, thinking, context size), price per million tokens and whether it's the default. The list is the caller's: the service filters it by role.
- **EN-22** Every model request carries `Blitz-Workspace`, `Blitz-Session` and `Blitz-Agent`, so the service applies the workspace's and the agent's guardrails to it.
- **EN-23** The guardrail checks the service runs on model traffic are the ones this repository runs (`pkg/engine/gates`: `RedactInjection`, `ConfirmInjection`, `CommandDenied`, `DomainAllowed`, the secret scan, the limits), exported for a caller that has a model's request and reply rather than an engine turn, so a rule means the same at the service as on the desktop.

## 5. The organization's catalog

- **EN-30** With `catalog` listed, the organization's agents, rules, skills, guardrails, MCP server and connector definitions (never their secrets), slash commands and policies come from the service, as a signed bundle with a version.
- **EN-31** A layer above the workspace's and the person's: an organization item is **locked** (it applies and can't be changed or switched off) or a **default** (a workspace or the person may override it). The policy may switch off Blitz's built-in items.
- **EN-32** Delivery: a stream from the service sends a new bundle as soon as it's published; when the stream isn't up (at start, after a reconnect), the bundle is fetched with `If-None-Match`, and again after its `Cache-Control: max-age`. A bundle whose signature doesn't check against the service's key is refused and the last one kept.
- **EN-33** The last good bundle is kept on disk with its expiry. Out of date past the expiry, organization workspaces stop until the bundle is fetched again; Blitz never falls back to working without the organization's rules.
- **EN-34** Studio and Settings show organization items with a badge and their lock; locked ones can't be edited, only copied into the workspace when they're defaults.
- **EN-35** The policy may also: allow or forbid the person's own providers, MCP servers and connectors; switch off `!` commands or named tools; set guardrails no workspace can loosen.

## 6. Jobs on the service

- **EN-40** A job has `runs_on`: `here` (the default, today's) or `service`, and **Runs on** in Studio › Jobs with **Move to the service**. One job runs in one place; there is no "here when on, the service otherwise".
- **EN-41** Before a job moves, Blitz checks it can run there and says what can't: an MCP server that runs only on this computer (stdio), a file outside the workspace, a `localhost` address, a connector the service doesn't have.
- **EN-42** A service job's runs show beside local ones in Studio › Jobs and the Processes view, with their output, from the service (`JobRunner`); the person's computer doesn't need to be on for them to run.

## 7. Not in scope

- The enterprise service itself: OIDC, roles and SCIM, licensing and the heartbeat, the gateway, the catalog's authoring (policy as code), Git sync, sharing, cloud jobs, the database and Terraform. Those are specified in `blitz-enterprise`.
- Making the person's own computer enforce the organization's rules against the person: a local agent runs under rules it fetched (EN-33), which is best effort; what a company must be sure of goes through the service (its models, its data, its jobs).
