---
title: "045 · Composer"
weight: 45
---

*A guided conversation that makes a workspace's component: it asks what it needs to know, writes the component in its own file format, checks it with the engine's own rules until it loads, and hands it to the editor as a draft* (`spec_composer_045`)

| | |
|---|---|
| Status | **Done for workflows** in v0.7.1 (the owner's request, 2026-10-18): CMP-01–CMP-06. Next, for v0.7.2: the other kinds' recipes (CMP-07) and the Overview's Improve through the composer (CMP-08). |
| Source | `pkg/engine/compose/` (sessions, recipes), `pkg/engine/compose.go` (the workspace's context and model), `apps/service/internal/server/compose.go`, `proto/blitz/v1/workspace.proto`, `apps/desktop/web/src/studio/ComposeDialog.tsx`, `apps/desktop/web/src/studio/compose.ts` |
| Tests | `pkg/engine/compose/compose_test.go`, `pkg/engine/compose_test.go`, `apps/service/internal/server/compose_test.go`, `apps/desktop/web/src/studio/compose.test.ts`; manual checks in MANUAL_VERIFICATION §54 |
| Depends on | [spec_workflows_043](spec_workflows_043.md) (the workflow file and its checks), [spec_studio_040](spec_studio_040.md) (the editors), [spec_health_044](spec_health_044.md) (a later caller: Improve) |

## 1. Purpose

Studio's editors build components field by field, and the draft assist rewrites one field at a time. Many components are easier to describe than to build: "every weekday, read the support inbox summary, draft replies for the urgent ones and save them for me to review" is a workflow, but turning it into steps, links, error paths and a mode takes knowing the canvas.

The composer is one service for every kind: a short conversation that asks only what it needs, then writes the component. It's driven by parameters, not a service per kind: each kind is a **recipe** (its file format, what the model may refer to, and the engine's own check of the result), so adding a kind is adding a recipe.

## 2. Requirements

- **CMP-01** **Sessions** (`compose.Session`): a kind, the workspace, the conversation so far (the user's request and answers, the composer's questions), and when it has one, the **draft** (the component in its file format, the name it suggests, a one-line summary) with the **problems** the engine's check finds in it, and its cost. Sessions live in the service's memory for an hour after their last turn; none is written to disk, and nothing is saved until the user saves the draft in the editor.
- **CMP-02** **A turn** (`Workspace.Compose`): the model gets the recipe's brief (what the kind is, its file format), the workspace's context (CMP-04), the current component when there is one (an edit), and the conversation, and answers either one **question** as JSON (with up to six suggested answers when there are natural choices) or a **draft**: a JSON line with its name and summary, then the file in its own fenced block (a file inside a JSON string breaks on its quotes). An answer that can't be read is asked for again once, with why. The composer asks at most what it must (a first turn may go straight to a draft when the request says enough), one question at a time, and drafts by the fifth answer at the latest.
- **CMP-03** **Checked before it's shown**: a draft is parsed and checked with the kind's own rules (a workflow's `flows.Parse` and `flows.Check`); its problems go back to the model with the draft, to fix, up to twice; what's still wrong is shown with the draft, which the editor can fix by hand.
- **CMP-04** **Context**: the recipe names what the model may refer to, from the workspace as it is: for a workflow, the step types and their settings, the workspace's agents, skills (and their scripts), rules, MCP servers' tools and the other workflows (for call steps), each by name with its description. It never sees secrets' values, only their names (for `{secrets:name}`).
- **CMP-05** **API**: `StartCompose` (workspace, kind, the request, and for an edit the current component as its file) and `AnswerCompose` (session, answer) each return the session as it is after the composer's turn; `DropCompose` forgets one. Plain calls: nothing is held open (the window's connections are few, spec_desktop_024).
- **CMP-06** **Workflows in Studio**: the new-workflow dialog has **Compose with AI** beside the templates: the request, then the conversation (questions as text with their suggested answers as buttons, a field to answer in your own words), then the draft's summary, its steps as a list and its problems, with **Open on the canvas**, which opens it as a new, unsaved workflow (its mode and name as drafted) to review, change and save.
- **CMP-07** *(next, v0.7.2)* Recipes for skills (SKILL.md, with scripts), agents (frontmatter and instructions, least tools), evals (cases from an agent's description and its failed runs), rules and guardrails, and jobs (schedule and least permissions); each editor's **Compose** opens the same dialog on its kind, an edit passing the current file.
- **CMP-08** *(next, v0.7.2)* The Overview's **Improve** asks the composer for the item with the fix as the request, and opens the result as a draft in the item's editor.

## 3. Known gaps

Workflows only, in v0.7.1; the other kinds follow in v0.7.2. A session doesn't survive the service restarting. A failed turn (the model unreachable, an answer unreadable twice) is `FailedPrecondition` (`COMPOSE_FAILED`), not `Unavailable`, which the page reads as the service gone. A schedule in the request ("every weekday") isn't drafted: schedules are kept apart from the workflow file (WF-20) and set after saving.
