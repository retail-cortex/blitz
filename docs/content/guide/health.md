---
title: Workspace health
weight: 59
---

**Studio › Overview** is your workspace's dashboard: what it holds, how its runs go, what needs attention, and how good each agent, skill, rule, guardrail, eval, workflow and worker is. Studio opens on it.

Spec: [workspace health](../about/specs/spec_health_044.md).

## What it shows

The facts come from what the workspace already records, so they're always current and cost nothing:

- **Workspace score**: a ring for the workspace and one for each kind, with how many items each has and how many don't load. Click a kind's ring to jump to its card.
- **Runs, last 30 days**: runs per day (done, failed, stopped) with their cost as a line, the success rate and the total cost. Open the card for what costs the most and what fails the most.
- **Eval coverage**: which agents have evals, how their latest results went, and which changed since their evals last ran.
- **Needs attention**: files that don't load, failing workflows and workers, low grades, agents with no evals or out-of-date evals, the auto reviewer's denials, rules and guardrails that fired, and items nothing uses, most urgent first.
- **One card per kind**: open it for a table of its items, with each one's grade, flags, uses or runs per day, its trend and its cost or eval results. Click a row for its verdict.

Every card opens in place with the arrow in its corner, and the Overview remembers which cards you opened.

## Grades

A judge model rates each item against a rubric for its kind: for an agent, whether its instructions are clear, whether its tools fit, whether its evals cover it and whether its runs succeed; for a workflow, whether it handles failure, what it costs and how often it succeeds; and so on. It's given the item's file and what the records say about it (its eval results, runs, failures and costs), so a grade rests on evidence, not opinion. Each rubric line gets a score from 1 to 5 with a reason, and up to three **suggested fixes**.

An item's grade is its overall score: **A** from 4.5, **B** from 3.5, **C** from 2.5, else **D**. A kind's and the workspace's grades average their items, weighted by how much each is used; a file that doesn't load counts as a D. Grades also show as badges beside each item in Studio's lists.

**Improve** on a fix puts a request in the chat for the agent to make that change: send it, and you review the edit as you would any other (its diff, approvals, undo).

## Checking

**Re-check** judges the items that changed since they were last judged (or have never been); **Re-check all** judges everything again; each kind's card has its own Re-check, and an item's verdict has **Judge again**. Each first says how many items it will judge and what that should cost, and shows its progress as it goes; **Stop** stops it after the item being judged. Verdicts are kept for 90 days, for the trends.

## On a schedule

Once you've checked by hand, the workspace is checked again **every day at 02:00** by default, judging only what changed. Change it with the gear on the Overview: when (in words or as cron, such as *weekdays at 6 AM*, or *off*), the most a scheduled check may cost (estimated first: a check that would cost more doesn't run, and the Overview says why), and the judge's model. These are kept in the workspace's own settings, never in the project's files:

```toml
[health]
model = "gemini/gemini-4-flash"   # empty: the auto mode's reviewer, else the default model
schedule = "every day at 02:00"   # or cron, or "off"
max_cost_usd = 1                  # 0: no limit
```

Scheduled checks run while the service runs and the workspace is open in it; a night missed runs at the next chance.
