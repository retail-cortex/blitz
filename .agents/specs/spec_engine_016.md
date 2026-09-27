# spec_engine_016 — Agent engine: turns, plan mode, steering, side questions, compaction

| | |
|---|---|
| Status | Implemented (reverse-engineered from `53f8c53`) |
| Source | `internal/runtime/engine.go`, `plan.go`, `steer.go`, `aside.go`, `compact.go`, `search.go`, `images.go`, `usage.go` |
| Tests | `internal/runtime/engine_test.go`, `plan_test.go`, `steer_test.go`, `aside_test.go`, `compact_test.go`, `bulkhead_test.go`, `telemetry_test.go`, `runtime_security_test.go`, `mcp_agents_test.go` |
| Related | [spec_models_015](spec_models_015.md), [spec_agents_014](spec_agents_014.md), [spec_workspace_018](spec_workspace_018.md) |

## 1. Purpose

The `Engine` wraps the Google ADK runner (`google.golang.org/adk/v2`). It builds the agent tree, runs one prompt as a turn, enforces per-turn limits and read-only modes, delivers user steering mid-turn, answers side questions without touching history, compacts history, runs sub-agents, and records usage and traces. It is safe for concurrent use.

## 2. Agent tree

- **ENG-01** The active agent is the root `llmagent`; every other registered agent is a sub-agent. If the active agent is unknown the engine falls back to `blitz` (error if absent).
- **ENG-02** Root instruction = the agent prompt interpolated with the configured agency level + (if skills are enabled and any exist) "## Available Agent Skills" listing name and description with a hint to use `activate_skill` + the image instruction + extra instructions (memory, reply language). Sub-agents use their own spec's agency level.
- **ENG-03** The image instruction ("You can see images…", plus a `view_image` hint when the agent has that tool) is added only when image support is on.
- **ENG-04** Each agent gets the registry tools named in its spec and the MCP toolsets offered to it (root: primary=true).
- **ENG-05** Generation config: `temperature` and `max_tokens` from `[blitz]` when > 0; per-model settings override per call ([spec_models_015](spec_models_015.md)).
- **ENG-06** Changing the active agent, model, pins, instructions or agency **rebuilds** the runner; on failure the previous value is restored. Session, artifact and memory services are shared across rebuilds so history survives.
- **ENG-07** The runner always has a compaction config, because it honours compaction events only then: `retain_events` (default 20) and the threshold, or `MaxInt32` when auto-compaction is off.

## 3. Execute (one turn)

- **ENG-10** `Execute(ctx, sessionID, prompt, handler, opts…)`: empty session ID is `default`. Run state in the context carries session ID, max turns, attachments, plan-only flag and mode.
- **ENG-11** The user content is the attachments (images first, as providers recommend) followed by the prompt text.
- **ENG-12** Tool-call parallelism: at most `tools.max_parallel` (default 8) tool calls of one model response run at once. The limit is per batch (sub-agents get their own), avoiding deadlock; every task runs exactly once, queued tasks after cancellation fail fast.
- **ENG-13** Streaming mode is SSE when the workspace was opened with streaming.
- **ENG-14** `WithMaxTurns(n)`: the before-model callback counts model calls in the run; call n+1 fails with `ErrMaxTurns` ("maximum turns reached (n model calls)"), which is not wrapped.
- **ENG-17** `WithAllowedTools(names)` refuses every tool outside the list for the run (sub-agents included): "X isn't among the tools this command allows (…)".
- **ENG-16** Per-run overrides: `WithAgent(name)` makes `name` the root agent and `WithModel(llm)` replaces the configured model, **for that run only**. Either builds a runner for the run (sharing the session, artifact and memory services) without changing the engine's active agent, model or pins. The override model runs the root agent even when it is pinned, and every unpinned agent; pinned sub-agents keep their pins; `invoke_agent` sub-agents in the run follow the same rule. An unknown agent is an error. Usage is priced by the model each agent ran on in that run, and the turn span names the run's agent and model. Used by workers ([spec_workers_023](spec_workers_023.md) WK-45).
- **ENG-15** Other runner errors are wrapped as "agent execution error: …"; a failing handler aborts the drain. Failures not caused by cancellation are logged at error level.

