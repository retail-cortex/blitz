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

Your own agents are Markdown files with YAML frontmatter in `~/.blitz/agents` (and the project's `./agents`): a name, a description, the system prompt, the tools it may use, and optionally its own model. Built-in agents can't be overridden.

## Tools

| Tools | |
|---|---|
| `read_file`, `list_files`, `glob`, `grep` | Reading and searching the workspace |
| `create_file`, `replace_in_file` (`edit`), `delete_snippet`, `apply_patch`, `delete_file` | Changing it; `apply_patch` takes a unified diff or `*** Begin Patch`, atomically, across files |
| `run_shell_command`, `manage_background_process` | Commands, in the OS sandbox |
| `web_fetch`, `web_search` | The web ([search](search.md)) |
| `view_image` | An image in the workspace ([images](images.md)) |
| `ask_user_question`, `todo`, `enter_plan_mode`, `exit_plan_mode` | Asking you, the task list, and plans |
| `list_or_search_skills`, `activate_skill`, `run_skill_script` | [Skills](skills.md) |
| `list_agents`, `invoke_agent` | Delegation |
| `universal_constructor` | Builds tools, saved in `~/.blitz/uc_tools` and reloaded on start |
| MCP tools | From [MCP servers](extending.md#mcp-servers) |

`/tools` lists the active agent's tools and whether each is allowed in plan mode.
