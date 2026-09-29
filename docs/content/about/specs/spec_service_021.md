---
title: "021 · Service"
weight: 21
---

*The per-user service and its API* (`spec_service_021`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `proto/blitz/v1/{session,workspace,turn,worker}.proto`, `buf.yaml`, `buf.gen.yaml`; `apps/service/main.go` (`blitzd`), `apps/service/internal/{daemon,server}/*.go`, `apps/service/servicetest`; `pkg/socket`; `proto` (generated, never edited); `apps/cli/service.go` |
| Tests | `apps/service/*_test.go`, `apps/service/internal/{daemon,server}/*_test.go`, `apps/cli/attach_test.go`, `service_test.go` |
| Depends on | [spec_workspace_018](spec_workspace_018.md) |
| Used by | [spec_client_022](spec_client_022.md), [spec_workers_023](spec_workers_023.md), [spec_desktop_024](spec_desktop_024.md) |

## 1. Purpose

`blitzd`, its own program (`apps/service`), runs one engine process per user that holds every workspace a client opens, runs scheduled workers, and exposes `api.Backend` over Connect (gRPC, gRPC-Web or plain JSON) on a Unix socket. The CLI and the desktop app share one copy of each workspace through it. Handlers only translate between protos and `pkg/engine`; they hold no logic.

- **SVC-00** `blitzd [--socket PATH] [--config FILE]`: no arguments (a usage error, exit 2, as a bad flag or a service already answering on the socket); `--version`; `--license[=full|third-party]` prints the NOTICE, the Apache License or the third-party notices and exits without starting. It takes no model, agent or workspace flags: each workspace's configuration decides. `blitz serve`, from before `blitzd` existed, is kept as a hidden command that runs `blitzd` (found as in SVC-50) with the same arguments, so login items installed by older versions keep working.

## 2. Transport and security

- **SVC-01** Socket: `$BLITZ_SOCKET` or `~/.blitz/run/blitz.sock` (`blitzd --socket` overrides it). The service is never on a network port: it runs shell commands.
- **SVC-02** `Listen`: create the directory 0700 **and chmod it** (no window where others could reach the socket), refuse if a service already answers (`ErrRunning` → exit 2), replace a stale socket, chmod the socket 0600. The socket is removed on exit.
- **SVC-03** HTTP/1.1 and unencrypted HTTP/2 (for gRPC clients); read-header timeout 10 s. Clients use base URL `http://blitz` with a dialer to the socket.
- **SVC-04** Shutdown on SIGINT/SIGTERM: stop accepting, wait up to 10 s for calls in progress, then close (turns still running are cut off); the scheduler stops its runs; all workspaces close.
- **SVC-06** `GetServiceInfo` reports the service's version (the release, or `dev`), the program it runs from (`os.Executable` at start; the file may have been replaced or removed since), when it started, its process ID, and `replaced`: whether the program file is no longer the one it started from (another file in its place, as a package install or a new build leaves, or none; `os.SameFile` against the file at start), so clients can tell a stale service (desktop DSK-51a). Services older than it answer `Unimplemented`.
- **SVC-06a** `ListLogDays` and `ReadLog` read the service's own diagnostic log ([spec_observability_003](spec_observability_003.md) LOG-06), in its `log.dir` (`WithLogDir`): the days there are and the directory; a day's records by level and text, newest first, with the file and how many matched. With `log.level = "off"` there's no directory: no days, no records. A bad day or level is `invalid_argument`.
- **SVC-05** Clients name their workspaces in each request. Request messages are limited to 32 MiB (an added image is the largest).

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
| `WorkspaceService` | `GetServiceInfo`, `ListLogDays`, `ReadLog` (SVC-06, SVC-06a), `ListWorkspaces`, `CloseWorkspace`, `GetSandbox`, `ListAgents`, `SetAgent`, `GetModel`, `SetModel`, `PinModel`, `UnpinModel`, `GetModelSettings`, `UpdateModelSettings` (every `[model_settings]` key, including `reasoning_effort` and `thinking_budget`), `GetSettings` (includes `permission_mode` and `effort`), `SetSetting`, `SetPermissionMode`, `ListCommands`, `ListPermissionRules`, `AddPermissionRule`, `RemovePermissionRule`, `ListSkills`, `GetSkill`, `ListEnvs`, `RemoveEnv`, `PruneEnvs`, `ListMCPServers`, `ListTools`, `ReloadMemory`, `AddMemory`, `ListLocales`, `SetLocale`, `ListCheckpoints`, `Undo`, `GetDiff`, `ListApprovals`, `RevokeApprovals`, `LoadImage`, `AddImage`, `GetSearchProvider`, `SearchWeb` |
| `FileService` | `ListDir`, `ReadFile`, `WriteFile`, `CreateFolder`, `RenameFile`, `DeleteFile`, `FindFiles`, `StatFiles` ([spec_files_029](spec_files_029.md) §3; reasons `FILE_CHANGED` with `current_version`, `BAD_PATH`, `FILE_NOT_FOUND`, `FILE_EXISTS`) |
| `ConfigService` | `DescribeConfig`, `SetApiKey`, `SecureApiKey`, `RemoveApiKey`, `SetProvider`, `SetConfigValue`, `DescribePermissions`, `AddPermission`, `RemovePermission`, `CheckPermission`, `SetReadOnlyDefaults`, `GetConfigFile`, `SaveConfigFile`, `CheckConfigFile`, `GetSettingsReference` (SVC-35) |
| `WorkerService` | `ListWorkers` (one workspace or every registered one), `CreateWorker`, `EnableWorker`, `DisableWorker`, `RunWorker`, `ListWorkerRuns`, `GetWorkerRun`, `WatchWorkerRun` (server stream) |

- **SVC-35** `ConfigService` edits the settings of a scope — `workspace` an absolute directory, `""` the global settings — through pkg/config ([spec_config_002](spec_config_002.md) §6), in the service's settings directory (`--config`). It never returns a key, only where each comes from. A change reloads the open workspaces it touches (every one for the global scope): each reloads its configuration and rebuilds its model and its agents' pinned models (`Workspace.ReloadProviders`; only the providers and the default model change; other settings apply when a workspace next opens; a pin that can't be built keeps its model), and the response carries the scope's workspace's model error, if any. A workspace whose model couldn't be built tries again (`Workspace.RetryModel`) when `GetModel` asks and before each turn or worker run, so a sign-in made outside Blitz (gcloud's Application Default Credentials, `ant auth login`) is noticed without a settings change. Invalid input (a relative directory, an unknown provider or setting, a file that doesn't decode) is `InvalidArgument`.

## 5. Turns over the stream

- **SVC-20** `RunTurn(workspace, session_id, Turn)` streams `TurnEvent{author, oneof: accepted | text | tool_call | tool_result | approval_request | question | tasks | finished}`. Events are sent under a lock (the agent and approval requests can produce them concurrently). The last event is always `finished{output, before, after, leftover, error}`; a hook refusal finishes with reason `PROMPT_BLOCKED`. Cancelling the call interrupts the turn.
- **SVC-21** `accepted` marks the prompt recorded; from then on the client may `Steer`.
- **SVC-22** Approval broker: the workspace's approver and question prompter become `approval_request{request_id, tool, kind, detail, diff, scope_label}` / `question{request_id, question, options}` events on **the running turn's stream**, and the agent waits until `Approve(request_id, decision)` or `Answer(request_id, text)` arrives, or the turn ends. Request IDs are random 96-bit hex. Answering an unknown or expired request → `NotFound UNKNOWN_REQUEST`. Decisions: `DENY`, `ONCE`, `SESSION` (until the service restarts), `ALWAYS` (saved); unspecified → `INVALID_DECISION`.
- **SVC-23** An approval or question outside a turn with a client (e.g. from a worker) is refused (`no client is attached to answer`).

## 6. Errors

- **SVC-30** Errors are Connect errors with an `ErrorInfo{reason, metadata, message}` detail, mapped from `pkg/engine` typed errors. Reasons: `UNKNOWN_COMMAND`, `BAD_RULE`, `UNKNOWN_MODE`, `BYPASS_NEEDS_SANDBOX`, `MAX_TURNS`, `COST_LIMIT`, `TIME_LIMIT` (a turn stopped at a limit; `Turn.max_cost_usd` and `Turn.timeout` carry the limits), `INVALID_WORKSPACE`, `OPEN_FAILED`, `WORKSPACE_BUSY`, `SHUTTING_DOWN`, `NO_ACTIVE_SESSION`, `SESSION_NOT_FOUND`, `RESUME_FAILED`, `SNAPSHOT_NAME_TAKEN`, `PROMPT_BLOCKED`, `INVALID_TURN`, `UNKNOWN_REQUEST`, `INVALID_DECISION`, `UNKNOWN_AGENT`, `BAD_MODEL_REF`, `INVALID_SETTING`, `UNKNOWN_SETTING`, `INVALID_AGENCY`, `UNKNOWN_LOCALE`, `NOTHING_TO_COMPACT`, `UNDO_CONFLICT`, `GIT_FAILED`, `IMAGES_DISABLED`, `UNKNOWN_IMAGE`, `NO_FETCH`, `NO_SEARCH`, `SCRIPTS_DISABLED`, `SKILL_NOT_FOUND`, `UNKNOWN_WORKER`, `WORKER_INVALID`, `WORKER_DISABLED`, `WORKERS_DISABLED`, `HASH_MISMATCH`, `RUN_IN_PROGRESS`, `RUN_NOT_RUNNING`, `UNKNOWN_RUN`, `TOO_MANY_RUNS`, `NO_SCHEDULER`, `INTERNAL`.
- **SVC-31** Errors reported inside a response (e.g. a failed save, a turn failure) use the same `ErrorInfo`.

## 7. API evolution

- **SVC-40** Protos live in `proto/blitz/v1` (package `blitz.v1`), linted with buf `STANDARD`, breaking-change checked with `FILE`. Their Go code is generated by Bazel and never committed: one package, `github.com/retail-cortex/blitz/proto/blitz/v1` (`blitzv1`), holds the messages and Connect's clients and handlers (`protoc-gen-connect-go` with `package_suffix=`; `//proto/blitz/v1:blitzv1_go_proto`). So is the desktop page's TypeScript (`//apps/desktop/web:api_ts`, `protoc-gen-es` pinned by the page's lockfile). CI checks the protos' formatting and breaking changes with buf (spec_release_025 REL-04).
- **SVC-41** A deliberate breaking change is declared with a `Breaking-API: <why>` line in the commit message, which CI's `buf breaking` step honours.

## 8. Login item (`blitz service`)

- **SVC-50** The login item lives in `pkg/loginitem`, shared by the CLI and the desktop app (which installs, restarts and stops the service itself, DSK-51a). On Linux, installing restarts a running unit, so a reinstall runs the new program. `install`: finds `blitzd` beside the real (symlinks resolved) `blitz` executable, else on `PATH` (neither: usage error), and registers it; macOS writes `~/Library/LaunchAgents/dev.blitz.service.plist` (RunAtLoad, KeepAlive on unsuccessful exit, stdout/stderr to `~/.blitz/logs/service.log`) and `launchctl bootout`/`bootstrap gui/<uid>`; Linux writes `~/.config/systemd/user/blitz.service` (`Restart=on-failure`, `WantedBy=default.target`), `daemon-reload`, `enable --now`. Other OSes: usage error. Prints that it must be re-run after upgrading, and warns about API keys set only in the shell environment (a login item doesn't see them) by reloading the config without them.
- **SVC-51** `uninstall` stops and removes the item; `status` reports whether the item is installed and whether the service answers.
- **SVC-52** `loginitem.Start` and `Restart` start or restart the login item's service (`systemctl --user start|restart blitz.service`; launchd: `bootstrap` if unloaded, then `kickstart`, with `-k` to restart); without the item they are `ErrNotInstalled`.
- **SVC-53** The tray (`apps/tray`, `blitz-tray`): an icon in the system tray, in colour while the service answers on its socket and grey when it doesn't, with a tooltip and a first, disabled menu line saying so ("Blitz service: running (1.4.0)", "stopped" for a login item that isn't running, "not running"); then **Start the service** (while it's stopped), **Stop the service** and **Restart the service** (while it runs), **Open Blitz**, **Open the logs**, **Quit the tray** (the service keeps running). It asks every 2 s (`Probe`: the socket, and `GetServiceInfo` for the version and process). Start, Stop and Restart go through the login item when there is one (SVC-52), else Start runs `blitzd` from beside the tray or `PATH` on its own, and Stop sends the process that answered SIGTERM; each waits for the service to be (or not be) running, up to 15 s, and says "Couldn't: …" in the first line for 5 s when it fails. **Open Blitz** runs the bundle the tray is in (macOS) or `blitz-desktop` beside it or on `PATH`; **Open the logs** opens `log.dir` in the file manager. One tray at a time: it holds `~/.blitz/run/tray.lock` (flock) while it runs, and a second one exits.
- **SVC-54** The tray starts at login from its own entry: `blitz-tray --install` writes it and starts the tray (Linux: an XDG autostart entry, `$XDG_CONFIG_HOME/autostart/blitz-tray.desktop`, and the tray started now in a graphical session; macOS: a launchd agent `dev.blitz.tray`, `LimitLoadToSessionType` Aqua, which starts it); `--uninstall` removes it (on macOS, stopping it). The desktop app's **Show in the system tray** (Settings › Service, DSK-60) runs one or the other. Linux needs a StatusNotifierItem host: KDE and most desktops have one; GNOME has it with the AppIndicator extension (on by default on Ubuntu); without one there's no icon.
