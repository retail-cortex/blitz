# spec_client_022 — Service client (attached front ends)

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `internal/client/{client,convert,workers}.go`; backend selection in `cmd/blitz/setup.go`, `workers.go` |
| Tests | `internal/client/client_test.go`, `internal/tui/remote_test.go` |
| Depends on | [spec_service_021](spec_service_021.md), [spec_workspace_018](spec_workspace_018.md) |
| Used by | [spec_cli_020](spec_cli_020.md), [spec_tui_019](spec_tui_019.md) |

## 1. Purpose

`client.Remote` is a workspace held by the service, reached over its socket, implementing `app.Backend`. Front ends drive it exactly like a local `*app.Workspace`, including typed errors.

## 2. Requirements

- **CL-01** `Attach(ctx, socket, dir, warn)` makes `dir` absolute, opens the workspace in the service (`GetModel`) and records whether its model works (`ModelErr`). `AttachHTTP` does the same over any HTTP client (tests).
- **CL-02** The CLI attaches automatically when the service answers on the socket, unless `--local`; it prints that it attached. Failing to attach is an error suggesting `--local`. The interface language catalogs are always the client process's own.
- **CL-03** `Close` leaves the workspace open in the service for other clients.
- **CL-04** `Processes()` is nil — background processes belong to the service and survive the client. `AuditShell` is a no-op (the `!cmd` ran locally, outside the service) — a known gap.
- **CL-05** `Run` streams `RunTurn`, converts events to `app.Event`, calls `OnAccepted` on `accepted`, and answers `approval_request`/`question` events through the UI set with `SetUI` (no approver, or an approver error → `DENY`; failures to send an answer are warned). `OnFinished` runs when `finished` arrives, after the service collected unread steer messages; a message sent later waits for the agent's next turn. The result carries output, usage and leftover; `finished.error` is converted to the typed error.
- **CL-06** Error conversion: an `ErrorInfo.reason` maps back to `internal/app`'s sentinel or typed error (e.g. `PROMPT_BLOCKED` → `*BlockedError`, `NO_ACTIVE_SESSION` → `ErrNoActiveSession`), wrapped so `errors.Is` matches while the message stays the service's.
- **CL-07** Operations whose `Backend` signature has no error (listings) return empty results and report the failure through `warn`.
- **CL-08** Images: `LoadImage`/`AddImage` upload to the service and turns reference images by ID.
- **CL-09** Workers: `AttachWorkers(socket, dir)` implements the CLI's worker operations over `WorkerService`; `RunWorker` starts a run, watches its events (`WatchWorkerRun`), and returns the record once the run has finished (immediately if it finished before watching began).
