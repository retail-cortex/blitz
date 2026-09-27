# spec_service_021 — The per-user service and its API

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `api/blitz/v1/{session,workspace,turn,worker}.proto`, `buf.yaml`, `buf.gen.yaml`; `internal/server/*.go`; `internal/gen` (generated, never edited); `cmd/blitz/serve.go`, `service.go` |
| Tests | `internal/server/*_test.go`, `cmd/blitz/serve_test.go`, `service_test.go` |
| Depends on | [spec_workspace_018](spec_workspace_018.md) |
| Used by | [spec_client_022](spec_client_022.md), [spec_workers_023](spec_workers_023.md), [spec_desktop_024](spec_desktop_024.md) |

## 1. Purpose

`blitz serve` runs one engine process per user that holds every workspace a client opens, runs scheduled workers, and exposes `internal/app` over Connect (gRPC, gRPC-Web or plain JSON) on a Unix socket. The CLI and the desktop app share one copy of each workspace through it. Handlers only translate between protos and `internal/app`; they hold no logic.

## 2. Transport and security

- **SVC-01** Socket: `$BLITZ_SOCKET` or `~/.blitz/run/blitz.sock` (`--socket` overrides for `serve`). The service is never on a network port: it runs shell commands.
- **SVC-02** `Listen`: create the directory 0700 **and chmod it** (no window where others could reach the socket), refuse if a service already answers (`ErrRunning` → exit 2), replace a stale socket, chmod the socket 0600. The socket is removed on exit.
- **SVC-03** HTTP/1.1 and unencrypted HTTP/2 (for gRPC clients); read-header timeout 10 s. Clients use base URL `http://blitz` with a dialer to the socket.
- **SVC-04** Shutdown on SIGINT/SIGTERM: stop accepting, wait up to 10 s for calls in progress, then close (turns still running are cut off); the scheduler stops its runs; all workspaces close.
- **SVC-05** `serve` refuses `--dir` (usage error): clients name their workspaces. Request messages are limited to 32 MiB (an added image is the largest).

## 3. Workspaces in the service

- **SVC-10** Every request names its workspace by absolute directory. The server keys workspaces by canonical path (symlinks resolved), opens one on first use with a **fresh configuration per workspace** (a workspace owns and mutates its config), streaming on, warnings to the log, and the shared worker store.
- **SVC-11** Opening is asynchronous and de-duplicated: callers for the same workspace share one open; other workspaces don't wait. A caller whose context ends gets `Canceled`. After close, requests get `SHUTTING_DOWN`.
- **SVC-12** The workspace lock ([spec_workspace_018](spec_workspace_018.md) WS-03) means a workspace held by the service can't also be opened by `blitz --local` (`WORKSPACE_BUSY`).
- **SVC-14** `CloseWorkspace` refuses while a turn runs in any of the workspace's sessions (`TURN_RUNNING`, FailedPrecondition): closing would cut off the turn (maybe another client's), and a turn waiting for an approval would hold the close forever. Closing an idle workspace is safe for workers: the scheduler reopens workspaces when a run is due.
- **SVC-13** Images: each workspace keeps the 16 most recently used prepared images by ID for later turns (`LoadImage`/`AddImage` return IDs; `Turn.image_ids` refer to them; unknown → `UNKNOWN_IMAGE`).

## 4. Services

| Service | RPCs |
|---|---|
| `SessionService` | `ListSessions`, `GetActiveSession`, `NewSession`, `OpenSession`, `LoadSession`, `SaveSnapshot`, `RenameSession`, `RunTurn` (server stream), `Steer`, `Approve`, `Answer`, `GetUsage`, `Compact`, `SearchSession` |
| `WorkspaceService` | `ListWorkspaces`, `CloseWorkspace`, `GetSandbox`, `ListAgents`, `SetAgent`, `GetModel`, `SetModel`, `PinModel`, `UnpinModel`, `GetModelSettings`, `UpdateModelSettings` (every `[model_settings]` key, including `reasoning_effort` and `thinking_budget`), `GetSettings` (includes `permission_mode` and `effort`), `SetSetting`, `SetPermissionMode`, `ListCommands`, `ListPermissionRules`, `AddPermissionRule`, `RemovePermissionRule`, `ListSkills`, `GetSkill`, `ListEnvs`, `RemoveEnv`, `PruneEnvs`, `ListMCPServers`, `ListTools`, `ReloadMemory`, `AddMemory`, `ListLocales`, `SetLocale`, `ListCheckpoints`, `Undo`, `GetDiff`, `ListApprovals`, `RevokeApprovals`, `LoadImage`, `AddImage`, `GetSearchProvider`, `SearchWeb` |
| `WorkerService` | `ListWorkers` (one workspace or every registered one), `EnableWorker`, `DisableWorker`, `RunWorker`, `ListWorkerRuns`, `GetWorkerRun`, `WatchWorkerRun` (server stream) |

