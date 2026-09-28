---
title: "009 · MCP"
weight: 9
---

*MCP servers* (`spec_mcp_009`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/tools/mcp.go`; `pkg/engine/breaker` |
| Tests | `pkg/engine/tools/mcp_test.go`, `mcp_resilience_test.go`; `pkg/engine/runtime/mcp_agents_test.go` |
| Depends on | [spec_shell_007](spec_shell_007.md) (exec env, sandbox), [spec_approvals_005](spec_approvals_005.md), [spec_models_015](spec_models_015.md) (breaker) |

## 1. Purpose

Model Context Protocol servers extend agents with external tools, over stdio (a child process) or streamable HTTP. Blitz decides which agents see which servers, prevents name clashes with built-in tools, gates calls with approval, bounds calls with timeouts, and pauses unhealthy servers.

## 2. Configuration (`[[mcp.servers]]`)

- **MCP-01** Each server needs a unique `name` and exactly one of `command` (stdio, with `args`, `env`) or `url` (streamable HTTP); otherwise startup fails.
- **MCP-02** Optional: `tools` (allow-list), `auto_approve` (skip approval), `sandbox` (stdio servers run in the OS sandbox unless `false`), `prefix` (expose `create_issue` as `<prefix>__create_issue`), `agents` (empty = the active primary agent only; `"*"` = every agent including `invoke_agent` sub-agents), `timeout_seconds` (per call; default 300).

## 3. Lifecycle

- **MCP-10** Servers connect lazily when their tools are first listed (before a model call).
- **MCP-11** Stdio servers run through the exec environment: process guard, scrubbed environment plus the server's `env`, and the OS sandbox unless disabled. A new process is started on every (re)connect and the previous one is killed, so a crashed server is restarted on the next call.
- **MCP-12** All sessions (HTTP included) are tracked and closed when the workspace closes; stdio processes are stopped.

## 4. Tool exposure

- **MCP-20** Listing a server's tools times out after 30 s. Tools not in `tools` are dropped. A tool whose (prefixed) name would shadow a built-in tool is skipped with a warning; a tool that can't be renamed under a prefix is skipped. If two servers serve the same name, the first wins with a warning.
- **MCP-21** Ownership (tool → server) is recorded for approvals; prefixed tools call the server under the original name.
- **MCP-22** In plan and read-only modes every MCP tool is refused ([spec_engine_016](spec_engine_016.md) ENG-30).

## 5. Calls

- **MCP-30** Unless the server is `auto_approve`, each call needs approval: kind `mcp_tool`, detail `<tool> (MCP server "<server>")` plus sorted `key=value` args, key `mcp:<server>:<tool>`, target `server:tool`.
- **MCP-31** Each call is bounded by the server's timeout ("MCP tool X timed out after d").
- **MCP-32** Circuit breaker per server, threshold **2** consecutive transport failures (timeouts or "failed to call MCP tool" errors; tool errors reported by the server count as healthy). An open server's tools disappear from the model's list and calls fail "MCP server X is unavailable after repeated failures; retrying in d". Cooldown 15 s doubling to 5 min; then one trial call.
- **MCP-33** The terminal gets one warning when a server first fails, one when it is paused, and one when it recovers; every failure is logged.

## 6. Diagnostics
- **MCP-40** `doctor` checks each stdio command is on PATH, and with `--online` connects and counts tools. `/mcp` lists servers ([spec_workspace_018](spec_workspace_018.md) WS-72); `/tools` shows which servers are offered to the active agent.
