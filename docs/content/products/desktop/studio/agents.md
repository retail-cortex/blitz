---
title: Agents
weight: 20
---

**Studio › Agents** is where you write the workspace's own agents: each a specialist with its own instructions, tools and limits, which you can switch to in the chat and which other agents can hand work to. Make one when a kind of task keeps coming back and deserves its own instructions, such as reviewing a change or answering support questions.

## Open it

Choose **Studio › Agents**. Studio's menu shows once **Show advanced** is on, in the Settings title bar.

![Studio › Agents: the workspace's agents in .agents/agents](../../../../images/desktop/studio-agents.png)

The list shows the agents in `.agents/agents`, each with its description and, once the workspace has been checked, its grade. A file that doesn't load says so in red: open it to see why.

## Make an agent

1. Choose **+** (**New agent**).
2. Fill in **Name** and **Description**. The description says what it's for: other agents read it to decide when to call this one, so make it specific.
3. Write its **System instructions**, in Markdown. `{agency_instructions}` is replaced by the rules of its agency level.
4. Choose **Save**. It's saved as `.agents/agents/<name>.md`, and applies from your next message, with no restart.

![An agent's form: name, description, system instructions, Run in the background, its evals and Advanced](../../../../images/desktop/studio-agents-edit.png)

**Redraft** asks the model to write or improve the instructions: leave its request empty to improve what's there, or say what to change.

## The rest of the form

- **Run in the background**: when another agent calls it, it runs as a background task, so the caller keeps working, unless the call says otherwise.
- **Evals**: the evals that check this agent and how their last run went, with **Open evals**. An agent with none offers **Write evals**.
- **Advanced**: where the agent is saved, its file name, its model and model settings, its agency level, the permission mode and most model calls when another agent calls it, its isolation, and its **Tools**. Empty fields use the defaults; the count shows how many are set. An agent with no tools can only reply.

**Delete** removes the file; jobs and pins that name the agent stop working until it's back. **Cancel** leaves without saving, and asks first if there are changes.

More: [Agents and tools](../../../guide/agents.md).
