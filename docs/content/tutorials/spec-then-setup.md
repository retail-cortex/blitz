---
title: "Start any project: spec, then setup"
weight: 5
---

Every project starts the same two ways in Blitz: **`/spec`** writes down what the project is for and what it must achieve, then **`/setup`** fits the workspace to it, with instructions, skills and agents for that kind of work. The project doesn't have to be software: research, data analysis and writing get their own questions, their own stages and their own helpers.

Both run in the desktop app and in the terminal. In the desktop app, type them in the chat (a new workspace's first tile, **Set up this workspace for agents**, offers `/spec` first); in the terminal, type them in a session.

## How the two fit together

1. **`/spec [what you're working on]`** looks at the folder, guesses what kind of project it is, and asks you to confirm: **Software**, **Research or science**, **Data analysis**, **Writing or documentation**, or **Something else**. Then it interviews you, a few questions at a time, with options where there are likely answers. Every kind starts with what the project is for, who it's for, how you'll judge success and what's out of scope; then come the kind's own questions.
2. It shows you the list of specs it plans to write and waits for your go-ahead, then writes them in `.agents/specs/`: `001-overview.md` first, then one spec per stage of the work, numbered in the order the work happens, with `README.md` as the index. `001-overview.md` records the kind on a `Kind:` line.
3. **`/setup`** reads the specs (and the `Kind:` line) and asks only what they don't answer. It makes the folder a git repository with a `.gitignore` that fits, writes `.agents/AGENTS.md` (the instructions every agent reads), points `AGENTS.md`, `CLAUDE.md`, `GEMINI.md` and `BLITZ.md` at it, and offers skills and agents for you to pick.

Nothing is written until you've answered, and everything it writes is yours to edit. Unattended (`blitz init`), both go on from what the files show and list their assumptions under "Open questions".

## Software: a small web API

**The folder:** empty, or a few files of a Go or TypeScript project.

```text
/spec a link shortener for my team
```

It confirms **Software**, then asks about the people who use it and what they must do, performance and scale (how many users, response times), where it runs and how it's deployed, security (how people sign in, what each may do, how sensitive the data is), the data and integrations, constraints (languages, frameworks), quality and delivery. You might answer: *a few hundred links a day, under 100 ms; one container on our cloud; sign-in with the company's Google accounts; admins can delete anyone's links.*

```text
.agents/specs/
├── README.md
├── 001-overview.md        # Kind: software; the users, performance, platform, sign-in
├── 002-architecture.md
├── 003-store.md
├── 004-api.md             # API-01 POST /links … (Requirements, Done when)
└── 005-admin.md
```

Then `/setup` writes `.agents/AGENTS.md` with the build, test and run commands and the boundaries the specs set (sign-in, who may delete), a `.gitignore` for the stack, and offers skills such as *deploy*, *add an endpoint* and *triage a bug*, and agents such as a *reviewer* (read-only) and a *test writer*. The [software developer tutorial](software-developer.md) carries on from here and builds it.

## Research or science: a study

**The folder:** a notebook, a dataset and a bibliography.

```text
study/
├── analysis.ipynb
├── data/survey.csv
└── refs.bib
```

```text
/spec
```

It guesses **Research or science** from the files and asks you to confirm, then asks about the question and your hypotheses, what's known already, the data (where it comes from, its size, its licence, whether it holds personal or sensitive data), the methods (experiments, statistics, the baselines to beat), compute (laptop, cluster or cloud, budget), reproducibility (environments, seeds, versioned data), ethics and compliance (an ethics board or IRB, consent, data agreements), the outputs (paper, thesis, dataset, poster, and where they go) and the timeline.

```text
.agents/specs/
├── README.md
├── 001-overview.md        # Kind: research; the question, data rules, ethics, compute
├── 002-literature.md
├── 003-data.md
├── 004-methods.md
├── 005-experiments.md     # EXP-01 … (Objectives, Done when)
├── 006-analysis.md
└── 007-write-up.md
```

Then `/setup` writes `.agents/AGENTS.md` with where the data and results live, how to run and record an experiment, the environment and the citation style; a `.gitignore` that keeps raw data, results and checkpoints out of git (and suggests git-lfs or DVC when the data must stay with the project); and offers skills such as *run and log an experiment*, *make figures* and *manage citations*, and agents such as a *literature reviewer*, a *statistics critic* (read-only) and a *reproducibility checker*. The [machine learning student tutorial](ml-student.md) shows a study folder like this through a whole term.

## Data analysis: a monthly report

**The folder:** a few SQL queries and a notebook that builds a report.

```text
/spec a monthly churn report for the leadership team
```

It confirms **Data analysis**, then asks about the decision the report informs and who makes it, the sources and how you reach them, data quality, how each metric is defined, the checks that make the numbers trustworthy, how often it's refreshed, the output (report, dashboard, model) and privacy.

```text
.agents/specs/
├── README.md
├── 001-overview.md        # Kind: analysis; the decision, the metric definitions
├── 002-sources.md
├── 003-cleaning.md
├── 004-analysis.md
└── 005-report.md
```

Then `/setup` offers skills such as *refresh the data*, *check a metric* and *build the report*, and agents such as a *data-quality checker* and a *reviewer of the numbers* (read-only).

## Writing or documentation: a handbook

**The folder:** a few Markdown chapters, or nothing yet.

```text
/spec an onboarding handbook for new engineers
```

It confirms **Writing or documentation**, then asks about the audience and purpose, the form and length, the sources and how they're cited, the voice and style guide, who reviews and approves, and where and when it's published.

```text
.agents/specs/
├── README.md
├── 001-overview.md        # Kind: writing; the audience, voice, sources
├── 002-outline.md
├── 003-research.md
├── 004-draft.md           # (Content, Done when)
├── 005-review.md
└── 006-publish.md
```

Then `/setup` offers skills such as *outline a chapter*, *check sources and citations* and *edit to the style guide*, and agents such as an *editor* and a *fact and citation checker*.

## Something else, or a mix

Choose **Something else** and describe it in your own words: `/spec` asks what it delivers, its constraints and its timeline, and plans the stages with you. A project can also be mixed, for example a study that ships a tool: it takes both kinds' questions, and `Kind:` lists both.

## When things change

Plans change. Keep the specs ahead of the work:

1. **Change the spec first.** Edit a file in `.agents/specs/`, or tell `/spec` what changed (`/spec we're adding a second dataset from the 2025 survey`): it asks what it can't work out and updates the specs it touches, keeping their IDs. A new stage takes the next free number; it renumbers only after asking.
2. **Bring the helpers in line.** `/skills update` and `/agents update` compare the skills and agents with the specs and offer what to add, change or retire; nothing changes until you pick. In the desktop app, the welcome screen shows **The specification changed: update the skills and agents** when a spec is newer than them.
3. **Then the work.** Ask the agent to work on a numbered item ("EXP-02 from `005-experiments.md`"), so the request, the result and any review name the same thing.

More: [Extending](../guide/extending.md) for instructions, skills and agents; [the command reference](../products/cli.md) for every command.
