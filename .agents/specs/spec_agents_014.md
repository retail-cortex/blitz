# spec_agents_014 — Agents, delegation and forged tools

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `internal/agents/{manager,frontmatter}.go`, `internal/agents/builtin/*.md`; `internal/tools/{agent_tools,universal_constructor}.go` |
| Tests | `internal/agents/*_test.go`; `internal/tools/uc_and_agent_tools_test.go`; `internal/runtime/pin_test.go` |
| Depends on | [spec_config_002](spec_config_002.md), [spec_shell_007](spec_shell_007.md), [spec_approvals_005](spec_approvals_005.md) |
| Used by | [spec_engine_016](spec_engine_016.md) |

## 1. Purpose

An agent is a persona: a system prompt, a tool list, an agency level and optionally its own model, defined in Markdown with YAML frontmatter. One agent is active (the root); the others are sub-agents it can delegate to. Helios can forge small tools at runtime.

## 2. Agent definitions

- **AG-01** File format: `---` YAML frontmatter `---` then the system prompt. Frontmatter: `name` (required), `display_name`, `description`, `tools` (registry tool names, aliases allowed), `default_model` (`provider/model`), `agency_level`.
- **AG-02** Prompts may contain `{agency_instructions}` or `{{agency_instructions}}`, replaced by the rules for the level (empty = high):
  - **low** — one step at a time; after each meaningful unit, stop, summarise and ask; no consequential or irreversible action without explicit approval.
  - **medium** — do routine, clearly requested work; check in at major milestones or before consequential changes.
  - **high** (default) — complete the task autonomously; ask only when blocked by missing requirements or irreversible actions needing approval.
  - **extreme** — maximal persistence; don't stop while a background process gates completion; check progress.
- **AG-03** The root agent uses the configured `blitz.agency_level` (changeable with `/set agency=`); sub-agents use their own frontmatter level.

## 3. Registry

- **AG-10** Built-in agents are embedded. External agents are loaded (recursively, `*.md`) from `~/.blitz/agents`, and `./agents` only with `trust_workspace`. External specs may add agents but **may not replace a built-in** (that would let a directory swap a trusted persona's prompt and tools); rejected or unparseable specs are reported, valid ones still load.
- **AG-11** `List` is sorted by name.

| Built-in | Role | Agency |
|---|---|---|
| `blitz` | Primary coding agent: reads, changes, runs and verifies code (default) | high |
| `helios` | Universal Constructor: forges and runs tools | high |
| `qa` | Tests, TDD, regression suites, edge cases | high |
| `web-retriever` | Documentation and web research | high |
| `planning-agent` | Requirement decomposition, roadmaps, verification gates (read-only tools + `invoke_agent`) | medium |
| `agent-creator` | Creates and validates agent specs and skills | high |
| `model-judge` | Compares model responses | medium |

- **AG-12** Built-in prompts are persona-free and terse (no mascot, plain status marks).

## 4. Models per agent

- **AG-20** An agent runs on (highest first): an `[agent_models]` pin, its own `default_model`, the configured model. Pinned agents keep the fallback chain behaviour of their own model and are priced by it. `/pin_model <agent> <model>` and `/unpin <agent>` change pins live and in the config file ([spec_workspace_018](spec_workspace_018.md) WS-41). `doctor` checks each pin.

## 5. Delegation tools

- **AG-30** `list_agents(filter?)` → name, display name, description (substring filter).
- **AG-31** `invoke_agent(agent_name, prompt)` → the sub-agent's final text (`response`) or `error`. The sub-agent runs in a fresh isolated session, gets the MCP servers offered to it, and nesting is capped at depth 3 ([spec_engine_016](spec_engine_016.md) ENG-70). In plan/read-only runs the sub-agent is equally restricted.

## 6. Universal Constructor (`universal_constructor`)

Args: `action` (`create|run|list|delete`), `tool_name`, `language` (`bash`/`sh`, `python`/`py`, `go`; default bash), `code`, `description`, `args` (whitespace-separated).

- **AG-40** Tool names match `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$` (no separators or traversal). Tools are stored in `tools.uc_tools_dir` (default `~/.blitz/uc_tools`, created lazily) as the script plus a JSON manifest (`name, language, description, created`); the script path is never stored — it is recomputed from the validated name and language.
- **AG-41** `create` needs write approval (kind `write_file`); `delete` needs delete approval (kind `delete_file`); `list` returns `name (language): description`.
- **AG-42** `run`: the command policy sees the synthetic command `universal_constructor <tool> <args…>` (shell-quoted, so allow-lists must opt in with e.g. `universal_constructor *`); deny blocks; otherwise approval unless auto-approved (kind `run_command`, key `uc-run:<tool>\x00<args…>`, "this forged tool with these arguments"). Runs as `bash <script>`, `python3 <script>` or `go run <script>` through the exec environment (OS sandbox, scrubbed env, process guard), 60 s timeout, 100 KiB output.
- **AG-43** Manifests are untrusted on reload: name and language are re-validated and the script must be a regular file (not a symlink) at the derived path. Forged tools persist across sessions.
