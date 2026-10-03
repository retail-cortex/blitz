---
title: Software developer
weight: 10
---

This tutorial builds a small program from an empty folder: **linkr**, a URL shortener in Go with an HTTP API. The program doesn't matter much. What matters is the order of the work, the one Blitz itself is built with:

1. [Choose a directory](#1-choose-a-directory)
2. [Run the wizard](#2-run-the-wizard)
3. [Create your skills](#3-create-your-skills)
4. [Create your agents](#4-create-your-agents)
5. [Create your specs](#5-create-your-specs)
6. [Build your software](#6-build-your-software)

The first four steps give the agent what it needs to work the way you want. The last two are the work itself: every feature is written down before it's built, and the agent builds it from what's written.

You need Blitz installed and a model it can use ([Getting started](../getting-started/_index.md)). The steps show the desktop app and the terminal side by side; use whichever you like.

## 1. Choose a directory

A workspace is a folder: everything the agent reads and changes is inside it, and its sessions, settings and checkpoints belong to it.

```sh
mkdir -p ~/src/linkr
```

- **Desktop:** **Workspaces › Open workspace…**, then choose `~/src/linkr`. The workspace opens with its files on the left and the chat beside them.
- **Terminal:** `cd ~/src/linkr && blitz`, or from anywhere `blz -d ~/src/linkr`.

Starting empty is fine. With an existing project, open its root: the folder with its `go.mod`, `package.json` or equivalent.

## 2. Run the wizard

The setup wizard gives the workspace its *agent harness*: the instructions every agent reads, and the skills and agents worth having for this project.

- **Desktop:** a new workspace's chat opens on the tile **Set up this workspace for agents**. It's also **Help › Set up the agent harness**.
- **Terminal:** `/setup` in a session, or `blitz init` from the shell. `/setup testing` focuses it on one area.

The wizard surveys the folder first and changes nothing until it has asked you. In an empty folder it asks what the project is and who uses it, the stack, how it's built, tested and deployed, your conventions, and what agents must never do. In a folder with code it shows what it worked out and asks only about the gaps. For linkr you might answer:

> A URL shortener for my team. Go 1.25, standard library only, SQLite for storage. `go test ./...` must pass before anything is done. Never commit, never touch `deploy/`.

Then it:

1. offers to make the folder a git repository, with a `.gitignore` for the stack;
2. writes **`.agents/AGENT.md`**: what the project is, the exact build, test and run commands, the layout, conventions, boundaries and gotchas, in under 200 lines;
3. points `AGENTS.md`, `CLAUDE.md`, `GEMINI.md` and `BLITZ.md` at it (each holds just `@.agents/AGENT.md`), so every coding agent you use reads the same instructions;
4. proposes skills and agents for you to pick (the next two steps).

```text
linkr/
├── .agents/
│   ├── AGENT.md          # the project, for every agent
│   ├── skills/           # step 3
│   └── agents/           # step 4
├── AGENTS.md  CLAUDE.md  GEMINI.md  BLITZ.md   # each: @.agents/AGENT.md
└── .gitignore
```

Read `AGENT.md` and fix anything the wizard got wrong; it's yours, and short, accurate instructions beat long ones. Then make the first commit. Later, `/memory add <note>` adds a line the agent should always know, and `/memory` shows what's loaded. More in [Extending](../guide/extending.md).

## 3. Create your skills

A skill is a recipe the agent follows for one kind of task: how *this* project runs its checks, cuts a release or adds an endpoint. The wizard proposes up to five; you can write more at any time.

A skill is a folder in `.agents/skills/` with a `SKILL.md`: a short description (agents read it to decide when the skill applies), then the steps.

```markdown
---
name: run-checks
description: Run linkr's checks before calling any change done
---
# Run the checks

1. `gofmt -l .` must print nothing; if it does, run `gofmt -w` on those files.
2. `go vet ./...` must pass.
3. `go test -race ./...` must pass. Fix the code, not the test, unless the test is wrong.
4. Report what ran and what you fixed, in two or three lines.
```

Every skill is also a command: `/run-checks` runs it now, `/run-checks only the store package` with a focus. The agent also activates a skill by itself when its description fits the task.

Skills can carry Python or TypeScript scripts, run in a sandbox that reads the workspace and writes only to its own output folder; a project's scripts run once you trust the project's settings. Most project skills don't need one: plain steps with the project's real commands go a long way. The format, the scripts and the skills policy are in [Skills](../guide/skills.md).

## 4. Create your agents

An agent is a role: its own instructions, the tools it may use, its model, and how freely it acts. Blitz comes with general agents (`blitz`, `qa`, `planning-agent` and others, see [Agents and tools](../guide/agents.md)); your own add the roles this project needs. The wizard offers up to four, such as a reviewer, a test writer or a specialist for the riskiest part of the code.

- **Desktop:** turn on **Settings › Appearance › Show advanced settings**, then **Agents › Add agent**. Fill in a name, a description, and the system instructions; under **Advanced**, the model, agency, permission mode and tools.
- **By hand:** a Markdown file in `.agents/agents/`, the frontmatter for its settings and the body for its instructions.

A reviewer that reads and reports but never edits:

```markdown
---
name: reviewer
display_name: Reviewer
description: Reviews a change against the spec and the project's conventions before it's committed
tools: [read_file, list_files, glob, grep, run_shell_command]
permission_mode: plan
agency_level: medium
---
You review changes in linkr. Read the diff (`git diff`), the spec it implements
and .agents/AGENT.md. Report, most serious first: requirements not met, bugs,
missing tests, and departures from the conventions. Quote file and line.
Never edit files.

{agency_instructions}
```

`permission_mode: plan` keeps it read-only. Switch to an agent with `/agent reviewer` (in the desktop app, also by clicking the agent in the status bar). From then on it works on the conversation, with its history. Agents also hand work to each other: ask the main agent to "have the reviewer check this", and it delegates.

## 5. Create your specs

A spec says what the software must do before anyone builds it. It's the agent's brief, the reviewer's checklist and, later, the record of why things are the way they are. Blitz itself is built this way: every feature starts as a requirement in a spec.

The shape Blitz uses works for any project:

- **One file per area,** numbered in the order to build them, since each builds on the ones before: `docs/specs/spec_store_001.md`, `spec_api_002.md`, `spec_admin_003.md`.
- **Numbered requirements** with a short prefix per spec (`STO-01`, `API-04`), one testable statement each, so a prompt, a test and a review can all name the same requirement.
- **Specs change with the code:** a change in behaviour updates its spec in the same commit, with the date. A spec that drifts from the code stops being worth reading.

Write the first one with the agent, in plan mode, so it asks before writing anything:

```text
/plan Write docs/specs/spec_api_002.md for linkr's HTTP API: create a short link,
follow one, list mine, delete one. Number the requirements API-01, API-02 and so on.
Include the status codes, validation and limits.
```

The agent drafts the plan and asks you to approve it. Read it as you would a colleague's design: this is the cheapest moment to change your mind. A finished spec looks like:

```markdown
# 002 · API

| | |
|---|---|
| Status | Draft |
| Depends on | spec_store_001 |

## 1. Links
- **API-01** `POST /links` with `{"url": …}` creates a short link and answers `201` with `{"code": …}`. A URL that isn't http or https is `400`.
- **API-02** `GET /{code}` answers `302` to the URL; an unknown code is `404`.
- **API-03** Codes are 7 characters from `[a-zA-Z0-9]`, generated, never reused.

## 2. Known gaps
- No custom codes yet.
```

Add a line to `.agents/AGENT.md` so every agent follows the habit: "Specs are in `docs/specs/`; every change implements or updates a numbered requirement there."

## 6. Build your software

Now build from the specs, a requirement or a small group at a time:

```text
Implement API-01 to API-03 from docs/specs/spec_api_002.md, with tests.
Run the checks when you're done.
```

As it works:

- **Approvals.** Edits and commands ask first, unless you've allowed them. Read what it wants to do; **Allow once** is always safe. The permission mode is in the status bar; **Ask** is the default, and [Safety](../guide/safety.md) explains the rest.
- **Watching.** The chat shows each step: the files it reads, the edits, the commands and their results.
- **Reviewing.** **Changes** (desktop), or `/diff` in the terminal, shows everything this session changed, file by file, beside the agent's summary. Then `/agent reviewer` and "review this change against the spec".
- **Undoing.** Every turn is a checkpoint: **Undo last turn** in Changes, or `/undo`, puts the files back as they were. To go further back, hover a prompt and choose **Edit**.
- **Committing.** The agent won't commit unless you ask; commit when the reviewer is satisfied and the checks pass.

Then the next requirements, and the next spec. When a change of plan comes up mid-build, change the spec first, then the code.

Two habits keep this working as the project grows:

- **Short sessions.** A new chat per feature (**+** in the chat, or `/new`) keeps the context on the task. The spec carries what matters from one session to the next, so nothing is lost.
- **Workers for the chores.** A worker is a workflow the service runs on a schedule, unattended and with only the permissions you give it: a nightly dependency check, a weekly report. With advanced settings on, **Workers › Add worker**; see [the service](../products/service.md).

## How this repository does it

Blitz's own repository follows the same order, with two differences worth knowing if you read it:

- **Its instructions point at the site.** The root `AGENTS.md` is a short pointer to [Development](../development/_index.md) and lists the rules that matter most (everything through Bazel, tests with every change, a feature updates its spec).
- **Its specs are part of the documentation.** They're in [About › Specifications](../about/specs/_index.md), numbered in build order, with requirement IDs such as `SK-12` and `DSK-60`. The [contributing guide](https://github.com/retail-cortex/blitz/blob/main/docs/CONTRIBUTING.md) says it plainly: every feature is specified before it's built.
