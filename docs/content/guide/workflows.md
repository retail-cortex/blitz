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
| Extract | Pulls named values out of its input (fields you list, each a type) for later steps and conditions |
| Gather | Joins its inputs, each under its step's title |
| Call an API | An HTTP request: method, URL, query, headers, a text or JSON body, and auth |
| Call an MCP tool | A tool of one of the workspace's MCP servers, with arguments |
| Ask a remote agent | A message to an agent over A2A; its reply is the output |
| Call a workflow | Runs another workflow on its input; its output is that workflow's |
| Script | Runs a skill's script; the input is its last argument, its output the step's |
| Read a file | A workspace file's text |
| Save to a file | Writes its input to a workspace file, once you approve |

Email and data steps are shown on the canvas, and come later.

## Repeating a step

Any step can run **once per item** of a list: set *For each* to an expression giving one (`steps.read.data.orders`), and how many to run at a time. The step's settings see `{{ item }}` and `{{ index }}`; its output is every run's, in order, and its data the list of their results. A **Call a workflow** step runs another workflow as a step, so a long process can be built from smaller ones (workflows can't call each other in a loop).

## Calling services

An **API call**'s values (URL, query, headers, body) take `{{ templates }}` and secrets as `{secrets:key}`, put in as the call is made and never kept in the workflow or shown. Its auth can be a bearer token, basic auth, an API key (in a header or the query) or **OAuth2 client credentials** (a token URL, a client ID and secret, scopes): the token is fetched and kept until it expires. Its output's data is `{status, headers, body}`, so a condition can read `output.data.body.total`; a status of 400 or more fails the step (catch it with an on error link).

```yaml
- id: order
  type: api_call
  url: "https://api.shop.example/orders/{{ input }}"
  headers: [{name: Accept, value: application/json}]
  auth: {type: bearer, token: "{secrets:shop_token}"}
```

An **MCP call** names one of the workspace's [MCP servers](mcp.md), a tool and its arguments; it's approved as the agent's MCP calls are. A **remote agent** step sends a message over [A2A](https://a2a-protocol.org) to the agent whose card is at a URL.

Calls reach the network as `web_fetch` does: only when the web is on, public addresses only (unless `web.allow_private`), and you're asked before a new host, unless it's in `web.allow_domains` or a permission rule allows it.

## Choices, branches and failures

A link can carry a **condition**: an expression in [CEL](https://cel.dev) on the step's result (`output.data.total > 100`, `output.text.contains("approved")`), or one in plain English that a model judges on the step's output ("the customer sounds angry"). **Gateways** decide which links a step's result takes:

| Gateway | Takes |
|---|---|
| Exclusive | its first link whose condition holds, else its *otherwise* link: one way on |
| Inclusive | every link whose condition holds, else its *otherwise* link |
| Parallel | every link: the branches run side by side |

Branches that aren't taken are skipped, and so is everything only they lead to; a step that branches lead back to runs once, after every branch that was taken. A step can be **tried again** when it fails (how many times, how long to wait first) and stopped after a **timeout**; an **on error** link catches a failure and goes on, the error as the next step's input.

Steps pass on text and, when there is some, **data**: JSON an agent answered with, a `.json` file, an Extract step's fields. Later steps read it in their settings as `{{ steps.pull.data.order }}` (any CEL expression in `{{ }}` is filled in as the step starts), and conditions as `steps.pull.data.amount` (`steps["look-up"]` for an ID with a hyphen).

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
  - {id: save, type: save_file, path: briefs/topic.md, position: {x: 840, y: 80}, retry: {attempts: 3, delay: 2s}}
edges:
  - {from: notes, to: gather}
  - {from: look-up, to: gather}
  - {from: gather, to: draft}
  - {from: draft, to: save}
```
