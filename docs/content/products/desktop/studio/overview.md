---
title: Overview
weight: 10
---

**Studio › Overview** is the workspace's health dashboard: what it holds, how its runs go, what needs attention, and a grade for each agent, skill, rule, guardrail, eval, workflow and job. Studio opens on it. Look here when you've set a workspace up, after a week of runs, or whenever the Studio menu shows a red dot.

## Open it

Choose **Studio › Overview**. Studio's menu shows once **Show advanced** is on, in the Settings title bar.

![Studio's Overview: the workspace score, the runs chart, eval coverage and the items that need attention](../../../../images/desktop/studio-overview.png)

## What's on it

- **Workspace score**: the workspace's grade, A to D, with its score out of 5, the change since the last check and how many things need attention. Beside it, a ring for each kind with how many items it has; a red count is the items that don't load.
- **Runs, last 30 days**: runs per day, done, failed or stopped, with their cost as a line, the share that succeeded and the total cost.
- **Eval coverage**: how many agents have evals, how many changed since their evals last ran, and each agent's latest result.
- **Needs attention**: files that don't load, failing workflows and jobs, low grades, agents with no evals or evals out of date, most urgent first. The arrow beside an item opens it.
- **A card per kind** (Agents, Skills, Rules, Guardrails, Evals, Workflows, Jobs), with its grade, how many items were judged and flagged, and its own **Re-check**.

The arrow in a card's corner opens it in place: a kind's card becomes a table of its items, with each one's grade, flags, uses and trend. Click an item for its verdict and up to three **Suggested fixes**. **Improve** on a fix puts a request in the chat for the agent to make that change, which you review like any other edit.

The facts come from what the workspace already records, so showing the Overview costs nothing.

## Grade the workspace

A judge model grades each item against a rubric for its kind, using its file and its records (eval results, runs, failures, costs).

1. Choose **Re-check** to judge the items that changed since they were last judged, or **Re-check all** to judge everything again.
2. Blitz says how many items it will judge and about what that costs. Choose **Judge**.
3. The Overview shows its progress, item by item. **Stop** stops it after the item being judged.

Once you've checked by hand, the workspace is checked again every day at 02:00, judging only what changed.

## Change the checks

The gear beside **Re-check all** opens **Check settings**:

- **Schedule**: when items that changed are judged again, in words or as cron (*every day at 02:00*, *weekdays at 6 AM*, *off*).
- **Most a scheduled check may cost (USD)**: estimated before it starts; a check that would cost more doesn't run, and the Overview says why. 0 means no limit.
- **Judge model**: `provider/model`; empty uses the auto mode's reviewer, else the default model.

They're kept in the workspace's own settings, never in the project's files.

More: [Workspace health](../../../guide/health.md).
