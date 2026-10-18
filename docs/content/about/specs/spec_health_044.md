---
title: "044 · Workspace health"
weight: 44
---

*A workspace's dashboard: what it holds, how its runs go, and how good each agent, skill, rule, workflow and worker is, judged on the evidence, on request and on a schedule* (`spec_health_044`)

| | |
|---|---|
| Status | **Planned** for v0.7.1 (the owner's request, 2026-10-18). Phase A (facts and the dashboard) first, then phase B (the judge, its schedule and Studio's grades). |
| Source | (planned) `pkg/engine/health/` (facts, rubrics, verdicts, history), `pkg/engine/healthjudge.go`, `apps/service/internal/server/health.go`, `proto/blitz/v1/workspace.proto`, `apps/desktop/web/src/studio/OverviewPane.tsx`, `apps/desktop/web/src/studio/health.ts`, `apps/desktop/web/src/ui/charts.tsx` |
| Tests | (planned) `pkg/engine/health/*_test.go`, `pkg/engine/healthjudge_test.go`, `apps/service/internal/server/health_test.go`, `apps/desktop/web/src/studio/health.test.ts` |
| Depends on | [spec_studio_040](spec_studio_040.md) (the kinds), [spec_evals_042](spec_evals_042.md) (pass rates), [spec_workflows_043](spec_workflows_043.md) (receipts, schedules), [spec_gates_041](spec_gates_041.md) (the judge, rules, guardrails), [spec_background_agents_032](spec_background_agents_032.md) (workers) |

## 1. Purpose

Studio builds a workspace's agents, skills, rules, guardrails, evals, workflows and workers one at a time. Nothing shows the whole: how much there is, what's broken, which runs fail or cost the most, which agent has no evals, which skill nothing uses. And nothing says whether an agent's instructions or a skill's description are any good.

The dashboard answers both. **Facts** come from what the workspace already records and cost nothing, so they're always current. **Quality** is a judge agent's verdict on each item, grounded in those facts (its evals, its runs, its failures), not an opinion from nothing; it costs model calls, so it runs when asked or on the user's schedule, and only for what changed.

## 2. Requirements

### Phase A: facts and the dashboard

- **HL-01** **Facts** (`health.Facts`, no model call), per workspace:
  - **Inventory:** agents, skills (by type: simple, references, scripts, meta), rules (gates and instructions), guardrails (built-in and custom, on and off), evals (and their cases), workflows, workers (enabled, disabled), secrets (names only).
  - **Problems:** every file that doesn't load or check (an agent's frontmatter, a workflow's `Problem`, a rule, a guardrail, an eval's table), each with why and a link to it.
  - **Runs** (the last 30 days, from workflow receipts, WF-19, and worker runs): per workflow and worker, runs, success rate, cost per run, median duration, the step that fails most; for the workspace, runs and cost per day.
  - **Evals:** per eval and agent, the last run's pass rate and when; an agent changed since its evals last ran is **stale**; an agent with no eval is **uncovered**.
  - **Safety signals** (the audit log, 30 days): approvals the auto reviewer denied, guardrail blocks and redactions, rules that sent work back (revise), by item.
  - **Unused:** skills no agent, meta skill or workflow names; rules no agent gets; workers that haven't run in 30 days; workflows never run.