### 3.1 Callbacks
- **ENG-20** Before each tool: audit `tool_call` (name, args); apply the plan/read-only refusal (§4); MCP approval (`ApproveMCP`); `pre_tool` hooks (a block returns `{"error":"blocked by pre_tool hook: <reason>"}` instead of running). Refusals are returned as tool results so the model sees them.
- **ENG-21** After each tool: `post_tool` hooks (asynchronous, see [spec_hooks_010](spec_hooks_010.md)); audit `tool_result` with decision `ok`/`error` (tool error or an `error` field); attach steer messages (§5).
- **ENG-22** After each non-partial model response: note fallback changes (§ fallback in [spec_models_015](spec_models_015.md)) and record usage priced by the model that actually answered (`ModelVersion` if it has a price, else the calling agent's model), including cache-write tokens from `CustomMetadata["cache_creation_input_tokens"]`.

## 4. Plan mode and read-only modes

- **ENG-30** Tools allowed in plan/read-only mode: `read_file`, `list_files`, `glob`, `grep`, `view_image`, `web_fetch`, `web_search`, `list_agents`, `invoke_agent`, `list_or_search_skills`, `activate_skill`, `ask_user_question`, and the workflow tools `todo`, `exit_plan_mode`, `enter_plan_mode`. Everything else, including every MCP tool, is refused. Sub-agents run inside the same run and are equally restricted.
- **ENG-31** Refusal text: plan mode — "plan mode: X is disabled because it could change something. Describe this step in the plan instead."; named mode — "<mode> is read-only: X is disabled because it could change something."
- **ENG-32** `PlanPrompt(goal)` asks for: objective summary, numbered implementation plan naming files/functions, risks and unknowns, verification, and questions only if blocking — presented with `exit_plan_mode` (or as the answer when no one can review it). `CarryOutPrompt(planFile)` is the go-ahead after approval.
- **ENG-34** A turn is also planning while its `tools.PlanGate` says so (the agent called `enter_plan_mode`); plan refusals then apply from the next tool call. The primary agent always gets the workflow tools (`todo`, `exit_plan_mode`, and `enter_plan_mode` when `plan_review = agent-decides`), whatever its tool list; sub-agents don't.
- **ENG-33** Read-only modes are used by `/search` (`WithReadOnly`) and `/btw` (mode `btw`).

## 5. Steering

- **ENG-40** `Steer(session, text)` queues a message. The next tool result **of that session's own turn** (not a sub-agent's session) carries queued messages under `message_from_user` (a string for one, an array for several), so the model reads them before its next step without interrupting anything; the history keeps them where they arrived. A tool error is preserved in the result's `error` field. A trace event `steer` records the count.
- **ENG-41** `TakeSteers(session)` removes and returns messages no tool result carried (the model finished without another tool call); the front end sends them as the next prompt (`Turn.Accepted`) or drops them if the turn was interrupted.
- **ENG-43** Path-scoped rules ride on tool results the same way, under `project_rules` ([spec_memory_012](spec_memory_012.md) MEM-07); when both apply, rules come first and steer messages are added to the same result.
- **ENG-42** Rationale (decision): ADK model callbacks cannot add session events, so tool results are the channel that reaches both the model and the history.

## 6. Side questions (`/btw`)

- **ENG-50** `Aside(session, prompt)` copies the session's events (through JSON, sharing nothing) into a throwaway in-memory session, runs the root agent there in read-only mode `btw`, then discards it. Neither question nor answer reaches the history, the saved event log, the transcript or later prompts.
- **ENG-51** The copy honours existing compaction summaries but never compacts (threshold `MaxInt32`). Tokens count toward the real session's usage. A `btw` span is recorded. A session with no turns yet starts empty.

## 7. Compaction

- **ENG-60** Automatic: the ADK runner compacts once a prompt reaches `context.token_threshold`, retaining `retain_events` raw events.
- **ENG-61** Manual `Compact(session, focus, keepTurns≥1)`: find the start of the keepTurns-th most recent **user turn** (a user-authored event that is not a function response and not a compaction), so a tool call is never separated from its result. Nothing before it, or everything already compacted, is `ErrNothingToCompact`.
- **ENG-62** The window is summarised by the current model with a fixed prompt (goals and constraints, decisions and why, files read/changed, commands and outcomes, unresolved errors, next steps) plus "Pay particular attention to: <focus>", timeout 3 min. Earlier summaries inside the window are fed as "[Summary of earlier conversation]" events, and the events they cover are omitted, so a second compaction never loses the first summary's content.
- **ENG-63** Only prose parts of the summary are kept; an empty summary is an error and history is unchanged (recording it would delete the covered turns).
- **ENG-64** The compaction event spans the window's true timestamp range and lists as excluded any in-range event outside the window not already covered, so nothing is dropped unsummarised. If the clock is not after the window's end, it refuses to record. Summarisation tokens count toward session usage. A `compact` span is recorded.

## 8. Sub-agents

- **ENG-70** `InvokeSubagent(agent, prompt)` (behind the `invoke_agent` tool) builds the agent fresh and runs it in an isolated in-memory session `subagent-<name>-<seq>`; returns the concatenated final non-thought text authored by that agent. Nesting is capped at depth 3 (`ErrSubagentDepth`). The calling run's usage accounting, per-model settings and turn limit context still apply.

## 9. Usage and cost

- **ENG-80** Per call: input = prompt + tool-use prompt tokens; cached; cache-write (clamped to uncached input); output = candidates + thoughts; `LastPrompt` = input (the current context size).
- **ENG-81** Cost = (uncached × input + cached × cached_input + cache_write × write_rate + output × output_rate) / 1e6, where write_rate defaults to the input rate. Price lookup: exact model name, else the longest configured prefix (`gemini-3.8-flash-001` → `gemini-3.8-flash`). An unpriced call marks the session `Priced=false`.
- **ENG-82** Usage is kept per session in memory for the life of the process; it is not persisted.

## 10. Tracing

- **ENG-90** Each turn is its own trace root (`turn` span) with attributes agent, model, prompt chars, attachments, max_turns, plan_only, `gen_ai.conversation.id`, `turn.index`, and at the end model_calls, tokens.input/output and cost_usd (when priced).
- **ENG-91** Turns of a session are chained: the previous turn's traceparent and index are loaded from the turn store (session metadata `last_turn`) and linked (`link.type=previous_turn`); after the turn the new traceparent is saved. With telemetry off nothing is written.

## 11. Prompts built for search

- **ENG-95** `WebSearchPrompt` lists the approved links (title, URL, snippet), tells the agent to read them with `web_fetch`, skip failures, cite URLs used, say what is missing, and that other pages need approval; appends the engine's own summary marked unverified.
- **ENG-96** `SessionSearchPrompt` lists matching passages as `[message N, role, YYYY-MM-DD HH:MM]` with a note that earlier turns may be compacted; with no matches, asks the agent to answer from memory, say if it never came up, and not use tools.
