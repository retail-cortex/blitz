---
title: Extending
weight: 50
---

Specs: [memory](../about/specs/spec_memory_012.md), [MCP](../about/specs/spec_mcp_009.md), [hooks](../about/specs/spec_hooks_010.md), [agents and commands](../about/specs/spec_agents_014.md).

## Project memory

`AGENTS.md`, `CLAUDE.md`, `GEMINI.md` and `BLITZ.md`, from the repository root down to the workspace, plus `~/.blitz/BLITZ.md`, are added to the agents' instructions, so a repository set up for another agent works unchanged. A file with the same content under two names loads once. `BLITZ.local.md` and `CLAUDE.local.md` are personal, and Blitz warns if git tracks them.

A line can import another file with `@docs/style.md`: relative to the file, at most five deep, never from outside the repository (or `~/.blitz` for your own files), and never a blocked path.

Rule files in `.blitz/rules/`, `.agents/rules/`, `.claude/rules/` and `~/.blitz/rules/` load too. One with frontmatter `paths: ["src/api/**/*.go"]` is given to the agent only when it first reads or edits a matching file. None of these files can grant permissions. `/memory` lists what's loaded; `/init` has the agent write `BLITZ.md`.

## MCP servers

```toml
[[mcp.servers]]
name    = "github"
command = "npx"
args    = ["-y", "@modelcontextprotocol/server-github"]
env     = { GITHUB_PERSONAL_ACCESS_TOKEN = "..." }
# url = "https://example.com/mcp"   # streamable HTTP instead of stdio
# tools = ["create_issue"]          # an allow-list
# auto_approve = false
# sandbox = true                    # stdio servers run in the OS sandbox
# prefix = "gh"                     # expose tools as gh__create_issue
# agents = ["blitz", "qa"]          # who gets these tools; default: the primary agent; "*": all
```

MCP tools need approval per server and tool unless `auto_approve = true`, and can't shadow built-in tools (`prefix` avoids clashes). Listing a server's tools times out after 30 seconds and each call after `timeout_seconds` (default 300). A stdio server that crashes is restarted on the next call. After two failures in a row a server is paused, its tools hidden from the model, for 15 seconds, doubling up to 5 minutes; then one trial call is let through.

## Hooks

Hooks are your commands, run at points in the agent's work. Each receives a JSON event on stdin: `event`, `session_id`, `prompt_id`, `workspace`, `transcript_path`, `permission_mode`, `agent`, and the event's own fields (`tool`, `args`, `result`, `prompt`, …).

```toml
[[hooks.pre_tool]]
match   = "run_shell_command"   # a tool-name glob
command = "~/.blitz/hooks/check.sh"

[[hooks.post_tool]]
command = "jq -c . >> ~/tool-log.jsonl"

[[hooks.prompt_submit]]
command = "grep -qv 'password' || { echo 'no secrets' >&2; exit 2; }"
```

| Hook | Can |
|---|---|
| `pre_tool`, `prompt_submit` | Block the action: exit 2 (stderr is the reason) or print `{"decision":"block","reason":"…"}`. What a `prompt_submit` hook prints is given to the agent with the prompt |
| `session_start` | Add context for the agent |
| `stop` | Keep the agent going: `{"continue": true, "reason": "now run the tests"}`, at most five times per prompt |
| `permission_request` | Answer an approval: `{"decision": "allow"}` or `"deny"` (deny rules still come first) |
| `post_tool`, `post_tool_failure`, `session_end`, `subagent_start`, `subagent_stop`, `pre_compact`, `post_compact`, `notification` | Observe only. They run in the background, in order, and never delay the agent |

Other failures warn unless `fail_closed = true`. Hooks come from your own trusted settings, so they run outside the OS sandbox with your environment; they're still killed with Blitz.

## Custom commands

A Markdown file is a slash command: `~/.blitz/commands/fix.md` is `/fix`. In a project, `.blitz/commands/`, `.claude/commands/` and `.agents/workflows/` work too; the project's win, and `db/migrate.md` is `/db:migrate`. The body is the prompt, with `$ARGUMENTS` or `$1`…`$9` for what you type after the command.

```markdown
---
description: Fix a GitHub issue
argument-hint: <issue>
agent: qa                                  # run as this agent
model: anthropic/claude-haiku-4-5
allowed-tools: Read, Grep, Bash(git *)     # this turn may use only these
mode: plan                                 # read-only
---
Fix issue #$1: read it with `gh issue view $1`, find the cause, fix it and add a test.
```

`/review`, `/security-review` (both read-only), `/simplify` and `/verify` are bundled, and every skill is a command too. Built-in commands win name clashes; `/help` lists the custom ones.
