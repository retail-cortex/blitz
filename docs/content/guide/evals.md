---
title: Evals
weight: 57
---

An **eval** checks that your agents still answer as they should, after you change their instructions, their skills or their model. It's a list of cases, each a prompt and the answer you expect; Blitz runs each prompt on the agents you name, without changing anything in the workspace, and compares the answers with yours.

Spec: [evals](../about/specs/spec_evals_042.md).

## From an agent to its evals

Evals belong with making an agent, so in Studio they come right after **Agents**:

1. **Write the agent** (or let another agent write it).
2. **Draft its evals.** The agent's form has **Write evals**, and Studio › Evals lists your agents without evals (not Blitz's built-in ones) with **Draft evals**: a model reads what the agent is for and its instructions and proposes cases (typical asks, edge cases, what it should decline). Edit them, then save.
3. **Run them.**
4. **Refine.** When the agent misses cases, **Refine its instructions** has the model rewrite them with those cases in hand; you review the change, and it's saved to the agent.
5. **Run again.** Later, saving the agent (or a skill its evals use) offers to run its evals again.

Evals run when you make or change an agent, never during a conversation: checking each answer as it happens is what [rules and guardrails](extending.md) are for.

## An eval file

An eval is a Markdown file in `.agents/evals/`, so it's versioned with the project. Its cases are a table:

```markdown
---
description: Return policy answers
agents: [support, qa]   # each case runs on each; none: the active agent
skills: [policies]      # given to the agent with every prompt
near: judge             # or embedding
---
Cases from last month's tickets.

| ID | Name | Prompt | Output | Output type | Threshold | Scope |
| --- | --- | --- | --- | --- | --- | --- |
| window | Return window | What's the return window for an online order? | 30 days from delivery, with the receipt. | Near | 0.8 | Fail |
| code | | Reply with only the return code format. | RET-0000 | Exact | | Info |
```

- **Exact** passes when the answer is the expected text (spaces at the ends aside).
- **Near** passes when the answer means the same: scored from 0 to 1 and compared with the threshold. With `near: judge` (the default) a fast model, the suggestions model, scores it and says why; with `near: embedding` it's the similarity of the two texts' embeddings, from search's `search.embedding_model`.
- **Scope** is what a failure means: **Fail** fails the eval, **Warn** is a warning, **Info** is only noted.

A line break in a cell is `<br>`; a `|` is `\|`. Text above and below the table stays as you wrote it.

## What a case may do

Each case runs in a throwaway session, unattended. It can read the workspace, but **never changes files or runs commands**: those actions are refused, and listed with the result. Add `allow: [network]` for the web tools, or `allow: [mcp]` for MCP tools. A case stops after 25 model calls or 5 minutes, and your guardrails apply as in any turn.

## Running one

In the desktop app, **Studio › Evals** lists the evals with their last run; ▶ runs one. An eval's **Cases** tab edits its cases (each opens into its editor), **Settings** its agents, skills and scoring, and **Runs** shows the run as each case is scored, with what it has cost so far (each case's agent calls and the judge's scoring), then past runs and their cost: open a result to see the expected answer beside the agent's, and why.

From the shell, for CI:

```text
blitz eval returns              # each result as it's scored, then a summary
blitz eval returns --case window
blitz eval returns --json       # the whole run as JSON
```

`blitz eval` exits 0 when the eval passes, 1 when a Fail case fails, and 2 when there's no such eval or it has a problem. Runs are kept in `~/.blitz/eval-runs/`, never in the project.
