---
title: Evals
weight: 30
---

**Studio › Evals** checks that your agents still answer as they should. An eval is a set of cases, each a prompt and the answer you expect; a run gives each prompt to the agents you choose, without changing files, and compares their answers with yours. Write evals right after an agent, and run them whenever you change it: a model update or a tidied prompt can quietly break an agent that worked last week.

## Open it

Choose **Studio › Evals**. Studio's menu shows once **Show advanced** is on, in the Settings title bar.

![Studio › Evals: the evals with their last run, and the agents that have none](../../../../images/desktop/studio-evals.png)

Each eval shows its cases, its last run (passed, warnings, cost) and ▶ to run it. **Agents without evals** lists your agents that nothing checks yet, each with **Draft evals**.

## Draft evals for an agent

1. Under **Agents without evals**, choose **Draft evals** beside the agent (or **Write evals** in the agent's own form).
2. A model reads what the agent is for and its instructions and proposes cases: typical asks, edge cases, and what it should decline. Say what you'd like covered, if anything, then choose **Draft**.
3. Edit the cases until each expected answer is one you'd accept. Nothing is saved until you save.
4. Choose **Save**. It's saved as `.agents/evals/<name>.md`, a Markdown table of cases.

**+** (**New eval**) starts an empty one instead.

## Edit the cases

![An eval's Cases tab: each case's prompt and how it's compared, with Add a case and Draft cases](../../../../images/desktop/studio-evals-edit.png)

An eval has three tabs:

- **Cases**: each case, with its prompt and how it's compared. Open one to edit it: **ID**, **Prompt**, **Expected answer**, **Compared** (**Exact**, the expected text; **Near**, the same meaning, scored from 0 to 1 against the **Threshold**) and **If it fails** (**Fail** fails the eval, **Warn** warns, **Note** only notes it). **Add a case** adds one; **Draft cases** has the model propose more. The arrows reorder them.
- **Settings**: its name, the agents it runs on, and the rest of how it's run and scored.
- **Runs**: the run going on, each case scored as it finishes with its cost so far, then past runs. Open a result to see the expected answer beside the agent's, and why it was scored that way.

## Run it, and refine the agent

1. Choose **Run** (or ▶ in the list). The run goes on in the background: leave the eval, or close Studio, and it carries on. Open the eval again to follow it, or **Stop** it.
2. When the agent misses cases, choose **Refine *agent*'s instructions**. The model rewrites the instructions with the failed cases in hand; you review the change, and it's saved to the agent.
3. Run it again.

From then on, saving the agent offers to run its evals again. In CI, `blitz eval <name>` runs an eval and fails on a **Fail** case.

More: [Evals](../../../guide/evals.md).
