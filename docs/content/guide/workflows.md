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
| Ask a person | Waits until someone fills in its form; their answer is the output |
| Wait | Waits for a time (`wait: 2h`) or until one (`until: "{{ steps.pull.data.due }}"`) |

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

## People and time

An **Ask a person** step pauses the run until someone answers its form: approve or reject, a note, an amount, a choice. The form is [A2UI](https://a2ui.org) (text, fields, checkboxes, choices, sliders, dates and buttons, laid out in rows, columns, cards and tabs), drawn with the app's own controls; on the canvas, write it as JSON beside a live preview, or have **Draft** write it from a description. Its data can come from the run: `data: {amount: "{{ steps.pull.data.amount }}"}` opens the form with the amount an earlier step found (a template alone keeps its type, so it's a number). The button pressed is the answer's `action`, with the form's values beside it, so a gateway after it can go on with `output.data.action == "approve"`. The service checks every answer against the form (its required fields and checks, and that the button is one of its own), so a script or another app can't answer what the form wouldn't accept.

The form travels as standard [A2UI](https://a2ui.org) messages, so another A2UI client can show a task: `WatchHumanTasks` streams them, and `AnswerHumanTask` takes A2UI's action message.

A **Wait** step pauses the run for a time or until one. A paused run costs nothing while it waits: the service keeps it, picks a timer's run up when it's due, and an answered task's run goes on in the background, in the chat it started in. A workflow you change while a run waits won't resume that run.

Waiting tasks show under the run on the canvas and, from any workspace, as a button in the status bar. How new ones reach you is **Settings › Appearance › Workflow tasks**: *Notify me* (the default), *Show the task at once*, or *Off*. With the app closed, the tray tells you: a dot on its icon, *1 workflow task waiting* in its menu, and a notification that opens a small window with only the task's form. A [job](workers.md) can run a workflow on a schedule (its `workflow` field), so a nightly job can stop and ask before it goes on.

## Building one

In the desktop app, **Studio › Workflows** lists them; **+** starts one, blank or from a template (a research brief; draft and refine; a script's report). The canvas takes the room: Studio's list of kinds shrinks to icons. Open **Steps** on the canvas for every step type by group, and drag one where you want it (or click it to add it in view); drag from a step's right edge to another's left edge to pass its output on, and select a step for its settings. The settings beside the canvas can be made wider or narrower by dragging their edge, or folded away with **»** when you want the whole canvas. An agent step's skills and rules are a drop-down of check boxes.

**Compose with AI** in the same dialog writes one from a description instead: say what it should do, answer the questions it asks (one at a time, with suggested answers as buttons), and it drafts the workflow, checked with the same rules as the canvas (and fixed by the composer when they find problems). It knows the workspace's agents, skills, rules, MCP servers, other workflows and secrets' names (never their values). **Ask for a change** redrafts it; **Open on the canvas** opens the draft as a new, unsaved workflow to review and save. A schedule is set on the canvas after saving, even when the description says when.

## Running and following a run

**Run** asks for the input and runs it in the background. You don't have to watch: close Studio and carry on; the run goes on in the service. While it runs, the status bar shows it (*/research-brief 2/5*); click it to go back to the canvas. If a run fails, you hear of it wherever you are (or as a system notification when the app isn't in front), with **Open** to see where and why; with the app closed, the tray tells you, and its notification opens the app on the run. The tray's menu also lists every run going on, with **Open** and **Stop**, and each workspace's workflows, to run one from there; its icon pulses while one runs. Opening a workflow while a run of it goes, started on the canvas, in a chat or by a job, shows that run.

Under the canvas, the run panel shows how it's going: each step marked on the canvas with what it took and spent, an approval or a person's form it waits on, and three tabs:

- **Steps**: when each step started, how long it took, its tries, the model it used, its tokens in and out, and its cost, with the run's totals.
- **Events**: everything that happened, as it happens: steps starting and ending, tools called, notices.
- **Stream**: the text a step's agent is writing, live.

Every run keeps this **receipt**, so a run is a record of what was done, by which models, when and at what cost. **Past runs** (the clock button) lists a workflow's runs; pick one to see its receipt, and **Copy the receipt** to paste it elsewhere. Fold the panel to its first line, drag its top edge to give it more or less room, or close it; closing it doesn't stop the run. **Stop** stops the run, wherever it was started.

## Its permission mode

A workflow runs in the `auto` [permission mode](safety.md#permission-modes) unless you choose another: a reviewer model decides what would otherwise ask you, so a run in the background or on a schedule doesn't stop for every file it writes. Choose the mode when you make the workflow (**+** asks, with Auto first) or in its settings: *Accept edits*, *Ask*, *Don't ask* (anything that would ask is refused, for unattended runs that must not do more than their rules allow) or *Bypass*.

The workspace's mode is the limit. If you restrict a workspace to *Ask*, its workflows ask too, whatever they choose: the settings say so, and the run's receipt says which mode its steps ran in and why. A workflow can always choose a stricter mode than its workspace's.

## Running on a schedule

A workflow's settings (select nothing on the canvas) have **Schedule**: **Add a schedule**, say when in words or as cron (*every weekday at 9:00*, *hourly*, `0 6 * * 1`) and, if it needs one, its input. Each schedule is a [job](workers.md) that runs the workflow's steps exactly as they are; switch it off and on, change it or remove it there. What a scheduled run may do without asking (write files, use the web) and its limits are the job's, in **Studio › Jobs**.

## The file

A workflow is `.agents/workflows/<name>.flow.yaml`, versioned with the project, beside the [Markdown commands](extending.md#custom-commands) in the same folder (a workflow wins a name clash):

```yaml
description: A one-page brief from the notes and the web
mode: accept-edits   # leave it out for auto
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
