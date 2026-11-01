---
aliases: ["/about/specs/spec_enterprise_053/"]
title: "053 · Enterprise foundations"
weight: 53
---

*What the open-source Blitz gives a licensed enterprise service to build on: the service assembled from `pkg/` with extension points, and clients that connect to an organization's service, sign in through it, use its models and its catalog, run jobs on it, and wear its branding* (`spec_053_enterprise`)

| | |
|---|---|
| Status | **Draft** (the owner's request, 2026-10-27: "licensable closed source features that would include a configurable oAuth provider interface, … the ability for users to create and share workspaces, run jobs on schedules in the cloud and still access all of the current features"). Drafted 2026-11-01 for the owner's review, before the first release: the catalog's client side with **Publish to organization** (EN-31 to EN-36), organization workspaces with sync (§8, EN-60 to EN-65), both needing sign-in (EN-10 to EN-14) first |
| Source | `pkg/service` (the assembly: `service.go`; `network.go`: the network listener, `Authenticator`, `Authorizer`), `pkg/service/server` (`listen.go`: `ServeTLS`), `tools/check_deps.sh` (EN-04); to come: `pkg/config` (`EnterpriseConfig`), pkg/enterprise (discovery, sign-in, the catalog cache), `pkg/engine/workers.go` (`runs_on`), `pkg/engine/gates` (checks on model messages), the desktop's Settings and model menu, `apps/cli` (`blitz enterprise`) |
| Tests | `pkg/service/service_test.go`, `pkg/service/network_test.go` (`TestRunNetwork`, `TestNetworkCheck`, `TestGuard`), `pkg/service/server` (its tests, moved with it); to come |
| Depends on | [spec_021_service](spec_021_service.md) (one service, many clients), [spec_023_workers](spec_023_workers.md) (jobs and their schedules), [spec_039_secrets](spec_039_secrets.md) (the keychain), [spec_041_gates](spec_041_gates.md) (rules and guardrails), [spec_040_studio](spec_040_studio.md) (what a workspace's `.agents` folder holds) |

## 1. Purpose

Companies want Blitz's agents with their own sign-in, their own approved models, rules every employee works under, workspaces they share, and jobs that run when a laptop is shut. That service is licensed and closed source, in its own repository (`blitz-enterprise`), and the customer runs it in their own Google Cloud project. This spec is the open-source half: the seams that service plugs into, and what the desktop app, the CLI and the tray do when they're pointed at one. Without an enterprise service set, nothing here changes how Blitz works.

The line: open source holds the interfaces, their open-source defaults and the clients; the enterprise repository holds the implementations (OIDC, RBAC, SCIM, licensing, the model gateway, the catalog's authoring, Git sync, sharing, cloud jobs, Terraform). Nothing public imports anything private.

## 2. The service, assembled from `pkg/`

- **EN-01** *Built.* The service's assembly is in `pkg/service` (moved from apps/service/internal/daemon and apps/service/internal/server): `service.Run(ctx, service.Options)` opens each workspace a client names and serves it; `pkg/service/server` has the Connect handlers; `pkg/service/servicetest` runs it in other packages' tests. `apps/service/main.go` is the flags and that call. Another Go module can't import internal/, so this is what lets the enterprise service reuse the whole service rather than copy it.
- **EN-02** `service.Options` takes the extension points, each an interface in `pkg/` with an open-source default that keeps today's behaviour. `Authenticator` and `Authorizer` are built (`pkg/service/network.go`, with EN-03); each of the others is added with the feature that first uses it, so none is an interface nothing calls:

  | Point | What it decides | Open-source default |
  |---|---|---|
  | `Authenticator` | who a request is from, from its credentials | the Unix socket's owner (only they can open it) |
  | `Authorizer` | whether that caller may do an action on a workspace or item | allow (one user) |
  | `Catalog` | the organization's agents, rules, skills, guardrails, MCP servers, connectors and policies (§5) | none |
  | `ModelRouter` | where a model's requests go and which models a caller may use, for the engine when it runs in the service (jobs, §6); clients reach the organization's models through its gateway (§4) | the person's own settings (`[llm.*]`) |
  | `JobRunner` | where a job runs (§6) | this service |
  | `Telemetry` | usage counts sent elsewhere | none |
  | `WorkspaceStore` | where a workspace's files come from and go back to: on the client, the sync of an organization's workspace with the service (its clone through `<url>/git/<handle>.git`); agents never run in the service's own pod, only in job pods (§6) | the local folder |

- **EN-03** *Built.* `service.Options.Network` serves the API on a second listener, beside the Unix socket, over TLS (HTTP/2, and HTTP/1.1; `server.ServeTLS`). It's used only when the program running the service sets it (blitzd never does), and it needs a listener, a certificate and an `Authenticator`. Every request on it needs credentials the `Authenticator` accepts, else `401` with `{"code": "unauthenticated", …}`, and the `Authorizer` (when given) must allow its procedure, else `403` with `forbidden`. The caller is in the request's context (`service.CallerFrom`). `Network.Routes` are the program's own paths on the same listener (discovery, health checks, its own sign-in): a request one of its patterns matches goes there without the `Authenticator`, the program checking its callers itself, and every other request is Blitz's, guarded. An `Authenticator` or `Authorizer` that returns a `service.Refusal` answers with its own status and code (`403 terms_required`, say). The socket serves its owner as before. `Network.Wrap` sees every request first, so a program's own session can stand in for the credentials `Auth` reads (the enterprise admin console's cookie, its spec_011_admin AD-11). `Network.Plain` serves without TLS instead (HTTP/1.1, and HTTP/2 in the clear; `server.ServePlain`), for a platform that ends TLS in front of the program and forwards over its own network: Cloud Run's front end, say. Plain needs no certificate; everything else is the same.
- **EN-04** *Built.* `//tools:check_deps` checks that nothing in this repository imports `github.com/retail-cortex/blitz-enterprise`, and that the CLI, the tray and the Electron app's tools don't reach `pkg/service`.
- **EN-05** *Built* (2026-11-01; `pkg/webui`, `@blitz/webui`: `bazel test //pkg/webui:all`). **The page's controls as a package:** the desktop page's controls (`src/ui`: Button, IconButton, Tabs, Segmented, MultiSelect, Menu, Dialog, Snackbar, Switch, Field, Drawer, the charts), its styles and themes (`m3.css`, `glass.css`, `themes.css`, the palettes) move to `pkg/webui`, a TypeScript package (pnpm's `workspace:*`, linked by Bazel as `//pkg/webui:pkg`; its words come from the app's translator, `setTranslator`) the desktop page imports, so another program's web pages look like Blitz without copying it: the enterprise admin console (its spec_011_admin) builds on it from Blitz's Bazel module. It holds no Blitz-specific state or API calls. New shared controls (a data table, a diff viewer, a date range) are added there.

## 3. Connecting to an organization's service

- **EN-10** `[enterprise]` in the settings: `url` (the service's address). Set by the person (Settings › **Organization**, `blitz enterprise connect <url>`) or by the company's device management in the machine-wide settings; a workspace's settings can't set it.
- **EN-11** Discovery: `GET <url>/api/v1/info` gives the service's name, version, its OIDC issuer and client ID, the token lifetime, and its capabilities (`models`, `catalog`, `jobs`, `sharing`, `workspaces`, `branding`). Clients show only what's listed.
- **EN-12** Sign-in goes straight to the company's identity provider: the desktop app and the tray with authorization code and PKCE on a loopback redirect, the CLI with the device-code flow (`blitz enterprise login`). The refresh token is kept in the keychain (spec_039_secrets); the access token in memory. A token is renewed before it expires (the service sets how often, in hours); a refused renewal signs the person out and says so. The calls are JSON over HTTPS, so open-source clients need nothing from the service's code:
  - `POST <url>/api/v1/session` with `{"id_token", "client"}` (the provider's ID token, and the client and its version) answers `{"access_token", "refresh_token", "expires_in", "person": {"id", "email", "name", "roles"}, "terms": {"version"}}`; `terms` is there when there's a version to accept (EN-53).
  - `POST <url>/api/v1/session/refresh` with `{"refresh_token"}` answers the same, with a new refresh token: the old one ends the session if it's used again, so a client renews one at a time and keeps the new one before it uses it.
  - `DELETE <url>/api/v1/session` signs this session out; `GET <url>/api/v1/sessions` lists the person's sessions; `POST <url>/api/v1/sessions/end-others` signs the others out.
  - Every other request carries `Authorization: Bearer <access token>`.
  - Refusals are `{"code", "message"}`, the message written for the person: `401 unauthenticated` (sign in again), `403 forbidden`, `403 license_lapsed` (no new sign-ins), `403 terms_required` (EN-53).
- **EN-13** Signed out, or the service unreachable: the window says which, with **Sign in** or **Retry**. Work that needs the service (its models, cloud jobs, shared workspaces) waits; local workspaces work as before, under the last catalog (EN-33).
- **EN-14** `blitz doctor` shows the service, whether it's reached, who is signed in and when the token renews.

## 4. The organization's models

- **EN-20** With `models` listed, the service is the model provider: the `api` provider (`[llm.api]`) is filled from the service (its base URL), with the access token in place of a token script, and the person's own providers are off unless the catalog's policy allows them.
- **EN-21** The model menu lists `GET <url>/api/v1/models` (sent with `If-None-Match`): each model's ID, display name, vendor, the API it speaks (Anthropic's Messages or OpenAI's Responses; the service translates for the models that speak neither), capabilities (tools, images, thinking, context size), price per million tokens and whether it's the default. The list is the caller's: the service filters it by role.
- **EN-22** Every model request carries `Blitz-Workspace`, `Blitz-Session` and `Blitz-Agent`, so the service applies the workspace's and the agent's guardrails to it.
- **EN-23** *Built* (`pkg/guardrails`, a public package over the engine's private `pkg/engine/gates`). The guardrail checks the service runs on model traffic are the ones this repository runs (`pkg/engine/gates`: `RedactInjection`, `ConfirmInjection`, `CommandDenied`, `DomainAllowed`, the secret scan, the limits), exported for a caller that has a model's request and reply rather than an engine turn, so a rule means the same at the service as on the desktop.
- **EN-24** *Draft.* **Your usage and limits.** Settings › Models shows the person's own usage of the organization's models this day and month, by model (tokens and cost), with each budget and token quota that applies and what's left (`GET <url>/api/v1/usage`, the enterprise spec's GW-27). A request refused for a budget or quota (`budget_exceeded`, `quota_exceeded`) is shown as that, never as a model failure: which limit, what's left, when it resets, and the other models the person may still use. A request refused for a rate limit (`rate_limited`, GW-15) is sent again after its `Retry-After`, with **Waiting for <model>: busy** in the window, for up to 2 minutes before it's shown as an error. While the organization keeps full prompt logs that include the person (the enterprise spec's GW-21), Settings › Models says *"Your organization keeps what's sent to its models"*, from `/api/v1/usage`.

## 5. The organization's catalog

- **EN-30** *Checking built* (`pkg/catalog`: each file read as Blitz reads it, for the service's builds; the clients' side is to come). With `catalog` listed, the organization's agents, rules, skills, guardrails, MCP server and connector definitions (never their secrets), slash commands and policies come from the service, as a signed bundle with a version.
- **EN-31** A layer above the workspace's and the person's: an organization item is **locked** (it applies and can't be changed or switched off) or a **default** (a workspace or the person may override it). The policy may switch off Blitz's built-in items.
- **EN-32** Delivery (the service's side is the enterprise spec's: `GET <url>/api/v1/catalog`, signed with a key `/api/v1/info` lists as `catalog_keys`; its files at `/api/v1/catalog/files/<sha256>`; the stream at `/api/v1/catalog/watch`, server-sent events): a stream from the service sends a new bundle as soon as it's published; when the stream isn't up (at start, after a reconnect), the bundle is fetched with `If-None-Match`, and again after its `Cache-Control: max-age`. A bundle whose signature doesn't check against the service's key is refused and the last one kept.
- **EN-33** The last good bundle is kept on disk (EN-54) with its expiry. Out of date past the expiry, organization workspaces stop until the bundle is fetched again; Blitz never falls back to working without the organization's rules. The branding doesn't expire with it (EN-54).
- **EN-34** Studio and Settings show organization items with a badge and their lock; locked ones can't be edited, only copied into the workspace when they're defaults.
- **EN-36** *Draft.* **Publish to organization** in Studio, for a signed-in Admin: from an item's menu, or several chosen in the list, with each one's lock (`locked` or `default`) and groups. Blitz sends them to the service as a review (the enterprise spec's CA-04) and shows its state on the items (**In review**, with its problems if the checks found any, then **Published** with the version); another Admin approves it in the admin pages. Locked organization items show who may change them (Admins, through a review) instead of an edit button.
- **EN-35** The policy may also: allow or forbid the person's own providers, MCP servers and connectors; switch off `!` commands or named tools; set guardrails no workspace can loosen.

## 6. Jobs on the service

- **EN-40** A job has `runs_on`: `here` (the default, today's) or `service`, and **Runs on** in Studio › Jobs with **Move to the service**. One job runs in one place; there is no "here when on, the service otherwise".
- **EN-41** Before a job moves, Blitz checks it can run there and says what can't: an MCP server that runs only on this computer (stdio), a file outside the workspace, a `localhost` address, a connector the service doesn't have.
- **EN-42** A service job's runs show beside local ones in Studio › Jobs and the Processes view, with their output, from the service (`JobRunner`); the person's computer doesn't need to be on for them to run.

- **EN-43** A job's `on_request` says what a run does when it needs something its permissions don't allow: `refuse` (today's, WK-42; the default for jobs here) or `pause` (the default on the service). A paused run is saved, not kept running: its session with the waiting request, and its changes on its branch. Its process ends. The owner sees the request (a notification, the Jobs view, the tray's Jobs menu, `blitz jobs pending`) with **Approve** or **Deny**, whenever they get to it. The answer resumes the run from where it stopped, with the decision as the tool's result. Background processes the run had started don't come back. A run waiting longer than `approval_wait` (7 days by default) stops. While a run waits, the job's next times are skipped, as `overlap: skip` skips them while a run goes on.

## 7. Branding and terms

- **EN-50** With `branding` listed, `GET <url>/api/v1/branding` (with `If-None-Match`) gives:
  - the app's name and short name;
  - a logo and an icon, each for light and dark;
  - two accent colours, one for the light theme and one for the dark (no one colour reads well on both);
  - a support link and an about text;
  - the terms of service and privacy notice, each as text or a link, with a version.

  It needs no sign-in, so the sign-in screen is branded too. It comes signed with the catalog bundle's key (EN-32), the images inside it rather than linked, so one signature covers everything the window shows and an offline start has all of it. A copy whose signature doesn't check is refused and the last one kept. It's kept on disk (EN-54), so the window is branded before it reaches the service.
- **EN-51** The name replaces "Blitz" wherever the app says it at run time:
  - the window's title;
  - the page's menus, the welcome page and dialogs;
  - notifications;
  - the tray's tooltip and menu titles;
  - the CLI's banner and help.

  The logo replaces the mark in the title bar and on the welcome page. Each accent replaces its theme's accent tokens, and is refused, keeping Blitz's, when it doesn't meet WCAG AA contrast (4.5:1) against that theme's background. What the system names from the app's bundle can't change at run time: the macOS Dock and menu bar, the installer, the Start menu and file names. Those need a rebranded build, which is out of scope here.
- **EN-52** **About** shows the organization's name, logo, about text, terms of service, privacy notice and support link. It always ends with "Built on Blitz <version>", plus Blitz's license and notices (the Apache License's NOTICE must stay with the program).
- **EN-53** **The user agreement:** after sign-in, and whenever the terms' version changes, the window shows the terms with **Accept** and **Decline** before anything else. The CLI shows them, or their link, and asks `[y/N]`. Accept is recorded on the service with the version and the time (`POST <url>/api/v1/terms/accept` with `{"version"}`; `GET <url>/api/v1/terms` is the version in force and whether it's accepted). Decline signs out. Until the current version is accepted, the service answers every other request with `403 terms_required`, so no client can skip it.
- **EN-54** **Offline** *(the owner's question, 2026-10-29: "how it will be persisted when the user is offline")*:
  - **Where.** The last good bundle and branding, each as the signed copy the service sent, are kept in the service's own folder, `~/.blitz/enterprise/<the service's host>/` (0700, files 0600, each written to a temporary file and renamed into place), so another service's never mixes in. At start the window reads the branding from there and checks its signature again before it shows it; a copy that doesn't check is ignored (Blitz's own look until the service answers).
  - **No expiry.** The branding is how the app looks, not a rule, so it stays until a newer one replaces it. Past the bundle's expiry (EN-33) the organization's workspaces stop, but the window keeps the organization's name and logo. A service that stops listing `branding` returns the app to Blitz's.
  - **Signed out.** Signing out, or a token that can't be renewed, keeps the branding and the terms: it's still the organization's computer, and the sign-in screen (EN-13) wears its name.
  - **Disconnected.** Removing `[enterprise] url` (Settings › **Organization**, `blitz enterprise disconnect`, or device management) deletes the folder, and the app is Blitz again.
  - **The terms.** Their text and version are kept with the branding, so About shows them offline. Acceptance is recorded only on the service (EN-53). Offline, the version last accepted on this computer stands, and a newer one is asked for, before anything else, when the service answers again.

## 8. Organization workspaces

*Draft* (2026-11-01). The client's `WorkspaceStore` (EN-02): an organization's workspaces are Git repositories kept by the service (the enterprise spec's spec_007_workspaces), cloned to the person's disk, where Blitz's agents work as they do in any folder.

- **EN-60** **Open an organization workspace.** Signed in, the window's workspace picker and `blitz workspaces` list the workspaces the person has a role in (`GET <url>/api/v1/workspaces`), under the organization's name, with **Create a workspace**. Opening one the first time clones it through the service (`<url>/git/<handle>.git`) to `[enterprise] workspaces_dir` (`~/Blitz/<organization>/` by default) and opens it; after that it opens the clone. A Viewer's clone is read-only: Blitz's file tools and the editor refuse to change it, saying why.
- **EN-61** **The credential helper** `blitz git-credential` (Git's credential protocol: `get`, `store`, `erase`). Blitz sets it in each organization clone's own Git config (`credential.<url>.helper`), never the person's global config. `get` answers the service's host with the access token from the keychain (spec_039_secrets), renewed first when it's within its last minute (EN-12); signed out, it answers nothing, so Git fails with "sign in to Blitz", and Blitz says so. A person may set it globally to use Git's own clients with the service.
- **EN-62** **Sync** (for a workspace with `sync` on; the enterprise spec's WS-04). Blitz's service, not the window, runs it, so it goes on with the window closed:
  - **Saving:** a change on disk (the person's, or an agent's write) starts a timer; 5 seconds after the last change, and never while an agent's turn in that workspace is still writing, Blitz commits everything (`git add -A`, its ignore rules included) with a message that says who changed what (*"Ana: edited plan.md"*, *"Coder (Ana's session): 4 files"*) and pushes.
  - **Getting others' changes:** Blitz fetches when the workspace opens, when the watch stream (WS-06) names a commit it doesn't have, and before each push. It rebases its own commits onto what it fetched. Changes from others land between turns: while a turn runs in the workspace, they wait for it to end, so an agent never has a file change under it mid-turn.
  - **A push refused** as not a fast-forward is fetched, rebased and tried again, up to 3 times, then retried as the watch stream brings news.
- **EN-63** **Conflicts.** When a rebase can't apply a change to a file, Blitz keeps both: the other person's version under the file's name, and this person's beside it as `name (conflict, Ana).ext`, committed and pushed like any change. The window shows **2 files have both versions** with each pair, and the agent's next turn is told of them. A file deleted by one and changed by the other keeps the changed one. Nothing is ever lost: the person's own commits stay in the clone's reflog.
- **EN-64** **The state is always shown, never in Git's words.** The status bar shows the workspace's sync state: **Saved**, **Saving…**, **Offline: saved on this computer** (commits wait and go when the service is back), **Signed out: sign in to sync**, or **Both versions kept** (EN-63). The window never says commit, push, pull or rebase for a sync workspace; its Changes view shows **History** instead, each save with who and when, and any one can be opened or restored as a new save.
- **EN-65** **Workspaces without sync** (code, the default for repositories with a build file) are plain clones: Blitz's Git features work as with any repository (Changes, branches, merges, worktrees), with the credential helper, and the watch stream only shows **New changes on main** in the window.

## 9. Not in scope

- The enterprise service itself: OIDC, roles and SCIM, licensing and the heartbeat, the gateway, the catalog's authoring (policy as code), Git sync, sharing, cloud jobs, the database and Terraform. Those are specified in `blitz-enterprise`.
- Making the person's own computer enforce the organization's rules against the person: a local agent runs under rules it fetched (EN-33), which is best effort; what a company must be sure of goes through the service (its models, its data, its jobs).
