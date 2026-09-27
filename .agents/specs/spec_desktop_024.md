# spec_desktop_024 — Desktop app

| | |
|---|---|
| Status | Implemented, first version (reverse-engineered from `53f8c53`, plus the bundle fixes of 2026-09-26) |
| Source | Go side: `cmd/blitz-desktop/{main,app,proxy}.go`, `wails.json`, `go.mod` (own module). Page: `web/desktop/src/{main,App,Conversation,Workers}.tsx`, `api.ts`, `desktop.ts`, `turns.ts`, `style.css`, `gen/` (generated). Build: `web/desktop/{package.json,vite.config.ts,index.html}`, `build/desktop/{appicon.png,darwin/Info*.plist,windows/*}`, `Makefile` (`desktop`, `desktop-check`), `.github/workflows/ci.yml` (`desktop` job) |
| Tests | `cmd/blitz-desktop/proxy_test.go` (Go, `-race`), `web/desktop/src/turns.test.ts` (vitest); manual checks in MANUAL_VERIFICATION §37 |
| Depends on | [spec_service_021](spec_service_021.md) (all behaviour), [spec_workers_023](spec_workers_023.md), [spec_approvals_005](spec_approvals_005.md) |

## 1. Purpose and scope

`Blitz.app` is a window onto the per-user Blitz service: one tab per workspace, each with a conversation view and a workers view. It contains **no engine**. Every workspace, turn, approval, session and worker lives in the service (`blitz serve`), which keeps running — and keeps running workers — after the window closes. The app adds only what the service can't do for itself: detecting and installing the service, native dialogs, and a bridge from the web view to the service's Unix socket.

Non-goals of the first version (see §11): the REPL's slash commands, Markdown rendering, images, undo/diff, settings, and localisation.

## 2. Identity and packaging

- **DSK-01** Product name **Blitz**; macOS bundle `Blitz.app` (Wails project `name`), bundle identifier `dev.blitz.desktop`, executable `blitz-desktop` (Wails `outputfilename`), product version from `wails.json` `info.productVersion` (currently `0.1.0`, independent of the CLI's version), copyright "Ryan McGuinness, Apache License 2.0".
- **DSK-02** The app's executable must never be named `blitz`/`Blitz`: macOS file systems ignore case, and the bundled CLI (`Contents/MacOS/blitz`) would overwrite it. `make desktop` fails if the bundle's `MacOS` directory doesn't hold exactly `blitz-desktop` and `blitz` afterwards.
- **DSK-03** Minimum macOS 13.0: the build sets `-mmacosx-version-min=13.0`, and `LSMinimumSystemVersion` in `Info.plist`/`Info.dev.plist` says the same.
- **DSK-04** The CLI is bundled beside the app's executable (`Blitz.app/Contents/MacOS/blitz` on macOS; `build/desktop/bin/blitz` elsewhere) so the app can install the service without a separate CLI install. On macOS the bundle is re-signed ad hoc after adding it (adding a file breaks Wails's signature).
- **DSK-05** Windows packaging files (manifest, icon, version info, NSIS installer scripts) are Wails's templates and are not built or tested.

## 3. Architecture

```
┌──────────────── Blitz.app ────────────────┐        ┌──────── blitz serve ────────┐
│ Web view (React page, embedded dist/)     │        │ Connect API: Session,       │
│   ├─ Connect-ES clients ─► same origin ───┼─┐      │ Workspace, Worker services  │
│   └─ window.go.main.App.* (Wails binding) │ │      │ workspaces, turns, workers  │
│ Go (Wails v2):                            │ │ HTTP │                             │
│   ├─ asset server: page files             │ │ over │                             │
│   ├─ serviceProxy: /blitz.v1.* ───────────┼─┴─Unix─► ~/.blitz/run/blitz.sock    │
│   └─ App: ServiceStatus, InstallService,  │  socket└─────────────────────────────┘
│           ChooseWorkspace                 │
└───────────────────────────────────────────┘
```

- **DSK-10** Separate Go module (`cmd/blitz-desktop/go.mod`, cgo; WebKit on macOS, webkit2gtk on Linux) so the CLI module stays `CGO_ENABLED=0`. It imports `internal/server` only for the socket path, `BaseURL` and the socket HTTP client.
- **DSK-11** Window: title "Blitz", 1100×760, minimum 640×420. One Go `App` value is bound to the page (`window.go.main.App`).
- **DSK-12** The page is built by Vite from `web/desktop` directly into `cmd/blitz-desktop/dist` (go:embed can only reach files under its package) and embedded with `//go:embed all:dist`. The build empties the directory and writes back the committed `.gitkeep` placeholder so the Go package compiles before any page build. Build outputs (`dist/*`, `wailsjs/`, `build/desktop/bin/`) are not committed.

## 4. The service bridge (proxy)

