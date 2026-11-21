---
title: "066 · Reasoning"
weight: 66
---

*A turn as a graph: its model calls, the reasoning each shared, the tools it chose and the sub-agents it asked, drawn as the turn runs and kept for every past turn. A view in the desktop app, opened beside the chat's history.*

| | |
|---|---|
| Status | **RSN-01 to RSN-15 built** (2026-11-21, the owner's ask: "a graphical view to show the model reasoning … a complete pane with an icon next to the chat history"; choices: calls with their reasoning and tools, a node graph first, past chats too). |
| Source | `pkg/api/reasoning.go` (the graph's types); `pkg/engine/reasoning.go` (built from a session's event log and its sub-agents' log); `pkg/engine/session/adkstore.go` (`ReadSubagentEvents`); `proto/blitz/v1/session.proto` (`SessionService.GetReasoning`); `pkg/service/server/session.go`, `convert.go`; `apps/desktop/web/src/reasoning/` (the layout, the cards, the drawer, the detail panel, the chat link); `apps/desktop/web/src/dev/fakeReasoning.ts` |
| Tests | `pkg/engine`: `TestWorkspaceReasoning`, `TestBuildReasoning`, `TestReasoningMasksSecrets`, `TestReasoningCut`, `TestThoughtSteps`; `pkg/service/server`: `TestSessionToolsAndSuggestions`; the page: `reasoningGraph.test.ts` (no two cards overlap, whatever is shown or open), `reasoningParts.test.ts`, `css.lint.test.ts` (the top menu) |
| Depends on | [spec_056_conversation_adapters](spec_056_conversation_adapters.md) (how each provider's reasoning arrives, CA-29), [spec_032_background_agents](spec_032_background_agents.md) (sub-agents), [spec_052_processes](spec_052_processes.md) |

## 1. Purpose

A model's answer is the end of a path: what it read, what it thought, what it tried and what failed. The chat shows that path as a stream; the Reasoning view shows it as a shape, so a long turn can be read at a glance and any step in full.

## 2. Requirements

### The graph (service)

- **RSN-01** `SessionService.GetReasoning` returns a saved session's **turns** (each prompt's start, its calls, the steps of reasoning shared, its usage) and the **graph** of one turn (the last when none is asked, or past the end). It's built from the session's event log as saved, so every past chat has one and nothing new is stored: a **turn** node (the prompt), a **call** node per model answer (provider, model, tokens, time since the step before it, the cost at today's prices), under each call its **thought** steps, the **tools** it called (arguments, result, time, ok or failed — a result with an `error` — or running) and its **text**.
- **RSN-02** A call's reasoning is split into **steps** where the model gives them: a line in bold (`**Like this**`, Gemini's summaries) starts one; otherwise its paragraphs are its steps. More than 8 are grouped into 8, so the graph stays readable.
- **RSN-03** A call that reasoned (its usage counts reasoning tokens) without sharing it is marked **hidden**: the provider keeps it (OpenAI's reasoning models).
- **RSN-04** A foreground **sub-agent** (`invoke_agent`) is a node whose children are its own calls, read from the session's sub-agent log: its events by its name, between the call and its result; nested up to 3 deep. A background one is a node without children.
- **RSN-05** The reasoning, the words and the prompt are masked as the conversation's are (CA-29a); arguments and results are cut as a reopened chat's are (4 KiB per string), each node's text at 16 KiB, and a turn at 1,500 nodes (**cut**, said).

### The view (desktop)

- **RSN-10** An icon in the chat's bar, beside **History**, opens **Reasoning**: a drawer view under the top menu, to the status bar. The turn reads top to bottom as the chat does: its calls on a **spine**, each call's steps of reasoning to its left, its tools, sub-agents and words to its right; a sub-agent opened draws its own spine further right. Cards have one shape, each kind its colour (theme tokens, light, dark and glass alike); a failed tool is red, its link dashed.
- **RSN-11** The header: the **turn picker** (previous, next, and every turn by its prompt), toggles for the **reasoning**, the **tools** and the **words**, **open all** and **fold all** (a call's steps start open, a sub-agent's calls folded), and **Follow**. A summary line under it: the prompt, its calls, steps and estimated cost.
- **RSN-12** While a turn runs, the graph is asked for again as the conversation changes (at most every 700 ms) and grows as calls are saved; a **live** card after the last call shows the reasoning streaming now, and **Follow** keeps the newest card in view until the person moves the graph.
- **RSN-13** Selecting a card shows it **in full** beside the graph: its facts (model, provider, agent, status, tokens, cost, time), its text as Markdown, a tool's arguments and result as JSON, **Copy**, and **Show in chat**, which closes the view and scrolls the chat to that turn's prompt, marked a moment. The panel folds to a rail and resizes; in a narrow drawer it goes under the graph. Arrow keys (and `j`/`k`) move between cards in reading order, Enter opens or folds one.
- **RSN-14** With no chat or no turn yet, the view says what will appear; the fake service (`?fake`) draws two turns: one that reasons, fails a test, fixes it and asks a reviewer; one whose provider keeps its reasoning.
- **RSN-15** **The top menu is always usable.** Every view opens under it; the diagram and picture viewers start under it too, and no full-window layer of the page's covers it (a stylesheet test).

## 3. Not yet

- An outline view of the same graph, and `/reasoning` in the CLI.
- Workflow runs' steps in the graph (they have their receipts, spec_043).
- Costs from the session's ledger rather than today's prices.