- **HL-02** **API:** `GetWorkspaceHealth` (the facts and the latest verdicts, HL-11), `WatchWorkspaceHealth` (the same, again whenever a file, a run or a verdict changes; the page polls an older service).
- **HL-03** **Overview** is Studio's first kind (`OverviewPane`), and Studio opens on it. A visual dashboard first, facts underneath:
  - A **score ring** for the workspace (HL-12; "not judged yet" until phase B, with the facts' own health: problems, failing runs, uncovered agents) and a **ring per kind** (how many, how many with problems or low grades).
  - **Runs:** a 30-day chart of runs per day (done, failed, stopped stacked) with cost as a line; the top five by cost and by failures.
  - **Coverage:** agents with and without evals, and eval pass rates, as bars.
  - **Attention:** problems, failing runs, stale evals, denials and unused items, most urgent first.
  - Every card **expands** in place into its detail (a table: the item, its numbers, a sparkline of its last 30 days, its grade), each row opening the item in Studio (its editor, its canvas, its runs). Expanded cards are remembered per workspace.
  - Charts are drawn in SVG by the page's own components (`ui/charts.tsx`: ring, bars, stacked columns, sparkline; theme tokens, light and dark, no new library), each with a text alternative.
  - The layout follows the room: cards in a grid that reflows to one column on a narrow pane.

### Phase B: the judge, its schedule and the grades

- **HL-10** **The judge** (`healthjudge.go`): one item at a time, the judge model (`[health] model`, else the auto reviewer's, else the default) gets a **rubric** for the item's kind and its **evidence**, and answers JSON: per rubric line a score 1–5 and a reason, an overall score, and up to three fixes, each a concrete change.
  - **Rubrics** (versioned, in the engine):
    - *Agent:* its instructions are clear and complete; its tools fit what it's for (no more); its evals cover what it does; its runs succeed.
    - *Skill:* its description says when to use it (it's what triggers it); its instructions are followable; its scripts run and pass the policy; something uses it.
    - *Rule / guardrail:* it can be judged pass or fail; it isn't redundant with another; how often it fired and was right.
    - *Eval:* its cases cover the agent's job; thresholds are sensible; it isn't always passing or always failing.
    - *Workflow:* failure handling (retries, error links); cost and duration; its success rate; its mode (WF-21) fits what it does.
    - *Worker:* its permissions are as tight as its work allows; its schedule and limits fit; its success rate.
  - **Evidence:** the item's file (secrets redacted, `SecretRedactor`) and HL-01's facts for it: eval results, recent runs' outcomes, failures and costs, signals. Nothing from outside the workspace.
  - The judge never changes anything; a fix is a suggestion.
- **HL-11** **Verdicts are kept** in the service's storage (`<storage>/health/<workspace>/`): each with the item, its content hash, the rubric version, the judge model, the scores, reasons and fixes, its cost and when. A check **re-judges only what changed** (another hash, rubric version or model) unless told to re-judge all. The last 90 days are kept, for trends.
- **HL-12** **Scores:** an item's grade is its overall score (A ≥ 4.5, B ≥ 3.5, C ≥ 2.5, D below); a kind's and the workspace's are their items' average, weighted by use (runs, triggers), problems counting as D.
- **HL-13** **Checking:** `CheckWorkspaceHealth` (every item, a kind or chosen items; `force` re-judges unchanged ones) streams progress item by item, with its cost so far; `EstimateHealthCheck` says how many items would be judged and an estimated cost first; `StopHealthCheck`. One check runs per workspace at a time. Its model calls are priced and counted as the workspace's.
  - The dashboard's **Re-check** (what changed) and **Re-check all** show the estimate, then progress on the score rings as verdicts arrive; each card has its own Re-check; an item's verdict has **Re-judge**.
- **HL-14** **Schedule:** a check runs **nightly at 02:00 by default**, changeable (when, in words or cron, as worker schedules are) or off, in the Overview's settings. It re-judges only what changed. It's kept in the workspace's own user-side settings (`[health] schedule`), never the project's files (a project may not spend the user's money), and run by the service's scheduler while the service runs; a missed night runs at the next start. A scheduled check whose estimate exceeds `[health] max_cost_usd` (default $1) doesn't run and says why.
- **HL-15** **Studio's grades:** each item in Studio's lists shows its grade as a small badge (A–D, coloured; a dot while it's being judged; nothing when not judged), and its editor shows the verdict: scores per rubric line with reasons, the fixes, when it was judged and by which model.
- **HL-16** **Improve:** a fix opens the item in its editor with the change drafted (the draft assist's **Use** / **Discard**, spec_studio_040), applied only when used.
- **HL-17** **Trends:** each grade and score ring shows its change since the last check, and the expanded cards a sparkline of the item's score over the kept history.

## 3. Order and checkpoints

1. **Phase A, engine and API:** `health.Facts` with table tests per source (files, receipts, worker runs, evals, audit), `GetWorkspaceHealth` and `WatchWorkspaceHealth` with service tests.
2. **Phase A, the dashboard:** `ui/charts.tsx`, `OverviewPane`, the fake service's facts; WebKit screenshots (light, dark, narrow). **Checkpoint with the owner.**
3. **Phase B, the judge:** rubrics, `healthjudge.go`, verdict storage and hashing, `CheckWorkspaceHealth` / `EstimateHealthCheck` / `StopHealthCheck`, tests on the mock model.
4. **Phase B, the page:** Re-check with estimate and progress, verdicts in editors, Studio's grade badges, Improve, trends; the schedule and its settings. **Checkpoint with the owner.**
5. **The end:** docs (a guide page, this spec, manual checks), i18n (en-US, es, fr-CA), the full suite and coverage.

## 4. Known gaps

Planned; nothing is built yet.
