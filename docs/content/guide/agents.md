---
title: Agents and tools
weight: 40
---

Spec: [agents](../about/specs/spec_agents_014.md).

## Built-in agents

| Agent | Role |
|---|---|
| `blitz` | The primary coding agent |
| `helios` | The Universal Constructor: builds and runs custom tools |
| `qa` | Test loops, edge cases, regression suites |
| `web-retriever` | Documentation and web research |
| `planning-agent` | Breaking down requirements; roadmaps |
| `agent-creator` | Writes agent definitions and skills |
| `model-judge` | Compares models |

`/agents` lists them and `/agent <name>` switches. The primary agent delegates to the others with `invoke_agent`, up to three levels deep.

Your own agents are Markdown files with YAML frontmatter in `~/.blitz/agents` (for every workspace) and the project's `.agents/agents`: a name, a description, the system prompt, the tools it may use, and optionally its own model. Built-in agents can't be overridden. Adding, editing or deleting a file applies from the next prompt, without a restart.

```markdown
---
name: quote-writer
display_name: Quote Writer
description: Picks and writes a daily quote
tools: [read_file, create_file]
default_model: anthropic/claude-sonnet-5   # optional
temperature: 0.9                           # optional model settings, for this agent only
effort: low
---
You are Quote Writer. Each time you run, choose a quote that…
```

An agent's own `temperature`, `top_p`, `max_tokens`, `effort` and `thinking_budget` apply only when it runs, over the model's settings and the session's effort. The desktop app edits these files: the **Agents** view lists the project's, and **Settings › Agents** your own.

Every agent, built-in or your own, is told to draw diagrams in Markdown as ```` ```mermaid ```` blocks rather than ASCII art; the desktop app and the docs draw them.

## Background tasks

An agent can hand work to another agent in the background: `invoke_agent` with `background: true` starts the sub-agent as a task (`task-1`, `task-2`, …) and returns at once, so the main agent keeps working with you. When the task ends, the main agent hears about it with its next step, or before your next message, and can read the whole result with `task_output`; `list_tasks` and `stop_task` do the rest.

- A task runs what your permission mode and rules allow. Anything it would ask about waits for you: the REPL asks at its prompt, labelled ("qa · task-3 asks"), and the desktop app shows the request beside the conversation (**Ctrl+J** jumps to the next).
- `/tasks` lists them (`/tasks show <id>`, `/tasks stop <id>`); the desktop app shows a card under the call, with **Show** and **Stop**. Exiting offers to stop or wait for them.
- Limits: `tools.max_background_agents` (4 at once), `tools.background_agent_timeout` (30 minutes), `tools.background_agent_max_turns` (50 model calls) and `tools.background_agent_max_cost_usd` (none). Their cost counts in the session's.
- `isolation: "worktree"` gives a background task its own git worktree and branch, so its changes stay out of your checkout until you merge them; `blitz worktrees list` shows them. You can start a whole session in one too: `blitz --worktree[=name]`.
- An agent's own file can set `background: true` (the default for `invoke_agent`), `max_turns` and `permission_mode` (for example `plan` for a reviewer that must not change anything). A project's agent may only tighten the mode until you trust the project.

## Tools

| Tools | |
|---|---|
| `read_file`, `list_files`, `glob`, `grep` | Reading and searching the workspace |
| `create_file`, `replace_in_file` (`edit`), `delete_snippet`, `apply_patch`, `delete_file`, `notebook_edit` | Changing it; `apply_patch` takes a unified diff or `*** Begin Patch`, atomically, across files; `notebook_edit` a Jupyter cell |
| `lsp` | Code intelligence from the language's server (gopls, typescript-language-server, pyright, rust-analyzer, or `[lsp.<language>]`): definitions, references, hover, symbols, diagnostics |
| `run_shell_command`, `manage_background_process` | Commands, in the OS sandbox |
| `web_fetch`, `web_search` | The web ([search](search.md)) |
| `browser` | A real browser: pages that need JavaScript, and your own web app ([search](search.md#the-browser)) |
| `view_image` | An image in the workspace ([images](images.md)) |
| `ask_user_question`, `todo`, `enter_plan_mode`, `exit_plan_mode` | Asking you, the task list, and plans |
| `list_or_search_skills`, `activate_skill`, `run_skill_script` | [Skills](skills.md) |
| `list_agents`, `invoke_agent` | Delegation |
| `universal_constructor` | Builds tools, saved in `~/.blitz/uc_tools` and reloaded on start |
| MCP tools | From [MCP servers](extending.md#mcp-servers) |

`/tools` lists the active agent's tools and whether each is allowed in plan mode.