## 5. Turns over the stream

- **SVC-20** `RunTurn(workspace, session_id, Turn)` streams `TurnEvent{author, oneof: accepted | text | tool_call | tool_result | approval_request | question | tasks | finished}`. Events are sent under a lock (the agent and approval requests can produce them concurrently). The last event is always `finished{output, before, after, leftover, error}`; a hook refusal finishes with reason `PROMPT_BLOCKED`. Cancelling the call interrupts the turn.
- **SVC-21** `accepted` marks the prompt recorded; from then on the client may `Steer`.
- **SVC-22** Approval broker: the workspace's approver and question prompter become `approval_request{request_id, tool, kind, detail, diff, scope_label}` / `question{request_id, question, options}` events on **the running turn's stream**, and the agent waits until `Approve(request_id, decision)` or `Answer(request_id, text)` arrives, or the turn ends. Request IDs are random 96-bit hex. Answering an unknown or expired request → `NotFound UNKNOWN_REQUEST`. Decisions: `DENY`, `ONCE`, `SESSION` (until the service restarts), `ALWAYS` (saved); unspecified → `INVALID_DECISION`.
- **SVC-23** An approval or question outside a turn with a client (e.g. from a worker) is refused (`no client is attached to answer`).

## 6. Errors

- **SVC-30** Errors are Connect errors with an `ErrorInfo{reason, metadata, message}` detail, mapped from `internal/app` typed errors. Reasons: `UNKNOWN_COMMAND`, `BAD_RULE`, `UNKNOWN_MODE`, `BYPASS_NEEDS_SANDBOX`, `MAX_TURNS`, `COST_LIMIT`, `TIME_LIMIT` (a turn stopped at a limit; `Turn.max_cost_usd` and `Turn.timeout` carry the limits), `INVALID_WORKSPACE`, `OPEN_FAILED`, `WORKSPACE_BUSY`, `SHUTTING_DOWN`, `NO_ACTIVE_SESSION`, `SESSION_NOT_FOUND`, `RESUME_FAILED`, `SNAPSHOT_NAME_TAKEN`, `PROMPT_BLOCKED`, `INVALID_TURN`, `UNKNOWN_REQUEST`, `INVALID_DECISION`, `UNKNOWN_AGENT`, `BAD_MODEL_REF`, `INVALID_SETTING`, `UNKNOWN_SETTING`, `INVALID_AGENCY`, `UNKNOWN_LOCALE`, `NOTHING_TO_COMPACT`, `UNDO_CONFLICT`, `GIT_FAILED`, `IMAGES_DISABLED`, `UNKNOWN_IMAGE`, `NO_FETCH`, `NO_SEARCH`, `SCRIPTS_DISABLED`, `SKILL_NOT_FOUND`, `UNKNOWN_WORKER`, `WORKER_INVALID`, `WORKER_DISABLED`, `WORKERS_DISABLED`, `HASH_MISMATCH`, `RUN_IN_PROGRESS`, `RUN_NOT_RUNNING`, `UNKNOWN_RUN`, `TOO_MANY_RUNS`, `NO_SCHEDULER`, `INTERNAL`.
- **SVC-31** Errors reported inside a response (e.g. a failed save, a turn failure) use the same `ErrorInfo`.

## 7. API evolution

- **SVC-40** Protos live in `api/blitz/v1` (package `blitz.v1`), linted with buf `STANDARD`, breaking-change checked with `FILE`. Generated Go (`internal/gen`, `protoc-gen-go` + `protoc-gen-connect-go` pinned in `tools/go.mod`) and TypeScript (`web/desktop/src/gen`, `protoc-gen-es` pinned by the desktop lockfile) are committed. `make proto` regenerates; `make proto-check` fails if protos are unformatted or generated code is stale.
- **SVC-41** A deliberate breaking change is declared with a `Breaking-API: <why>` line in the commit message, which CI's `buf breaking` step honours.

## 8. Login item (`blitz service`)

- **SVC-50** `install`: resolves the real executable path; macOS writes `~/Library/LaunchAgents/dev.blitz.service.plist` (RunAtLoad, KeepAlive on unsuccessful exit, stdout/stderr to `~/.blitz/logs/service.log`) and `launchctl bootout`/`bootstrap gui/<uid>`; Linux writes `~/.config/systemd/user/blitz.service` (`Restart=on-failure`, `WantedBy=default.target`), `daemon-reload`, `enable --now`. Other OSes: usage error. Prints that it must be re-run after upgrading, and warns about API keys set only in the shell environment (a login item doesn't see them) by reloading the config without them.
- **SVC-51** `uninstall` stops and removes the item; `status` reports whether the item is installed and whether the service answers.
