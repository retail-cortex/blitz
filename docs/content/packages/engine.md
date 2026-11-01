---
title: "engine"
weight: 4
---

Blitz without a user interface. Source: [`pkg/engine`](https://github.com/retail-cortex/blitz/tree/main/pkg/engine).

`pkg/engine` opens a workspace and exposes what a front end needs as an `api.Backend`. Only the service and the CLI's `--local` mode link it; its Bazel visibility and `//tools:check_deps` keep everything else out. [The engine](../architecture/engine.md) explains how a turn runs.

## API

| Name | |
|---|---|
| `Open(ctx, cfg, Options)` | Opens a workspace: a `*Workspace`, which implements `api.Backend` |
| `Workspace` | Turns, sessions, agents and models, settings, changes, extensions, files, jobs |
| `StartObservability`, `SetupLocale`, `SecretRedactor` | Process-wide setup the apps share |
| `ModelErrorSummary` | A model error cut short and masked, since some SDK errors embed the key |

## Subpackages

| Package | |
|---|---|
| `pkg/engine/runtime` | The agent over Google's ADK runner: the agent tree and sub-agents, turns within their limits, steering, side questions, compaction, usage and cost, tracing. It also builds the models (Gemini, Anthropic, OpenAI, Ollama) and falls back between them. Specs: [engine](../about/specs/spec_016_engine.md), [models](../about/specs/spec_015_models.md) |
| `pkg/engine/tools` | Everything the agent can do and the rules it does it under: file tools through the workspace sandbox with checkpoints, shell commands under the command policy and the OS sandbox, approvals and permission rules, web fetch and search, MCP servers, skill scripts (gVisor), hooks, the task list, and questions to the user. Specs: [file tools](../about/specs/spec_006_filetools.md), [shell](../about/specs/spec_007_shell.md), [approvals](../about/specs/spec_005_approvals.md), [web](../about/specs/spec_008_web.md), [MCP](../about/specs/spec_009_mcp.md), [hooks](../about/specs/spec_010_hooks.md) |
| `pkg/engine/session` | Sessions on disk: metadata, the transcript, the ADK's event log, snapshots, and transcript search. Spec: [sessions](../about/specs/spec_017_sessions.md) |
| `pkg/engine/agents` | Agent definitions: the built-in ones (embedded Markdown) and your own. Spec: [agents](../about/specs/spec_014_agents.md) |
| `pkg/engine/skills` | Skill definitions (`SKILL.md`), and the policy that caps what their scripts may do. Spec: [skills](../about/specs/spec_013_skills.md) |
| `pkg/engine/commands` | Custom slash commands: Markdown files whose body is a prompt, plus the bundled ones |
| `pkg/engine/memory` | Project instruction files, their imports, and rules scoped to paths. Spec: [memory](../about/specs/spec_012_memory.md) |
| `pkg/engine/workers` | `WORKER.md`: schedules, permissions, the enabled store, run logs. Spec: [jobs](../about/specs/spec_023_workers.md) |
| `pkg/engine/audit` | The append-only JSONL audit log, secrets masked |
| `pkg/engine/breaker` | A circuit breaker for dependencies that keep failing (MCP servers, models): a cooldown from 15 seconds, doubling to 5 minutes |

## Used by

apps/cli (`--local`), apps/service (its daemon and server), and pkg/service/servicetest.

Spec: [workspace](../about/specs/spec_018_workspace.md).
