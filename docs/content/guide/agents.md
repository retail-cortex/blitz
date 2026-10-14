---
title: Agents and tools
weight: 40
---

An **agent** is a system prompt, the tools it may use, and optionally its own model and settings. Blitz comes with a general coding agent and a few specialists; you can add your own for yourself or for a project, and the main agent hands work to the others when it helps.

Spec: [agents](../about/specs/spec_agents_014.md).

## Using agents

- **Switch:** `/agent <name>` in a session, or the agent and model button in the desktop app's status bar (**Agent for this conversation**). `/agent` alone shows the current one.
- **List:** `/agents` lists them with their descriptions and pinned models.
- **Start with one:** `blitz --agent qa`, `BLITZ_AGENT`, or `blitz.default_agent` in your settings (the default is `blitz`).
- **Delegate:** you don't have to switch. The main agent passes a piece of work to another agent when its description fits ("ask `qa` to write the tests"), and gets its result back.

### A model per agent

An agent runs on the session's model unless something more specific says otherwise, in this order: a **pin** for that agent, the agent's own `default_model`, then the configured model.

- `/pin_model qa anthropic/claude-haiku-4-5` pins `qa` to a cheaper model in this workspace; `/pin_model` alone lists the pins, and `/unpin qa` removes one.
- In the desktop app, pins are in the workspace's **Run settings**.
- `/model` changes every agent that isn't pinned.

### How much it does on its own

The **agency level** says how much the agent does before checking in with you:

| Level | The agent |
|---|---|
| `low` | Works a step at a time, summarizes, and asks before continuing; never takes a consequential action without approval |
| `medium` | Does routine, clearly requested work, and checks in at milestones and before consequential changes |
| `high` (default) | Completes the task, asking only when blocked or before something irreversible |
| `extreme` | Completes it with maximal persistence, waiting out background work rather than stopping |

Set it with `--agency`, `BLITZ_AGENCY`, `/set agency=medium`, `blitz.agency_level`, or the desktop app's **Run settings**. Agency is separate from approvals: what may run without asking is the [permission mode and rules](safety.md). `/effort` (or `--effort`) sets how hard the model thinks.

## Built-in agents

| Agent | For |
|---|---|
| `blitz` | General coding: reads, changes, runs and verifies code; delegates to the others |
| `qa` | Tests: test-driven development, regression suites, edge cases |
| `web-retriever` | Documentation and web research, extracting structured data |
| `planning-agent` | Breaking a large task into steps, roadmaps and checks; can delegate |
| `helios` | Building tools and scripts on demand (`universal_constructor`) |
| `agent-creator` | Writing agent definitions and skills |
| `model-judge` | Comparing models' answers, quality and speed |
| `project-spec` | Writing the project's specification (`/spec`) |
| `project-setup` | Setting up the workspace's instructions, skills and agents (`/setup`) |
| `harness-update` | Revisiting skills or agents against the specification (`/skills update`, `/agents update`) |

Their names are reserved: a file of your own can't replace one.

## Your own agents

An agent is a Markdown file: front matter, then the system prompt.

| Where | Who gets it |
|---|---|
| `~/.blitz/agents/*.md` | You, in every workspace (desktop: **Settings › Agents**) |
| `.agents/agents/*.md` in the project | Everyone who works in it (desktop: the **Agents** menu) |
| A plugin's `agents/` folder | Whoever enables the plugin |

In the desktop app both are with the advanced settings (**Settings › Appearance › Show advanced settings**): **Add agent**, fill in the name, description and instructions, and open **Advanced** for the rest. `/setup` and `/agents update` also write project agents for you, and the `agent-creator` agent writes one from a description.

```markdown
---
name: reviewer
display_name: Reviewer
description: Reviews a change against the specs and reports problems; changes nothing
tools: [read_file, list_files, glob, grep, search_workspace, lsp]
default_model: anthropic/claude-sonnet-5
permission_mode: plan
agency_level: medium
max_turns: 30
temperature: 0.2
---
You review code for this project. Check each change against .agents/specs/
and report problems with file and line. {agency_instructions}
```

