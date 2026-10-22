---
title: Rules
weight: 50
---

**Studio › Rules** holds the workspace's expectations of the work, in plain English. A rule can be checked: before the agent runs (does the prompt say what to change?) or after it (does the answer say how the change was tested?), judged by a fast model. Or it can be an instruction, which isn't checked but goes to the agent as guidance, always or when it reads or edits the files it names. Rules are about the quality of the work; for safety and limits, use [guardrails](guardrails.md).

## Open it

Choose **Studio › Rules**. Studio's menu shows once **Show advanced** is on, in the Settings title bar.

![Studio › Rules: rules before the agent, after the agent, and instructions](../../../../images/desktop/studio-rules.png)

The list has three groups:

- **Before the agent**: each judges the prompt before the agent runs, in this order.
- **After the agent**: each judges the agent's answer and the files it changed, in this order, before the turn ends.
- **Instructions**: not checked; they go to the agent as instructions, always or for the files they name.

Each rule shows what happens when it fails and what it's for. Drag a rule by its handle, or use its arrows, to change the order. A rule from another folder, such as `.claude/rules`, is read here and edited there.

## Make a rule

1. Choose **+** (**New rule**).
2. Give it a **Name** (its file's name in `.agents/rules`) and say **What it's for**.
3. Choose **When**: **Before the agent**, **After the agent** or **Instruction**.
4. For a checked rule, choose **When it fails**:
   - **Block**: before the agent, the prompt is stopped with the reason; after it, the answer is marked in the chat as failing the rule.
   - **Warn**: it goes ahead, with a warning in the chat.
   - **Revise**: before the agent, the reason goes to the agent with the prompt; after it, the agent is sent back to fix its answer, twice at most.
5. **Applies to**: every agent, or only the agents you choose.
6. Write **The rule**: what passes and what fails. **Redraft** has the model write or improve it. For an instruction, say what the agent should do, and name the **Files** it's for (globs such as `*.go`), or leave them empty for always.
7. **Try it** with a sample prompt or answer, and choose **Check** to see whether it passes and why.
8. Choose **Save**. It's saved as `.agents/rules/<name>.md`, and applies from your next message.

![A rule's form: name, what it's for, when, when it fails, who it applies to and the rule itself](../../../../images/desktop/studio-rules-edit.png)

A rule that can't be checked (the model is unavailable, say) lets the turn go on.

More: [Extending Blitz](../../../guide/extending.md#project-memory).
