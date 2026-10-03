---
title: "014 · Agents"
weight: 14
---

*Agents, delegation and forged tools* (`spec_agents_014`)

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/engine/agents/{manager,frontmatter,files}.go`, `pkg/engine/agents/builtin/*.md`; `pkg/engine/agentfiles.go`; `pkg/engine/runtime/settings.go`; `pkg/engine/tools/{agent_tools,universal_constructor}.go`; `apps/service/internal/server/agents.go`; `apps/desktop/web/src/AgentEditor.tsx` |
| Tests | `pkg/engine/agents/*_test.go`; `pkg/engine/agentfiles_test.go`; `pkg/engine/tools/uc_and_agent_tools_test.go`; `pkg/engine/runtime/{pin,settings}_test.go`; `apps/service/internal/server/handlers_test.go` |
| Depends on | [spec_config_002](spec_config_002.md), [spec_shell_007](spec_shell_007.md), [spec_approvals_005](spec_approvals_005.md) |
| Used by | [spec_engine_016](spec_engine_016.md) |

## 1. Purpose

An agent is a persona: a system prompt, a tool list, an agency level and optionally its own model, defined in Markdown with YAML frontmatter. One agent is active (the root); the others are sub-agents it can delegate to. Helios can forge small tools at runtime.

## 2. Agent definitions

- **AG-01** File format: `---` YAML frontmatter `---` then the system prompt. Frontmatter: `name` (required), `display_name`, `description`, `tools` (registry tool names, aliases allowed), `default_model` (`provider/model`), `agency_level`, and the model settings of AG-21.
- **AG-02** Prompts may contain `{agency_instructions}` or `{{agency_instructions}}`, replaced by the rules for the level (empty = high):
  - **low** — one step at a time; after each meaningful unit, stop, summarise and ask; no consequential or irreversible action without explicit approval.
  - **medium** — do routine, clearly requested work; check in at major milestones or before consequential changes.
  - **high** (default) — complete the task autonomously; ask only when blocked by missing requirements or irreversible actions needing approval.
  - **extreme** — maximal persistence; don't stop while a background process gates completion; check progress.
- **AG-03** The root agent uses the configured `blitz.agency_level` (changeable with `/set agency=`); sub-agents use their own frontmatter level.

## 3. Registry

- **AG-10** Built-in agents are embedded. External agents are loaded (recursively, `*.md`) from `~/.blitz/agents` (`config.UserAgentsDir`), then the workspace's `.agents/agents` (`config.ProjectAgentsDir`, as prompt text, [spec_project_config_031](spec_project_config_031.md); a later folder wins a name), then enabled plugins' `agents/`. *2026-10-01:* the workspace folder moved from `./agents` to `.agents/agents`, beside `.agents/workers` and `.agents/skills`; `./agents` is no longer read. External specs may add agents but **may not replace a built-in** (that would let a directory swap a trusted persona's prompt and tools); rejected or unparseable specs are reported, valid ones still load.
- **AG-11** `List` is sorted by name.
- **AG-13** *2026-10-01.* Agents reload without reopening the workspace. The registry keeps the folders it loaded and a stamp of their `*.md` files (path, size, modification time); `Registry.Refresh` reloads them when the stamp changes, keeping the built-ins. `Workspace.RefreshAgents` runs it before each turn and worker run and when agents or workers are listed or an agent is switched: an added file's agent is offered, an edited one's prompt, tools, settings and `default_model` apply from the next turn (models pinned or unpinned to match), and a removed one is gone (the active agent falls back to `blitz`). Files that don't load are warnings.
- **AG-14** *2026-10-01.* Agent files are written by the editors through `WorkspaceService.ListAgentFiles`, `SaveAgentFile` and `DeleteAgentFile`, per scope: `AGENT_SCOPE_WORKSPACE` (the workspace's `.agents/agents`) or `AGENT_SCOPE_USER` (`~/.blitz/agents`, needing no workspace). Listing returns every file with what it defines or why it doesn't load (unparseable, a built-in's name). Saving checks the name (`^[a-z0-9][a-z0-9_-]{0,63}$`, not a built-in's, not another file's in the folder), a description, the agency level and the model settings, and returns problems with nothing written; it writes `<name>.md` atomically (an edit keeps the agent's file whatever it's called, a rename moves it to the new name's file), and the workspace named reloads at once. Writing a file drops YAML comments and keys Blitz doesn't know.

| Built-in | Role | Agency |
|---|---|---|
| `blitz` | Primary coding agent: reads, changes, runs and verifies code (default) | high |
| `helios` | Universal Constructor: forges and runs tools | high |
| `qa` | Tests, TDD, regression suites, edge cases | high |
| `web-retriever` | Documentation and web research | high |
| `planning-agent` | Requirement decomposition, roadmaps, verification gates (read-only tools + `invoke_agent`) | medium |
| `agent-creator` | Creates and validates agent specs and skills | high |
| `model-judge` | Compares model responses | medium |
| `project-setup` | Sets up a workspace's agent harness: interviews, `.agents/AGENTS.md`, the instruction files importing it, skills and agents (`/setup`, [spec_memory_012](spec_memory_012.md) MEM-22) | medium |

- **AG-12** Built-in prompts are persona-free and terse (no mascot, plain status marks).

## 4. Models per agent

- **AG-21** *2026-10-01.* An agent's frontmatter may set `temperature` (0–2), `top_p` (above 0, at most 1), `max_tokens` (at least 1), `effort` (minimal, low, medium, high, max; `xhigh` reads as max) and `thinking_budget` (0 or more). They apply to that agent's model calls only, as the main agent or a sub-agent, over the model's own `[model_settings]` and the session's effort; settings it leaves unset stay the model's. Settings the provider can't take are dropped as for `[model_settings]` (spec_engine_016). The model is wrapped when the agent is built (`withAgentSettings`), and the settings travel in the call's context to the model's settings wrapper.
- **AG-20** An agent runs on (highest first): an `[agent_models]` pin, its own `default_model`, the configured model. Pinned agents keep the fallback chain behaviour of their own model and are priced by it. `/pin_model <agent> <model>` and `/unpin <agent>` change pins live and in the config file ([spec_workspace_018](spec_workspace_018.md) WS-41). `doctor` checks each pin.

## 5. Delegation tools

- **AG-30** `list_agents(filter?)` → name, display name, description (substring filter).
- **AG-31** `invoke_agent(agent_name, prompt, background?)` → the sub-agent's final text (`response`) or `error`; with `background: true` (or the agent's own `background: true` when the call doesn't say) it returns at once with a `task_id` and the sub-agent runs as a task ([spec_background_agents_032](spec_background_agents_032.md)). An agent's frontmatter may also set `permission_mode` (its own mode through `invoke_agent`; a looser one than the workspace's only for built-in agents, the user's own and a trusted project's; bypass only in the OS sandbox) and `max_turns` (its own model-call budget). The sub-agent runs in a fresh isolated session, gets the MCP servers offered to it, and nesting is capped at depth 3 ([spec_engine_016](spec_engine_016.md) ENG-70). In plan/read-only runs the sub-agent is equally restricted.

## 6. Universal Constructor (`universal_constructor`)

Args: `action` (`create|run|list|delete`), `tool_name`, `language` (`bash`/`sh`, `python`/`py`, `go`; default bash), `code`, `description`, `args` (whitespace-separated).

- **AG-40** Tool names match `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$` (no separators or traversal). Tools are stored in `tools.uc_tools_dir` (default `~/.blitz/uc_tools`, created lazily) as the script plus a JSON manifest (`name, language, description, created`); the script path is never stored — it is recomputed from the validated name and language.
- **AG-41** `create` needs write approval (kind `write_file`); `delete` needs delete approval (kind `delete_file`); `list` returns `name (language): description`.
- **AG-42** `run`: the command policy sees the synthetic command `universal_constructor <tool> <args…>` (shell-quoted, so allow-lists must opt in with e.g. `universal_constructor *`); deny blocks; otherwise approval unless auto-approved (kind `run_command`, key `uc-run:<tool>\x00<args…>`, "this forged tool with these arguments"). Runs as `bash <script>`, `python3 <script>` or `go run <script>` through the exec environment (OS sandbox, scrubbed env, process guard), 60 s timeout, 100 KiB output.
- **AG-43** Manifests are untrusted on reload: name and language are re-validated and the script must be a regular file (not a symlink) at the derived path. Forged tools persist across sessions.