- **DSK-20** A web view can't open a Unix socket, so the Go asset server's handler forwards every request whose path starts with `/blitz.v1.` to the service socket (`$BLITZ_SOCKET` or `~/.blitz/run/blitz.sock`, resolved at start), rewriting the URL to the service's `BaseURL`. The page's Connect client uses `window.location.origin` as its base URL, so it talks to its own origin and needs no CORS.
- **DSK-21** Responses stream as they arrive (`FlushInterval: -1`): turn events and worker-run events must appear as they happen. (Whether WebKit streams through Wails's asset server can only be checked manually — MANUAL_VERIFICATION §37.)
- **DSK-22** If the service can't be reached, the proxy answers `503` with Connect's JSON error shape `{"code":"unavailable","message":"the Blitz service isn't answering"}`, which the page's client reports like any API error.
- **DSK-23** Any other path the handler receives is `404`; the page's own files are served by Wails from the embedded assets.
- **DSK-24** Security: the socket is owner-only ([spec_service_021](spec_service_021.md) SVC-02); the proxy adds no listener of its own, so nothing new is reachable from the network. Only the embedded page runs in the web view. Model and tool output is rendered as text (React escapes it), never as HTML, and links in it aren't clickable, so output can't navigate the web view or inject script.

## 5. Bound Go methods

| Method | Behaviour |
|---|---|
| `ServiceStatus() → {running, installed, socket, cli}` | `running`: the socket answers (1 s dial). `installed`: the login item file exists (`~/Library/LaunchAgents/dev.blitz.service.plist` on macOS, `~/.config/systemd/user/blitz.service` on Linux; always false elsewhere). `cli`: the `blitz` binary found beside the app's executable (`blitz.exe` on Windows), else on `PATH`, else `""`. |
| `InstallService() → output` | Runs `<cli> service install` (starts the service now and at every login) and returns its output; the error includes the command's combined output. Fails if no CLI is found. |
| `ChooseWorkspace() → dir` | Native directory dialog "Open a workspace" (may create directories); `""` when cancelled. |

- **DSK-30** Outside the Wails window the bindings don't exist; calling them throws "not running inside the Blitz desktop app".

## 6. Start-up and workspace tabs

- **DSK-40** On start the page asks `ServiceStatus` (showing "Starting…" meanwhile, or the error). If the service isn't running it explains that the service holds workspaces and runs scheduled workers and keeps running after the window closes, and offers **Install and start the service** (or **Start the service** when the login item exists); after installing it re-checks. Without a CLI it says to install Blitz's CLI and run `blitz service install`. The status is not re-checked later (§11).
- **DSK-41** With the service running, a tab bar shows one tab per workspace directory, labelled by its last path element (full path as tooltip), plus **+**, which opens the directory chooser; choosing an already open directory switches to its tab. With no tab: "Open a workspace to start."
- **DSK-42** Tabs exist only for the window's lifetime; there is no closing a tab and no restoring tabs at the next start. Closing a tab would not close the workspace in the service either way.
- **DSK-43** A workspace header shows the directory, "<active agent's display name> on <model>" (with "(model unavailable: …)" when the service couldn't build it), and a switch between **Conversation** and **Workers**. The service opens the workspace on the first request that names it.

## 7. Conversation view

API used: `SessionService.GetActiveSession`, `NewSession`, `ListSessions` (this workspace only), `LoadSession`, `RunTurn`, `Steer`, `Approve`, `Answer`; `WorkspaceService.GetModel`, `ListAgents`.

- **DSK-50** On opening, show the workspace's active session, or start a new one when there is none; load the session list. The list shows each session's title (or "(untitled)"; snapshots as "snapshot <name>") with its message count; **New session** and choosing a session (by ID; a snapshot branches a new session, as everywhere) are disabled while a turn runs. The list refreshes after each turn.
- **DSK-51** A session's saved transcript is shown as user and model entries. Loading a session doesn't restore tool activity (the transcript has none).
- **DSK-52** Composer: Enter sends, Shift+Enter adds a line; empty text is ignored. When idle the button is **Send** and the text starts a turn (shown at once as a user entry). While a turn runs the button is **Steer** — the text goes to `Steer`, shown as a user entry plus "Queued for the agent." — and **Stop** appears.
- **DSK-53** Turn rendering follows the event rules, implemented as pure functions in `turns.ts` and unit-tested: streamed text grows one open model entry and the final repeat closes it (text shown once); final text that wasn't streamed is its own entry; thoughts and streamed copies of tool calls are left out; a tool call is an entry `name summary …` that its result completes with `✓` or `✗ <error>` (matched by ID, else by name for ID-less results); `finished` closes open text and adds an error notice if the turn failed. The argument summary is the first of `path`, `command`, `url`, `query`, `question`, `pattern`, cut to 80 characters.
- **DSK-54** "Working…" shows while a turn runs and nothing is waiting for the user. The view scrolls to the newest entry.
- **DSK-55** After a turn, a usage line: `↳ <in> in · <out> out · context <ctx>` plus ` · $<turn cost>` when priced (thousands as `12.4k`; no session total, unlike the terminal).
- **DSK-56** **Stop** aborts the `RunTurn` call, which cancels the turn in the service; the view adds "Interrupted." and drops unread steer messages. Otherwise, leftover steer messages (sent after the agent's last tool call) are sent at once as the next turn, marked accepted (not recorded twice).
- **DSK-57** Errors are shown under the conversation using the service's message (a `PROMPT_BLOCKED` refusal of a prompt or steer message appears this way).

## 8. Approvals and questions

- **DSK-60** An `approval_request` event shows inline: "<tool> wants to: <detail>", the diff (plain, unhighlighted) when there is one, and buttons **Allow once** (focused), **Allow <scope> this session** and **Always allow <scope>** (only when the request has a scope label; "this session" means until the service restarts), **Deny**. The choice goes to `Approve`; the agent waits until then.
- **DSK-61** A `question` event shows the question, one button per option, and a free-text field ("Or answer in your own words"; Enter submits). The answer goes to `Answer`.
- **DSK-62** Only one request is shown at a time (the latest); it disappears once answered or when the turn ends. Answers that arrive after the turn ended fail with the service's `UNKNOWN_REQUEST`.

## 9. Workers view

API used: `WorkerService.ListWorkers` (this workspace), `EnableWorker`, `DisableWorker`, `RunWorker`, `WatchWorkerRun`, `ListWorkerRuns`.

- **DSK-70** A list of the workspace's workers with state (`new`, `enabled`, `disabled`, `changed since enabled`, `invalid`) and schedule; empty: "No workers: add workers/<name>/WORKER.md to this workspace." The list reloads after an action, not when files change.
- **DSK-71** A selected worker shows what enabling approves: description; state; schedule as written, as cron, time zone and next run; agent and model when set; permissions (or "read only"); limits (model calls, cost, timeout in minutes); content hash; and every problem.
- **DSK-72** **Enable as shown** (for workers neither enabled nor invalid; its tooltip asks to read `WORKER.md` first) enables **the displayed hash**, so an edit since it was shown fails with `HASH_MISMATCH`. **Disable** and **Run now** for enabled workers.
- **DSK-73** **Run now** starts a run, then watches its events live with the same rendering rules ("Running…" until the first event); a run that finished before watching began is simply recorded. Afterwards the run list refreshes.
- **DSK-74** Runs (latest 20): start time, status (`running`, `succeeded`, `failed`, `stopped at a limit`, `skipped`), manual or scheduled, cost, session ID, each refusal, and the error.

## 10. Build, test and CI

- **DSK-80** `make desktop` (needs pnpm; Linux also webkit2gtk): installs page dependencies from the lockfile, runs the pinned Wails CLI (`go tool -modfile=../../tools/go.mod wails build -clean`, which builds the page with `pnpm run build` = `tsc --noEmit && vite build`), then bundles the CLI (DSK-04) and re-signs on macOS. Output: `build/desktop/bin/Blitz.app` on macOS, `build/desktop/bin/blitz-desktop` plus `blitz` elsewhere.
- **DSK-81** `make desktop-check`: `pnpm test` (vitest), the page build, then `go vet` and `go test -race` in the desktop module. CI's `desktop` job (macOS) runs `make desktop-check` and then `make desktop`, so the bundle check of DSK-02 runs on every push.
- **DSK-82** Proxy tests: API calls, a streamed turn and its approval reach a real `internal/server` service (mock model) over a Unix socket through the proxy; without a service the proxy returns the Connect-shaped `unavailable` error. Page tests: the event-to-entry rules and the summaries.
- **DSK-83** Dependencies are pinned: React 19, Connect-ES 2, protobuf-es 2 (`protoc-gen-es` generates `src/gen` via `make proto`), Vite 8, TypeScript 5.9, vitest 5; pnpm 10.24 via `packageManager`.
- **DSK-84** `wails dev` is configured to run the page's Vite dev server (`frontend:dev:watcher` = `pnpm run dev`, server URL `auto`).

## 11. Known gaps and planned requirements

Not implemented; each is a candidate requirement for the next version (tracked in [spec_backlog_026](spec_backlog_026.md)):

- **Service lifecycle**: the status is checked only at start; if the service stops later, every call fails with "isn't answering" and there's no reconnect prompt.
- **Tabs**: no closing, no persistence across restarts, no `CloseWorkspace`.
- **Conversation features the service already offers but the page doesn't use**: slash commands (`/undo`, `/diff`, `/compact`, `/cost`, `/model`, `/agent`, `/pin_model`, `/model_settings`, `/session save`, `/rename`, `/memory`, `/approvals`, `/skills`, `/envs`, `/mcp`, `/tools`, `/sandbox`, `/locale`), `/plan`, `/btw`, `/search web|session`, images (`LoadImage`/`AddImage`), sessions from other workspaces, snapshots by name.
- **Rendering**: Markdown, diff highlighting, tool result detail, thought display, session totals in the usage line.
- **Localisation**: every string is English and hard-coded; the terminal's catalogs aren't used.
- **Workers**: no refresh on file changes; no link from a run's session to opening it in the conversation view.
- **Release**: no release job, signing or notarisation; the app's version isn't tied to the CLI's; Windows packaging untested.
