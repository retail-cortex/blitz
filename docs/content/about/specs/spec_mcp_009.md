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
- **MCP-02** Optional: `tools` (allow-list), `auto_approve` (skip approval), `sandbox` (stdio servers run in the OS sandbox unless `false`), `prefix` (expose `create_issue` as `<prefix>__create_issue`), `agents` (empty = the active primary agent only; `"*"` = every agent including `invoke_agent` sub-agents), `timeout_seconds` (per call; default 300), `headers` (sent to an HTTP server), `disabled` (configured but not started).
- **MCP-03** `blitz mcp add <name> <command> [args…] | <url> [--env K=V] [--header K:V] [--prefix p] [--agents a,b]`, `list` (with "disabled" and "signed in"), `get` (values masked), `remove`, `enable`, `disable` edit the global settings' `[[mcp.servers]]` entries in place (`config.AddMCPServer`, `RemoveMCPServer`, `SetMCPServerDisabled`: other text and comments kept, a server's sub-tables removed with it; a name in use is refused) (PAR-MCP-01).
- **MCP-04** A project's `.mcp.json` and `.agents/mcp_config.json` (`{"mcpServers": {name: {command, args, env} | {url | httpUrl | serverUrl, headers}}}`) are the project's MCP servers, needing trust like those of `.blitz/settings.toml` and part of its hash ([spec_project_config_031](spec_project_config_031.md); PAR-MCP-06).

## 3. Lifecycle

- **MCP-10** Servers connect lazily when their tools are first listed (before a model call).
- **MCP-11** Stdio servers run through the exec environment: process guard, scrubbed environment plus the server's `env`, and the OS sandbox unless disabled. A new process is started on every (re)connect and the previous one is killed, so a crashed server is restarted on the next call.
- **MCP-12** All sessions (HTTP included) are tracked and closed when the workspace closes; stdio processes are stopped.

## 4. Tool exposure

- **MCP-20** Listing a server's tools times out after 30 s. Tools not in `tools` are dropped. A tool whose (prefixed) name would shadow a built-in tool is skipped with a warning; a tool that can't be renamed under a prefix is skipped. If two servers serve the same name, the first wins with a warning.
- **MCP-21** Ownership (tool → server) is recorded for approvals; prefixed tools call the server under the original name.
- **MCP-22** In plan and read-only modes every MCP tool is refused (the resource tools excepted, MCP-35) ([spec_engine_016](spec_engine_016.md) ENG-30).

## 5. Calls

- **MCP-30** Unless the server is `auto_approve`, each call needs approval: kind `mcp_tool`, detail `<tool> (MCP server "<server>")` plus sorted `key=value` args, key `mcp:<server>:<tool>`, target `server:tool`.
- **MCP-31** Each call is bounded by the server's timeout ("MCP tool X timed out after d").
- **MCP-32** Circuit breaker per server, threshold **2** consecutive transport failures (timeouts or "failed to call MCP tool" errors; tool errors reported by the server count as healthy). An open server's tools disappear from the model's list and calls fail "MCP server X is unavailable after repeated failures; retrying in d". Cooldown 15 s doubling to 5 min; then one trial call.
- **MCP-33** The terminal gets one warning when a server first fails, one when it is paused, and one when it recovers; every failure is logged.

## 5a. Resources, prompts, questions and sign-in

- **MCP-34** Each server gets its own MCP client (`MCPManager.mcpClient`): a middleware notes the session every request uses, so resources and prompts go over the connection the tools use (connecting first if need be).
- **MCP-35** Resources (PAR-MCP-02): the tools `list_mcp_resources(server?)` and `read_mcp_resource(server, uri)` — allowed in plan mode, approved like the server's tools (kind `mcp_tool`, action named after the tool), text up to 256 KB, binary content described; `@server:uri` in a prompt attaches the resource's text (`<resource name=…>`), as `@path` attaches a file.
- **MCP-36** Prompts (PAR-MCP-03): every server's prompts are custom commands `/mcp__<server>__<prompt>` (`ListCommands`, each server waited on at most 5 s; the argument hint lists `name=`), run with arguments as `name=value` or in the prompt's order; a missing required argument is refused. The prompt's text messages become the turn's prompt.
- **MCP-37** Elicitation (PAR-MCP-05): a server's request for input during a tool call is put to whoever that call's run asks (the user, or a background task's session: `Hooks.askUser`), one question per requested field (booleans and enums as choices; integers and numbers parsed), or, for a URL, whether to continue; nobody to ask, no call in progress, or no answer declines.
- **MCP-38** OAuth (PAR-MCP-04, `pkg/mcpauth`): `blitz mcp login <name>` runs the authorization-code flow with PKCE and dynamic client registration (go-sdk's `AuthorizationCodeHandler`), the redirect to a local `127.0.0.1` callback or, over SSH, the address the browser ends on pasted; the client, endpoints and token are kept in the secret store as `mcp-oauth-<name>`; `logout` forgets them. A signed-in HTTP server's requests carry the token, refreshed and kept as it expires; a server that asks for authorization without one is told to run `blitz mcp login`.

## 6. Diagnostics
- **MCP-40** `doctor` checks each stdio command is on PATH, and with `--online` connects and counts tools. `/mcp` lists servers ([spec_workspace_018](spec_workspace_018.md) WS-72); `/tools` shows which servers are offered to the active agent.
