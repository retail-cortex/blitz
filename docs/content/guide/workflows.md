---
title: Workflows
weight: 58
---

A **workflow** writes down work that's the same few steps every time (read the notes, look something up, draft, tighten, save) and runs them in order, each step's output going to the next. Run one from the chat as `/name`, or build and run it on a canvas in the desktop app.

Spec: [workflows](../about/specs/spec_workflows_043.md).

## Running one

```text
/research-brief coupon rounding
```

A workflow runs as one turn in the chat. Each agent step is a turn of its own, shown as `/research-brief › Draft the brief`, so you see what each step said, approve what it does, and its guardrails and rules apply as in any turn. A line marks each step as it starts; the run stops at the first step that fails, and says why. What follows the name is the input for the first steps.

## The steps

| Step | What it does |
|---|---|
| Agent | An agent works on its input, as its instructions say |
| Refine | An agent improves its input and gives back the better version |
| Summarize | An agent sums up its input (the instructions are the focus) |
| Browse | An agent reaches a goal with its browser tool |
| Gather | Joins its inputs, each under its step's title |
| Script | Runs a skill's script; the input is its last argument, its output the step's |
| Read a file | A workspace file's text |
| Save to a file | Writes its input to a workspace file, once you approve |

Email and data steps are shown on the canvas, and come later.

Agent steps can name an agent (otherwise the active one), skills (given with the prompt), and **rules** their output must pass, beyond the rules every turn has: a failing block rule stops the workflow, a revise rule asks the agent once to fix its answer, a warn rule says so. Evals don't run inside a workflow: they check an agent when you make or change it ([Evals](evals.md)).

## Building one

In the desktop app, **Studio › Workflows** lists them; **+** starts one, blank or from a template (a research brief; draft and refine; a script's report). On the canvas, click a step type to add it, drag from a step's right edge to another's left edge to pass its output on, and select a step for its settings. **Run** asks for the input and runs it in the background: each step is marked as it goes, and anything that needs your approval is asked beside the canvas.

## The file

A workflow is `.agents/workflows/<name>.flow.yaml`, versioned with the project, beside the [Markdown commands](extending.md#custom-commands) in the same folder (a workflow wins a name clash):

```yaml
description: A one-page brief from the notes and the web
nodes:
  - {id: notes, type: read_file, title: Read the notes, path: notes/topic.md, position: {x: 0, y: 0}}
  - {id: look-up, type: browser, title: Look it up, instructions: Find the latest figures., position: {x: 0, y: 160}}
  - {id: gather, type: accumulate, position: {x: 280, y: 80}}
  - id: draft
    type: agent
    title: Draft the brief
    agent: writer
    rules: [cites-sources]
    instructions: Write a one-page brief, the key points first.
    position: {x: 560, y: 80}
  - {id: save, type: save_file, path: briefs/topic.md, position: {x: 840, y: 80}}
edges:
  - {from: notes, to: gather}
  - {from: look-up, to: gather}
  - {from: gather, to: draft}
  - {from: draft, to: save}
```
