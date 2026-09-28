---
title: "010 · Hooks"
weight: 10
---

*Lifecycle hooks* (`spec_hooks_010`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/tools/scripthooks.go`; called from `pkg/engine/runtime/engine.go` and `pkg/engine/turn.go` |
| Tests | `pkg/engine/tools/scripthooks_test.go`, `posthooks_test.go` |
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

## 5b. More events (ROADMAP 25.7)

- **HK-50** Every event carries `event`, `session_id`, `workspace` and `cwd`, `prompt_id` (one per accepted prompt; `tools.WithPromptID`), `transcript_path`, `permission_mode` and `agent`.
- **HK-51** Synchronous: `session_start` (reason `startup`, `resume`, `new`; its output — plain stdout or `additional_context` — is added to the session's next real prompt), `prompt_submit` (may block; its output is added to that prompt, or to a steer message), `stop` (after a successful non-aside turn, with `stop_hook_active` after the first; exit 2 or `{"continue": true, "reason": …}` sends the reason as a new prompt, recorded in the transcript as `(stop hook) …`, at most 5 times per prompt; cost/time limits span them, `max_turns` applies to each), `permission_request` (just before the user would be asked, after rules and modes; `{"decision":"allow"|"deny","reason"}` answers instead of the user, audited `hook-allow`/`hook-deny`).
- **HK-52** Background (like `post_tool`, same ordered queue and limits): `post_tool_failure` (a tool error or an `error` field; `post_tool` still fires for every call), `session_end` (reason `new`, `load` when another session becomes active, `exit` at close), `subagent_start` (`subagent`, `prompt`) and `subagent_stop` (`output`, `error`), `pre_compact` (`reason: manual`, or `rewind` for `/rewind`'s summarize modes; `prompt`: the focus) and `post_compact` (manual `/compact` and `/rewind` only; the ADK's automatic compaction has no hook point), `notification` (`type: permission_prompt` or `question`, `message`).
- **HK-53** JSON replies: `decision` (`block`, `allow`, `deny`, `ask`), `reason`, `continue`, `additional_context`. Blocks win; the first decision and any continue across a list of hooks count; contexts are joined. `config_change` doesn't apply (Blitz doesn't watch its config file).

## 6. Diagnostics
- **HK-40** `doctor` checks the first word of every hook command (all events) is resolvable (skipped when it contains `$;|&`).
