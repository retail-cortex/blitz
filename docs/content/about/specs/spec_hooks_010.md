---
title: "010 · Hooks"
weight: 10
---

*Lifecycle hooks* (`spec_hooks_010`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/tools/scripthooks.go`, `hookif.go`; called from `pkg/engine/runtime/engine.go` and `pkg/engine/turn.go` |
| Tests | `pkg/engine/tools/scripthooks_test.go`, `posthooks_test.go`, `hookdecisions_test.go`; `pkg/engine/hooks_test.go` |
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
- **HK-52** Background (like `post_tool`, same ordered queue and limits): `post_tool_failure` (a tool error or an `error` field; `post_tool` still fires for every call), `session_end` (reason `new`, `load` when another session becomes active, `exit` at close), `subagent_start` (`subagent`, `prompt`) and `subagent_stop` (`output`, `error`), `pre_compact` (`reason: manual`, or `rewind` for `/rewind`'s summarize modes; `prompt`: the focus) and `post_compact` (manual `/compact` and `/rewind` only; the ADK's automatic compaction has no hook point), `notification` (`type: permission_prompt`, `question`, or `turn_finished` after a turn longer than `ui.notify_after` seconds; `message`).
- **HK-53** JSON replies: `decision` (`block`, `allow`, `deny`, `ask`), `reason`, `continue`, `additional_context`. Blocks win; the first decision and any continue across a list of hooks count; contexts are joined. `config_change` doesn't apply (Blitz doesn't watch its config file).

## 5c. Decisions, handler types and filters (spec_parity_027 §6.2)

- **HK-60** Replies may also carry `updated_args` (an object) and `system_message`. A `pre_tool` reply decides the call: `block` or `deny` stops it (the model gets `blocked by pre_tool hook: <reason>`); `updated_args` replace its arguments in place (the first hook's win; later hooks see them; the deny rules are checked again, and the tool still checks paths and the sandbox itself); `allow` lets the call through the approvals it would ask for (audited `hook-allow`; keyed by the tool call's ID, forgotten when it returns) but never past a deny or ask rule, plan mode, or the sandbox; `ask` puts the call to the user first (`MustAsk`, with the reason), and when approved it counts as allowed. `additional_context` goes to the agent with the tool's result as `hook_context`. Pre-tool hooks now run before an MCP tool's approval, so theirs applies to it.
- **HK-61** `system_message` from any hook is shown to the user (the turn's notice) and logged.
- **HK-62** `type = "http"`: the event is POSTed as JSON to `url` (with `headers`, where `$VAR` and `${VAR}` are replaced only for names in `allowed_env_vars`; others stay as written); a 2xx body is read like a command's stdout; other statuses and network errors are failures (`fail_closed` applies).
- **HK-63** `type = "prompt"`: a model judges the event against `prompt` and answers like a hook reply (JSON; `{"decision": ""}` when the criteria don't apply). The model is `model` (built at startup like a pinned agent's; a warning when it can't be, and the auto reviewer or the session's model judges instead), else `[permissions.auto] model`, else the session's; its tokens count in the session's `/cost`.
- **HK-64** `if = "<permission rule>"` runs a tool hook only for calls the rule names: `shell(…)` against each command of the script (as the command policy parses it), `read`/`write`/`delete(…)` against the call's `path`, `web(…)` against a `url`'s host, `skill(…)`, `agent(…)`, or a tool name. A rule that doesn't parse is a startup error.
- **HK-65** `args = [...]`, instead of `command`, runs a program directly without a shell. A hook needs what its type needs (`command` or `args`, not both; an http(s) `url`; a `prompt`), else startup fails.
- **HK-66** `/hooks` (REPL; `WorkspaceService.ListHooks`) lists every hook by event: what it runs, `match`, `if`, `fail_closed`, where it came from (the user's settings, or the project file of a trusted project), and its last five failures since the workspace opened.

## 6. Diagnostics
- **HK-40** `doctor` checks the first word of every command hook (all events; `args`' first) is resolvable (skipped when it contains `$;|&`); http and prompt hooks are listed.
