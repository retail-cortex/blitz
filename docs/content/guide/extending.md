---
title: Extending
weight: 50
---

Specs: [memory](../about/specs/spec_memory_012.md), [MCP](../about/specs/spec_mcp_009.md), [hooks](../about/specs/spec_hooks_010.md), [agents and commands](../about/specs/spec_agents_014.md).

## Project memory

`AGENTS.md`, `CLAUDE.md`, `GEMINI.md` and `BLITZ.md`, from the repository root down to the workspace, plus `~/.blitz/BLITZ.md`, are added to the agents' instructions, so a repository set up for another agent works unchanged. A file with the same content under two names loads once. `BLITZ.local.md` and `CLAUDE.local.md` are personal, and Blitz warns if git tracks them. `/setup` (also `/init`, `blitz init`, and the desktop app's wand) makes the folder a git repository with a `.gitignore` for its stack, if you agree, and sets a project up the recommended way: one `.agents/AGENT.md`, with each of those four files a single `@.agents/AGENT.md` line, so Blitz, Claude Code, Gemini CLI and Codex all read the same instructions.

A line can import another file with `@docs/style.md`: relative to the file, at most five deep, never from outside the repository (or `~/.blitz` for your own files), and never a blocked path.

Rule files in `.blitz/rules/`, `.agents/rules/`, `.claude/rules/` and `~/.blitz/rules/` load too. One with frontmatter `paths: ["src/api/**/*.go"]` is given to the agent only when it first reads or edits a matching file. None of these files can grant permissions. `/memory` lists what's loaded; `/init` has the agent write `BLITZ.md`.

### Notes the agent keeps

The agent also keeps its own notes across sessions, with its `remember` tool: a fact about the project, how you like things done, or a mistake not to repeat. Each is a small Markdown file in `~/.blitz/memory/<workspace>-<hash>/`, with credentials masked before it's saved. They're added to the agent's instructions when a session starts (the newest first) as things it learned, never as permission to do anything.

```text
/memory notes                 # what it saved, newest first
/memory forget 20260930-1412  # delete one (the start of its name will do)
blitz memory list             # the same from the shell, plus show and edit ($EDITOR)
```

Set `[memory] auto = false` to turn them off.

### Output styles

A style changes how the agent writes its answers, not what it may do: `concise` keeps them short, `explanatory` explains its choices and the codebase as it works. `/style` lists them and `/style concise` switches (the desktop app has it in a workspace's settings); `[ui] style = "concise"` makes it the default. Add your own as Markdown in `~/.blitz/styles/<name>.md`, with an optional `description` in frontmatter.

For one run, `--append-system-prompt "…"` or `--append-system-prompt-file notes.md` adds to the agent's instructions.

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
# headers = { X-Team = "core" }     # sent to an HTTP server
# disabled = true                   # keep it configured without starting it
```

Or from the command line, which edits `~/.blitz/.env.toml` in place:

```sh
blitz mcp add github npx -y @modelcontextprotocol/server-github --env GITHUB_PERSONAL_ACCESS_TOKEN=...
blitz mcp add docs https://mcp.example.com/mcp --header "X-Team: core"
blitz mcp list | get <name> | remove <name> | enable <name> | disable <name>
blitz mcp login docs    # sign in with OAuth, in a browser; blitz mcp logout docs forgets it
```

`blitz mcp login` registers Blitz with the server's authorization server, opens the sign-in page, and receives the answer on a local address; over SSH, open the printed address anywhere and paste the address the browser ends on. The tokens are kept in the OS keychain (or an owner-only file) and refreshed as they expire; the model never sees them.

Besides tools, servers offer:
- **resources**, which the agent lists and reads with `list_mcp_resources` and `read_mcp_resource` (allowed in plan mode, approved like the server's tools), and which you attach to a prompt with `@server:uri`;
- **prompts**, each a slash command `/mcp__<server>__<prompt> [name=value …]` (arguments by name, or in order);
- **questions** during a tool call (elicitation), which Blitz asks you as the agent's own questions; unattended runs decline them.

A repository's `.mcp.json` (Claude Code's) and `.agents/mcp_config.json` (Antigravity's) are read as the project's MCP servers: like those of `.blitz/settings.toml`, they start only once you trust the project's settings.

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

A `pre_tool` hook can do more than block. Its JSON reply may carry `"decision": "allow"` (the call skips its approval question; deny and ask rules still apply), `"ask"` (you're asked first), `"updated_args"` (the call runs with these arguments instead), `"additional_context"` (given to the agent with the result) and, from any hook, `"system_message"` (shown to you).

Hooks needn't be shell commands:

```toml
[[hooks.pre_tool]]                 # only for git push
match   = "run_shell_command"
if      = "shell(git push *)"
args    = ["/usr/local/bin/check-branch"]   # run directly, no shell

[[hooks.pre_tool]]                 # ask a policy service
type    = "http"
url     = "https://hooks.internal.example/blitz"
headers = { Authorization = "Bearer $HOOK_TOKEN" }
allowed_env_vars = ["HOOK_TOKEN"]

[[hooks.pre_tool]]                 # or have a model judge
match  = "run_shell_command"
type   = "prompt"
prompt = "Deny anything that deletes outside the workspace or touches production."
```

`/hooks` lists them all, where each came from, and their recent failures.

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

## Plugins

A plugin bundles skills, commands, agents, hooks and MCP servers in one directory with a `plugin.toml`:

```text
lint-kit/
  plugin.toml          # name = "lint-kit", version = "1.2.0", description = "…"
  skills/lint/SKILL.md
  commands/fix-lint.md
  agents/linter.md
  hooks.toml           # [[post_tool]] … as [hooks]; ${BLITZ_PLUGIN_ROOT} is the plugin's folder
  mcp.toml             # [[servers]] … as [mcp]
```

```sh
blitz plugin install ./lint-kit                    # or a .zip, an https URL, a git repo, or a name from a marketplace
blitz plugin list
blitz plugin disable lint-kit                      # enable, remove, update [name]
blitz plugin marketplace add https://example.com/marketplace.toml
blitz plugin install lint-kit@acme
blitz --plugin-dir ./lint-kit "try it"             # load one for a run, without installing
blitz plugin import claude ~/src/some-claude-plugin    # or: import gemini <extension>
```

Installing shows everything the plugin adds, and hooks and MCP commands first, because they run code on your machine; it asks before going ahead (`--yes` skips the question). Blitz records a hash of the installed files and won't load a plugin whose files changed afterwards: `blitz plugin update` fetches it again. A marketplace lists each plugin with its hash, and a plugin that doesn't match isn't installed.

Plugins you enable load in every workspace. A project can turn some off, or on once you trust its settings:

```toml
# .blitz/settings.toml
[plugins]
enable  = ["lint-kit"]   # needs trust
disable = ["noisy-kit"]
```

The importers convert what maps from Claude Code plugins and Gemini CLI extensions (commands, skills, agents, hooks, MCP servers) and list what they couldn't. See the [plugins spec](../about/specs/spec_plugins_033.md).
