# spec_workers_023 — Workers (scheduled, unattended workflows)

| | |
|---|---|
| Status | Implemented, first version (reverse-engineered from `53f8c53`) |
| Source | `internal/workers/{worker,schedule,permission,policy,state,runs}.go`; `internal/app/workers.go`; `internal/server/{scheduler,worker}.go`; `cmd/blitz/workers.go` |
| Tests | `internal/workers/*_test.go`, `internal/app/workers_test.go`, `internal/server/worker_test.go`, `cmd/blitz/workers_test.go` |
| Depends on | [spec_approvals_005](spec_approvals_005.md) (unattended approvals), [spec_workspace_018](spec_workspace_018.md), [spec_service_021](spec_service_021.md) |

## 1. Purpose

A workspace defines workers in `workers/<name>/WORKER.md`. The service runs enabled workers on their schedules, unattended, with exactly the permissions they declare (capped by host policy). Nothing runs until a person reviews and enables the exact content; editing disables it again.

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

- **WK-01** Worker roots: `[workers] paths` (default `workers`) relative to the workspace; a later root doesn't override an earlier worker of the same name. Directories without `WORKER.md` are ignored.
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
- **WK-43** Limits: `max_turns` (model calls), `max_cost_usd` (checked after each event; cancels the run), `timeout`. Status: `succeeded`, `failed`, `limited` (turns, cost or time, with the cause as error), `skipped`; `running` while live.
- **WK-45** Agent and model: a run uses the worker's `agent` (else the workspace's active agent) and `model` (else the workspace's), for that run only ([spec_engine_016](spec_engine_016.md) ENG-16); the workspace's active agent and model are unchanged. An undefined agent is listed as a problem and fails the run; a model that can't be built fails the run with the (secret-masked) reason. `workers enable` shows both so they are part of what is approved.
- **WK-44** Run log: one JSONL file per worker, `~/.blitz/worker-runs/<hex(sha256(workspace))[:16]>-<name>.jsonl` (0600): `id` (`YYYYMMDDTHHMMSS-<16 hex>`), workspace, worker, hash, status, manual, started, duration, cost, calls, session, refusals, error. Torn last lines are skipped. Listed newest first.

## 7. Scheduler (service)

- **WK-50** Runs only in `blitz serve` with `[workers] enabled`. It watches every workspace that has at least one enabled worker, even when no client has it open, rescanning every minute to pick up new, edited and removed workers; a changed hash, cron or time zone re-registers the entry.
- **WK-51** Concurrency: `max_concurrent` slots across workspaces. A manual run fails at once with `TOO_MANY_RUNS` when all are busy; a scheduled run waits, but a worker already waiting isn't queued again (a worker that falls behind runs once, not once per missed tick).
- **WK-52** `catch_up: once`: when a worker is first registered and a scheduled time passed since its last run, it runs once as soon as possible.
- **WK-53** Live runs keep their events so `WatchWorkerRun` replays them, then streams live ones until the run finishes; events aren't kept afterwards. Stopping the scheduler cancels all runs and waits for them.

## 8. Known gaps
- Workers' edits don't join `/undo` checkpoints.
- `service install` is needed for workers to keep schedules across logins; API keys must be in `~/.blitz/.env.toml`.
