---
title: "023 · Workers"
weight: 23
---

*Workers (scheduled, unattended workflows)* (`spec_workers_023`)

| | |
|---|---|
| Status | Implemented, first version. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/workers/{worker,schedule,permission,policy,state,runs}.go`; `pkg/engine/workers.go`; `apps/service/internal/server/{scheduler,worker}.go`; `apps/cli/workers.go` |
| Tests | `pkg/engine/workers/*_test.go`, `pkg/engine/workers_test.go`, `apps/service/internal/server/worker_test.go`, `apps/cli/workers_test.go` |
| Depends on | [spec_approvals_005](spec_approvals_005.md) (unattended approvals), [spec_workspace_018](spec_workspace_018.md), [spec_service_021](spec_service_021.md) |

## 1. Purpose

A workspace defines workers in `.agents/workers/<name>/WORKER.md` (or `workers/<name>/WORKER.md`). The service runs enabled workers on their schedules, unattended, with exactly the permissions they declare (capped by host policy). Nothing runs until a person reviews and enables the exact content; editing disables it again.

## 2. Definition (`WORKER.md`)

```markdown
---
description: Report outdated Go modules
schedule: Weekdays at 9:30            # cron "30 9 * * 1-5", "@every 2h", or a phrase
timezone: Europe/Paris                # optional; default the machine's
permissions: ["shell:go list -m -u all", "write:reports/"]
limits: { max_turns: 30, max_cost_usd: 0.50, timeout: 20m }
catch_up: once                        # none (default) | once
overlap: skip                         # only "skip"
agent: qa                             # optional: runs as this agent
model: anthropic/claude-haiku-4-5     # optional: runs on this model
---
Check for outdated Go modules and write reports/deps.md.
```

- **WK-01** Worker roots: `[workers] paths` (default `.agents/workers`, then `workers`) relative to the workspace; a later root doesn't override an earlier worker of the same name. Directories without `WORKER.md` are ignored.
- **WK-01a** `CreateWorker(spec)` writes a new worker from a form's fields (`api.WorkerSpec`: name, description, schedule, time zone, agent, model, permissions, limits, catch-up, workflow) in the first root: `workers.Render` writes the frontmatter with only the fields set (YAML, so text is quoted as needed) and the workflow. It is loaded from a temporary copy first, as the scheduler would read it, and an agent that isn't defined is a problem too: with problems nothing is written and they're returned. A name any root already has is `ErrWorkerExists` (`WORKER_EXISTS`). The new worker is `new` (disabled) until reviewed and enabled (WK-10).
- **WK-01b** *2026-10-01.* Editing. `GetWorkerSpec(name)` returns a worker's `WORKER.md` as written (`workers.ReadSpec`: the schedule, time zone, permissions and limits as given, not as understood or capped by the policy) with the hash of its files. It reads what it can of a file that isn't valid, so a broken worker can be fixed from the form: unknown keys are ignored, and a file whose frontmatter can't be read comes back whole as the workflow. `UpdateWorker(spec, hash)` rewrites `WORKER.md` in the worker's directory only if its files still have that hash (`ErrHashMismatch`, `HASH_MISMATCH`: changed since they were read), and only if the result passes WK-01a's checks (otherwise its problems, nothing written). The name, which is the directory and keys the run log and the enabled state, can't change. The file is replaced atomically (a temporary dot file beside the worker's directory, never in its hash). The new content has a new hash, so an enabled worker becomes `changed` and stops running until it's enabled again, as after any edit (WK-10); the service rescans at once. Writing the file drops YAML comments, keys Blitz doesn't know and `overlap` (only `skip`, the default).
- **WK-02** The directory name is the worker's name: `^[a-z0-9][a-z0-9_-]{0,63}$`; a frontmatter `name` must match it. Only valid names are looked up (they become file names).
- **WK-03** Frontmatter is decoded strictly (unknown keys are errors — a misspelling would silently change behaviour). A leading BOM and blank lines are tolerated; missing frontmatter delimiters are errors.
- **WK-04** Invalid if: bad name, empty workflow body, unparseable schedule or time zone, bad permission, bad or non-positive `limits.timeout`, negative limits, unsupported `overlap`/`catch_up`, hashing failure. An invalid worker still loads with its problems listed.
- **WK-05** Content hash: SHA-256 over every regular file under the worker directory (relative path, size, content; symlinks skipped; ≤ 64 MB), `sha256:<hex>`.

## 3. Schedules

- **WK-10** Accepted: five-field cron, descriptors (`@hourly`, `@daily`, `@weekly`, `@monthly`, `@every 90m`), or fixed plain-text phrases translated by rules, never guessed: `hourly|daily|weekly|monthly`, `every [N|word] minute(s)|hour(s)|day(s)` (non-divisors become `@every`), `daily|weekdays|weekends|every <weekday> at <time>` where time is `6`, `6 am`, `6:30 PM`, `18:30`, `noon`, `midnight`. Anything else is an error listing the accepted forms.
- **WK-11** `Next(t)` evaluates in the worker's time zone.

## 4. Permissions and policy

- **WK-20** A permission is `<kind>:<pattern>`: `shell:<command or glob>`, `write:<path glob>`, `delete:<path glob>`, `web:<host or search provider>`, `mcp:<server>:<tool glob>`. Write/delete paths are workspace-relative and may not be absolute or use `..`. Globs use `path.Match` (`*` doesn't cross `/`); a pattern ending in `/` covers everything below.
- **WK-21** A request is allowed only if it has targets and **every** target is covered by a permission of its kind. File targets must be clean workspace-relative paths. A shell command containing any of `; & | \` $ ( ) < > \ \n \r` is covered **only** by a permission that is exactly that command (so `go list *` can't cover `go list x; rm -rf ~`). Reading never needs permission.
- **WK-22** Host policy `[workers.policy]`: `allow` (permission kinds; default all five), `default_max_turns` 50, `default_max_cost_usd` 1, `default_timeout` 30m, `max_turns` 200, `max_cost_usd` 10, `max_timeout` 2h, `max_concurrent` 2. Disallowed permissions are dropped and caps applied, each with a note shown to the user.

## 5. Enabling

- **WK-30** State per (workspace, name) in `~/.blitz/workers.json` (the service is its only writer; atomic writes): `new`, `enabled` (at the current hash), `disabled`, `changed` (enabled at an older hash — doesn't run), `invalid`.
- **WK-31** `blitz workers enable <name>` shows the path, description, schedule (as written, as cron, time zone), effective permissions, limits, problems and content hash, asks `[y/N]` (or `-y`), then enables **the hash shown**; a change in between fails with `HASH_MISMATCH`. Invalid workers can't be enabled. If the service isn't running it says workers run in `blitz serve`.
- **WK-32** `disable`, `list` (name, state, schedule, cron, next run, description, problems), `runs <name> [-n 10]`, `run <name>` (runs now, prints events and the record; non-success exits 1). Request errors (unknown, not enabled, running, disabled, hash mismatch) exit 2. These go through the service when it runs, else in-process.

## 6. Runs

- **WK-40** A run requires the worker enabled at its current hash. The same worker never runs twice at once: a second request is `RUN_IN_PROGRESS`, and a skipped scheduled run is recorded (`skipped`).
- **WK-41** Each run gets its own session titled `⏰ <name> <YYYY-MM-DD HH:MM>` (openable with `/resume`) and recorded as the run's agent, and a prompt prefixed with an unattended preamble (nobody can answer; only permitted actions work; end with a summary of what was and wasn't done). The transcript records the workflow text.
- **WK-42** Unattended approvals ([spec_approvals_005](spec_approvals_005.md) APR-10): permitted requests pass; everything else is refused and recorded as a refusal `{tool, kind, detail, time}`. `ask_user_question` is refused.
- **WK-43** Limits: `max_turns` (model calls), `max_cost_usd` (checked after each event; cancels the run), `timeout` — applied through the turn's own limits (WS-29), so the run's error is the limit's message. Status: `succeeded`, `failed`, `limited` (turns, cost or time, with the cause as error), `skipped`; `running` while live.
- **WK-45** Agent and model: a run uses the worker's `agent` (else the workspace's active agent) and `model` (else the workspace's), for that run only ([spec_engine_016](spec_engine_016.md) ENG-16); the workspace's active agent and model are unchanged. An undefined agent is listed as a problem and fails the run; a model that can't be built fails the run with the (secret-masked) reason. `workers enable` shows both so they are part of what is approved.
- **WK-44** Run log: one JSONL file per worker, `~/.blitz/worker-runs/<hex(sha256(workspace))[:16]>-<name>.jsonl` (0600): `id` (`YYYYMMDDTHHMMSS-<16 hex>`), workspace, worker, hash, status, manual, started, duration, cost, calls, session, refusals, error. Torn last lines are skipped. Listed newest first.

## 7. Scheduler (service)

- **WK-50** Runs only in the service (`blitzd`) with `[workers] enabled`. It watches every workspace that has at least one enabled worker, even when no client has it open, rescanning every minute to pick up new, edited and removed workers; a changed hash, cron or time zone re-registers the entry.
- **WK-51** Concurrency: `max_concurrent` slots across workspaces. A manual run fails at once with `TOO_MANY_RUNS` when all are busy; a scheduled run waits, but a worker already waiting isn't queued again (a worker that falls behind runs once, not once per missed tick).
- **WK-52** `catch_up: once`: when a worker is first registered and a scheduled time passed since its last run, it runs once as soon as possible.
- **WK-53** Live runs keep their events so `WatchWorkerRun` replays them, then streams live ones until the run finishes; events aren't kept afterwards. Stopping the scheduler cancels all runs and waits for them.
- **WK-54** A run's file changes are its own checkpoint (BL-WK-01): its turn is a worker turn (`Checkpoints.BeginWorkerTurn`, the run's ID on it), which `/checkpoints` and `/undo` in interactive sessions leave out, and changes go to their own session's turn even while an interactive turn runs (FS-60a). The run record lists the files it changed (`files`; `blitz workers runs` shows them with the run's ID).
- **WK-55** `blitz workers undo <run-id> [--force]` (`WorkerService.UndoWorkerRun`, `Workspace.UndoWorkerRun`) restores those files as they were before the run, with `/undo`'s conflict rules (FS-63): a file changed since blocks it unless forced; `NOTHING_TO_UNDO` when nothing is left (undone already, or trimmed from the store). It's audited (BL-WK-02).

- **WK-56** `[workers] notify` (BL-WK-10): a shell command run after each scheduled run (and each scheduled run skipped because the last still ran) whose status is in `notify_on` (default `failed`, `limited`), never after manual runs. It gets the run record as a line of JSON on stdin, `BLITZ_WORKER` and `BLITZ_RUN_STATUS` in its environment, runs in the workspace outside the sandbox with a 30 s limit, and a failure is a warning. Only the user's settings may set it (never a project's).

## 8. Known gaps
- `service install` is needed for workers to keep schedules across logins; API keys must be in `~/.blitz/.env.toml`.