| Key | Meaning |
|---|---|
| `name` | Required. Lowercase, digits, `-` and `_`. Not a built-in's name |
| `display_name` | Shown in lists and menus |
| `description` | What it's for: the main agent reads this to decide when to delegate, so make it specific |
| `tools` | The tools it may use (below). An unknown name is ignored; an agent with none can only reply |
| `default_model` | `provider/model` it runs on, unless pinned |
| `agency_level` | `low`, `medium`, `high`, `extreme`, when it runs for another agent (as the active agent it follows the session's level) |
| `permission_mode` | `default`, `accept-edits`, `auto`, `plan`, `dont-ask` or `bypass`, when another agent invokes it; `plan` makes a reviewer that can't change anything |
| `max_turns` | Its own budget of model calls when invoked |
| `background` | `true`: run in the background when invoked, unless the call says otherwise |
| `isolation` | `worktree`: run in its own git worktree (needs `background`) |
| `temperature`, `top_p`, `max_tokens`, `effort`, `thinking_budget` | Model settings for this agent only, over the model's and the session's |

`{agency_instructions}` in the prompt is replaced by the agency level's instructions (the table above). Every agent is also told how Blitz shows images, and to draw diagrams as ```` ```mermaid ```` blocks.

- **Changes apply at once:** adding, editing or removing a file takes effect from the next prompt, without a restart. If the active agent's file is removed, the conversation falls back to `blitz`.
- **Two files, one name:** a project's agent wins over yours, and a plugin's over both.
- **Saving from the desktop app** rewrites the file: comments and keys Blitz doesn't know are dropped.
- **Project agents and trust:** a project's agents are offered as soon as you open it, as their prompt is text; until you trust the project's settings, one may make the permission mode stricter but never looser.

## Delegation and background tasks

The main agent hands work to another with `invoke_agent`: the other agent starts in a fresh conversation (it doesn't see yours), does the work, and returns its final answer. Agents can delegate up to three levels deep. `blitz` and `planning-agent` delegate; the others don't.

A delegated agent can also run **in the background**: `invoke_agent` with `background: true` (or an agent whose file says so) starts it as a task (`task-1`, `task-2`, …) and returns at once, so the main agent keeps working with you. When the task ends, the main agent hears about it with its next step, or before your next message, and can read the whole result with `task_output`; `list_tasks` and `stop_task` do the rest.

- A task runs what your permission mode and rules allow. Anything it would ask about waits for you: the terminal asks at its prompt, labelled ("qa · task-3 asks"), and the desktop app shows the request beside the conversation (**Ctrl+J** jumps to the next).
- `/tasks` lists them (`/tasks show <id>`, `/tasks stop <id>`); the desktop app shows a card under the call, with **Show** and **Stop**. Exiting offers to stop or wait for them.
- Limits: `tools.max_background_agents` (4 at once), `tools.background_agent_timeout` (30 minutes), `tools.background_agent_max_turns` (50 model calls) and `tools.background_agent_max_cost_usd` (none). Their cost counts in the session's.
- `isolation: worktree` gives a task its own git worktree and branch, so its changes stay out of your checkout until you merge them; `blitz worktrees list` shows them. You can start a whole session in one too: `blitz --worktree[=name]`.
- A background task can't start another, and [workers](workers.md) can't start them at all.

Not to be confused with **background runs**: `blitz --bg "<prompt>"` runs a whole prompt in the service, and `blitz agents` lists those runs ([the service](../products/service.md#background-runs)), not agent definitions.

## Tools

These are the names to list in an agent's `tools`. Some exist only when their feature is on.

| Tools | |
|---|---|
| `read_file`, `list_files`, `glob`, `grep` | Reading and searching the workspace |
| `search_workspace` | Ranked search over its files, PDFs, notebooks, past chats and notes ([search](search.md)) |
| `create_file`, `replace_in_file` (`edit`), `delete_snippet`, `apply_patch`, `delete_file`, `notebook_edit` | Changing it; `apply_patch` takes a unified diff or `*** Begin Patch`, atomically, across files; `notebook_edit` a Jupyter cell |
| `lsp` | Code intelligence from the language's server: definitions, references, hover, symbols, diagnostics (when one is set up) |
| `run_shell_command`, `manage_background_process` | Commands, in the OS sandbox |
| `web_fetch`, `web_search`, `browser` | The web, a search provider, a real browser ([search](search.md)) |
| `view_image`, `view_document`, `view_media` | An image, a PDF, or a recording or video, as far as the model takes them ([attachments](images.md)) |
| `export_pdf`, `export_docx`, `generate_audio` | A Markdown file as a PDF or a Word document; text read aloud, with a speech model set ([attachments](images.md)) |
| `ask_user_question` | Asking you, with options |
| `remember` | Notes the agent keeps for later sessions ([extending](extending.md#notes-the-agent-keeps)) |
| `list_or_search_skills`, `activate_skill`, `run_skill_script` | [Skills](skills.md) |
| `list_agents`, `invoke_agent`, `list_tasks`, `task_output`, `stop_task` | Delegation and background tasks |
| `list_mcp_resources`, `read_mcp_resource` | An MCP server's resources |
| `universal_constructor` | Builds tools, saved in `~/.blitz/uc_tools` and reloaded on start |

The main agent always has `todo` (its task list) and `exit_plan_mode`, whatever its list says. **MCP tools** aren't listed here: a server's `agents` setting says which agents get its tools ([MCP servers](mcp.md)). `/tools` lists the active agent's tools and which are allowed in plan mode.
