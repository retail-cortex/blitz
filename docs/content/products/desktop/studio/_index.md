---
title: Studio
weight: 20
---

**Studio** is where you shape how agents work in one workspace: the agents themselves, the evals that prove they still work, the skills they follow, the rules and guardrails that check every turn, the jobs and workflows that run without you, the databases they can query, and the secrets they use. Everything Studio makes is a file in the workspace's `.agents/` folder, so you can commit it, review it in a pull request, or write it by hand.

## Open it

Studio is in the menu bar once **Show advanced** is on (in Settings' title bar). Open a workspace, then choose **Studio** and the area you want. It opens as a panel over the chat, with the areas down its left edge; ✕ closes it, and **Studio › Close Studio** too. A dot on **Studio** means a job's last run failed.

![Studio's Overview: the workspace's grade, its runs, eval coverage and what needs attention](../../../images/desktop/studio-overview.png)

Studio only ever changes the workspace it's open in. What every workspace shares (your own agents, skills, jobs, connections and secrets) is in [Settings](../configure.md#global-setup).

## The areas

| Area | What it's for |
|---|---|
| [Overview](overview.md) | The workspace's health at a glance: a grade for each kind, the last 30 days' runs, eval coverage, and what needs attention |
| [Agents](agents.md) | Specialists with their own instructions, tools and limits |
| [Evals](evals.md) | Prompts with the answers you expect, to prove an agent still works after a change |
| [Skills](skills.md) | Recipes the agent follows for one kind of task, some with scripts |
| [Rules](rules.md) | The work's quality: checks on each prompt, each answer, or instructions for some files |
| [Guardrails](guardrails.md) | Safety and limits: prompt injection, secrets and personal data, and your own checks |
| [Jobs](jobs.md) | Prompts or workflows that run on a schedule, unattended |
| [Workflows](workflows.md) | Steps in order, each one's output the next one's input, built on a canvas or composed with AI |
| [Connectors](connectors.md) | Databases your agents can query, read-only unless you say otherwise |
| [Secrets](secrets.md) | Tokens and passwords this workspace's prompts, instructions and servers can use, kept out of conversations and logs |

## A good order

Starting from nothing, set up the agents first, since everything else checks or uses them; then their evals, while you still remember what each agent is for; then rules and guardrails, which watch every turn; then the jobs and workflows that use all of it. Check the [Overview](overview.md) as you go: it says what's missing. The [recipe app tutorial](../../../tutorials/studio-recipe-app.md) does exactly that, step by step, for a real project.
