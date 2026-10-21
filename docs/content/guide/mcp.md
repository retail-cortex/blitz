---
title: MCP servers
weight: 45
---

The [Model Context Protocol](https://modelcontextprotocol.io) lets the agent use tools other programs offer: your issue tracker, a database, a design tool, a company knowledge base. Add a server once, and its tools appear beside Blitz's own, with the same approvals.

Spec: [MCP](../about/specs/spec_mcp_009.md).

For PostgreSQL, AlloyDB, SQLite and BigQuery there's no server to add: Blitz has one built in, set up in **Studio › Connectors**. See [Databases](databases.md) and [Connectors](connectors.md).

## Add a server

Blitz speaks the two current transports: **stdio** (a command it starts) and **streamable HTTP** (a URL). The older SSE transport isn't supported.

```sh
# a command (stdio), with an environment variable
blitz mcp add github npx -y @modelcontextprotocol/server-github --env GITHUB_TOKEN=ghp_…

# a URL (streamable HTTP), with a header
blitz mcp add docs https://mcp.example.com/mcp --header "X-Team:core"

# then, for a server that signs in with OAuth
blitz mcp login docs
```

`--prefix gh` names its tools `gh__create_issue`; `--agents "*"` offers them to every agent (the default is the main agent only). The running service picks up a new server when it restarts: `blitz service restart`, or **Settings › Service** in the desktop app.

| Command | Does |
|---|---|
| `blitz mcp list` | The servers, which are disabled or signed in |
| `blitz mcp get <name>` | One server's settings, its environment and header values masked |
| `blitz mcp disable <name>`, `enable <name>` | Keep it configured without starting it, or start it again |
| `blitz mcp remove <name>` | Remove it |
| `blitz mcp login <name>`, `logout <name>` | Sign in to an HTTP server in the browser (OAuth), or forget the sign-in |
| `blitz doctor --online` | Connects to each server and counts its tools |

In a session, `/mcp` lists the servers, which are disabled, and whether their tools ask first, `/tools` shows which tools each agent gets, and `/status` names them. The desktop app has the same `/mcp`, `/tools` and `/status`; servers are added from the terminal or in the settings file.

## In the settings file

`blitz mcp add` writes to `~/.blitz/.env.toml`; you can write it yourself, with a few settings the command doesn't set:

```toml
[[mcp.servers]]
name = "github"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
env = { GITHUB_TOKEN = "ghp_…" }
# url = "https://example.com/mcp"   # streamable HTTP instead of a command
# headers = { X-Team = "core" }     # sent to an HTTP server
# tools = ["create_issue"]          # only these of its tools (their own names)
# prefix = "gh"                     # expose them as gh__create_issue
# agents = ["blitz", "qa"]          # who gets them; "*" for every agent
# auto_approve = false              # true: its tools don't ask first
# sandbox = true                    # stdio servers run in the OS sandbox
# timeout_seconds = 300             # the longest one tool call may take
# disabled = true                   # configured, never started
```

Each server needs a unique `name` and exactly one of `command` or `url`; otherwise the workspace won't open, saying why.

**Keep the file private.** `env` and `headers` values are stored as written, in plain text (Blitz masks them in its logs and in `blitz config show`). Only OAuth sign-ins (`blitz mcp login`) go to the system keychain. Prefer a server that reads its token from its own config, or OAuth, when you can.

## Servers a project brings

A repository can suggest servers in `.mcp.json`, `.agents/mcp_config.json` or `.blitz/settings.toml`, in the shape other tools use:

```json
{ "mcpServers": { "issues": { "command": "node", "args": ["tools/issues-mcp.js"] } } }
```

An entry Blitz can't start (an SSE server, or one with neither a command nor a URL) is skipped, and the project's settings review says why. They start only after you **trust the project's settings** (the desktop app asks; `blitz trust` in the terminal), and again after any change to them or the scripts they run. A project's server always runs in the sandbox and always asks before its tools run. A server of your own with the same name wins. Plugins can bring servers too ([plugins](extending.md#plugins)).

## Approvals and permissions

An MCP tool asks before it runs, like a shell command, showing the tool, the server and its arguments. **Allow once**, for the session or always, works per server and tool. `auto_approve = true` skips the question, for the server's tools and its resources alike (your `deny` and `ask` rules still apply), and you can write rules for them:

```toml
[permissions]
allow = ["mcp(github:list_*)"]
deny  = ["mcp(github:delete_*)"]
```

The rule names the server and the tool as the agent sees it (with its prefix, if it has one). Plan mode and read-only commands refuse MCP tools, except reading a server's resources. A [job](workers.md) may use one only with a `mcp:<server>:<tool>` permission.

## Prompts and resources

- **Prompts** a server offers become slash commands: `/mcp__<server>__<prompt>`, with arguments as `name=value` or in order. They're listed with your commands in `/help` and the ⌘K palette.
- **Resources** (files, records, documents a server exposes) are read with `@<server>:<uri>` in a prompt, or by the agent with `list_mcp_resources` and `read_mcp_resource`.
- **Questions a server asks** (elicitation) come to you like the agent's own questions; in an unattended run they're declined.

## When a server misbehaves

- **Startup:** stdio servers start the first time their tools are needed, not when the workspace opens. A crashed one starts again on the next call.
- **Failures:** after two failures in a row its tools pause for 15 seconds, then longer each time (up to 5 minutes), and come back by themselves. A tool call that runs past `timeout_seconds` fails.
- **Where to look:** a server going unavailable, being paused and coming back shows in the conversation where it happened. Everything else is in the service's log (**Settings › Logs** in the desktop app): look for `mcp server … unavailable`, `keeps failing` or `needs signing in again: blitz mcp login …`.
- **Names:** a tool named like one of Blitz's own is skipped, and when two servers offer the same name the first wins; a `prefix` fixes both.
- **Checks:** `blitz doctor` says whether each server's command is on the PATH; `blitz doctor --online` connects and lists its tools.
