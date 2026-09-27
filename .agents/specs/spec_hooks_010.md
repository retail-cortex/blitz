# spec_hooks_010 — Lifecycle hooks

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `internal/tools/scripthooks.go`; called from `internal/runtime/engine.go` and `internal/app/turn.go` |
| Tests | `internal/tools/scripthooks_test.go`, `posthooks_test.go` |
| Depends on | [spec_config_002](spec_config_002.md), [spec_shell_007](spec_shell_007.md) (process guard), [spec_observability_003](spec_observability_003.md) |

## 1. Purpose

Users run their own commands at three lifecycle points: before a tool (`pre_tool`, can block), after a tool (`post_tool`, observe only), and when a prompt is submitted (`prompt_submit`, can block). Hooks come from trusted config.

## 2. Configuration

`[[hooks.pre_tool]]`, `[[hooks.post_tool]]`, `[[hooks.prompt_submit]]`, each `{match, command, timeout_seconds, fail_closed}`.

- **HK-01** `match` is a tool-name glob (`path.Match`), empty matches all; ignored for `prompt_submit`. An empty command or invalid glob is a startup error.
- **HK-02** Hooks run as `bash -c <command>` in the workspace directory, **outside the OS sandbox with the user's full environment** (they are the user's own code), but still under the process guard. Default timeout 30 s.

## 3. Event

- **HK-10** The hook receives JSON on stdin: `event` (`pre_tool|post_tool|prompt_submit`), `session_id`, `workspace`, `tool`, `args`, `result`, `error`, `prompt` (fields as applicable).

## 4. Outcomes

- **HK-20** Block: exit code 2 (stderr is the reason, default "blocked by hook"), or exit 0 with stdout JSON `{"decision":"block","reason":"…"}`. The first blocking hook wins.
- **HK-21** Any other failure (non-zero exit, timeout) is audited and warned, and ignored — unless `fail_closed`, which blocks with "hook X failed and is fail_closed: …".
- **HK-22** Each run is a span `hook <event>` and is audited (`hook`, decision `block` or error).
- **HK-23** `pre_tool` block: the tool doesn't run; the model gets `{"error":"blocked by pre_tool hook: <reason>"}`.
- **HK-24** `prompt_submit` block: the prompt (including steer messages) is neither recorded nor sent; one-shot runs exit 4.

## 5. `post_tool` delivery

- **HK-30** `post_tool` hooks never delay the agent: the event is JSON-encoded when the tool finishes (so later mutation of result maps can't change it) and queued for a single background worker that runs events **in order**.
- **HK-31** The worker starts on first use. The queue holds 256 events; when full, events are dropped with one terminal warning ("falling behind") and a log entry per drop.
- **HK-32** A panic in one event is contained and logged; later events still run.
- **HK-33** On close, queued hooks get up to 5 s to finish, then running ones are killed.

## 6. Diagnostics
- **HK-40** `doctor` checks the first word of each hook command is resolvable (skipped when it contains `$;|&`).
