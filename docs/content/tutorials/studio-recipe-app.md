---
title: "A recipe app's harness, in Studio"
weight: 15
---

This tutorial takes one real project and gives it everything an agent needs to work on it well. The project is **Recipe Books**: a mobile-first web app where people collect recipes by hand, from a photo or by talking to an AI agent, then search them, share books and build shopping lists. You'll set up its agents, their evals, skills with scripts, rules, guardrails, secrets, a scheduled workflow and the dashboard that grades all of it, every one in **Studio**:

1. [Start from the spec](#1-start-from-the-spec)
2. [Open Studio](#2-open-studio)
3. [An agent that reviews, and changes nothing](#3-an-agent-that-reviews-and-changes-nothing)
4. [Evals: proof the agent still works](#4-evals-proof-the-agent-still-works)
5. [A skill with a script](#5-a-skill-with-a-script)
6. [Secrets](#6-secrets)
7. [Rules: the work's quality](#7-rules-the-works-quality)
8. [Guardrails: safety and limits](#8-guardrails-safety-and-limits)
9. [A workflow, composed with AI](#9-a-workflow-composed-with-ai)
10. [Run it every weekday](#10-run-it-every-weekday)
11. [Grade the whole workspace](#11-grade-the-whole-workspace)

The order matters. Agents come first because everything else checks or uses them. Evals follow straight away, while you still remember what the agent is for. Rules and guardrails watch every turn after that, and the workflow at the end uses all of it. Writing the app's code is the [software developer tutorial](software-developer.md)'s job. This one builds the harness that code gets written in.

You need the desktop app and a model it can use ([Getting started](../getting-started/_index.md)). Everything Studio makes is a file in `.agents/`, shown here as well, so you can write any of it by hand, review it in a pull request, or use it from the terminal. Most steps cost nothing. Drafting with AI, running evals and grading make small model calls (cents for this project), and each one says what it costs before or as it runs.

## 1. Start from the spec

```sh
mkdir -p ~/src/recipes
```

Open the folder (**Workspaces › Open workspace…**) and choose the first tile, **Set up this workspace for agents**, or type:

```text
/setup a recipe app: people save recipes by hand, from a photo or by talking to an agent, then search, share and shop from them
```

Setup confirms it's **Software** and interviews you. You might answer: *a PWA in React 19 and Tailwind; Go with Gin and GORM; Gemini through Google's ADK; Cloud Run, AlloyDB with pgvector, Cloud Storage; Google sign-in, with Owner, Editor and Viewer roles; 10,000 users, search under 300 ms at p95.* It shows the specs it plans to write, waits for your go-ahead, then writes them ([Start any project](spec-then-setup.md) explains each question):

```text
.agents/specs/
├── README.md
├── 001-overview.md                       # Kind: software; OVW-05: 10,000+ users, <300 ms p95
├── 002-architecture.md
├── 003-data-model.md
├── 004-authentication-and-access.md
├── 005-recipes-and-books.md
├── 006-search-and-discovery.md           # SRCH-06: search under 300 ms at p95
├── 007-agent-workflows.md
└── 008-shopping-and-recommendations.md   # SHOP-02: 1 cup flour + 2 cups flour = 3 cups flour
```

Then setup writes `.agents/AGENTS.md` (the stack, the Bazel commands, the layout) and offers skills and agents that fit the specs. For Recipe Books it offered skills such as `add-api-endpoint`, `db-migration`, `search-and-shopping-aggregation`, `pwa-feature` and `e2e-playwright`, and four agents: a search and performance reviewer, a security and access auditor, an ADK and Gemini specialist, and an accessibility reviewer. Take them all; you can delete any later. Make the first commit.

## 2. Open Studio

**Studio** in the menu bar opens the workspace's harness: one list of kinds on the left (Overview, Agents, Evals, Skills, Rules, Guardrails, Jobs, Workflows, Secrets) and the selected kind beside it. It opens on **Overview**.

The Overview costs nothing to show, because it reads only what the workspace already records. Right after setup you'll see:

- a ring per kind with how many each has (4 agents, a dozen skills, no workflows yet);
- **Needs attention**, which already has items: four agents with **no evals**, and skills that nothing uses yet;
- an empty runs chart, since nothing has run.

The workspace's own ring says **Not judged yet**. Leave it for now; you'll grade it in step 11, once there's something worth grading.

## 3. An agent that reviews, and changes nothing

**Studio › Agents** lists the four. Open **Search & Performance Reviewer**. Its form shows the description, the instructions, its tools and, under **Advanced**, its permission mode. The file behind it, `.agents/agents/search-perf-reviewer.md`, begins:

```markdown
---
name: search-perf-reviewer
display_name: Search & Performance Reviewer
description: Reviews AlloyDB GORM schemas, pgvector HNSW/ScaNN indexes, hybrid KNN + tag/ingredient search queries, and shopping list aggregation for the <300ms p95 latency target at 10,000+ user scale.
tools: [read_file, list_files, glob, grep]
agency_level: medium
permission_mode: plan
---

You are the Search & Performance Reviewer for this project. You operate in read-only `plan` mode …
```

Three choices make it a good reviewer:

- **The description is specific.** The main agent reads it to decide when to hand work over, so "reviews search queries for the 300 ms target" gets used and "helps with the database" doesn't.
- **Its tools only read.** With no edit or shell tools it can't change anything, even by mistake.
- **`permission_mode: plan`.** When another agent hands it work, it plans and reports instead of acting.

Its instructions also name the spec IDs it checks against (`SRCH-01`–`SRCH-06`, `SHOP-02`), so its reviews cite requirements, not taste. Change anything here and **Save**: it applies from your next message, with no restart. More in [Agents](../guide/agents.md).

## 4. Evals: proof the agent still works

Instructions drift. A model update, a tidy-up of the prompt or a new skill can quietly break an agent that worked last week. An **eval** catches that: a table of prompts, each with the answer you expect, run on the agent whenever it changes.

**Studio › Evals** lists your agents that have no evals, each with **Draft evals**. Choose it for **search-perf-reviewer**. A model reads what the agent is for and its instructions, then proposes cases: typical asks, edge cases, and what it should decline. For Recipe Books it drafted cases like these (shortened):

| ID | Prompt | Output (expected) | Type | Threshold | Scope |
|---|---|---|---|---|---|
| post-filter-rbac-vector-search | Review this GORM vector search: it fetches `limit * 2` matches, then calls `canView()` on each in a loop | Breaks `SRCH-01`: filtering access in Go loses top-K results and makes N+1 queries; put the access check in the SQL `WHERE` | Near | 0.7 | Fail |
| vector-index-l2-vs-cosine | Review `CREATE INDEX … USING ivfflat (embedding vector_l2_ops)` | Use `hnsw` with `vector_cosine_ops` for `<=>`, and add composite B-tree indexes on the join tables | Near | 0.7 | Fail |
| decline-write-code-request | Go ahead and edit `repository.go` to add the HNSW migration | It's read-only: it gives the migration and the query for you to apply | Near | 0.7 | Fail |

The last case matters as much as the others: a reviewer that starts editing when asked nicely is broken. Edit the cases until each expected answer says what *you* would accept, then **Save**. They're kept in `.agents/evals/search-perf-reviewer.md` as a Markdown table, so a pull request shows exactly what changed.

**Near** means "says the same thing". A fast model scores the agent's answer from 0 to 1 against yours and says why. **Exact** means character for character, which suits answers like a number or a code. **Scope** says what a miss means: **Fail** fails the eval, **Warn** and **Info** only report it.

Now run it with ▶. The **Runs** tab scores each case as it finishes, with its cost so far. Open a result to see the expected answer beside the agent's, and the judge's reason. Each case runs in a throwaway session that can read the workspace but can't change files or run commands.

When a case misses, choose **Refine its instructions**. The model rewrites the agent's instructions with the failed cases in hand, you review the change, and it's saved to the agent. Run again. From now on, saving the agent offers to rerun its evals. Repeat this for the other three agents. In CI, the same eval runs from the shell and fails the build on a **Fail** case:

```sh
blitz eval search-perf-reviewer
```

More in [Evals](../guide/evals.md).

## 5. A skill with a script

A skill is a recipe the agent follows for one kind of task. Most are instructions, but some steps are better as code: they're exact, quick and cost no tokens. Shopping lists are one. `SHOP-02` says `1 cup flour` + `2 cups flour` must become `3 cups flour`, and arithmetic is a script's job, not a model's.

**Studio › Skills** › **search-and-shopping-aggregation** › **Scripts** › new script, Python. Name it `normalize.py` and write (or describe it and use **Redraft** to have the model write it):

```python
"""normalize.py: shopping list lines, summed by canonical ingredient (SHOP-02)."""

import sys

# Each unit, as its base unit and how many of those it is.
UNITS = {"g": ("g", 1), "kg": ("g", 1000), "ml": ("ml", 1), "l": ("ml", 1000),
         "tsp": ("tsp", 1), "tbsp": ("tsp", 3), "cup": ("cup", 1), "cups": ("cup", 1)}


def main(args: list[str]) -> int:
    totals: dict[tuple[str, str], float] = {}
    for line in "\n".join(args).splitlines():
        parts = line.split(maxsplit=2)  # "2 tbsp Olive Oil"
        if len(parts) < 3:
            continue
        qty, unit, name = parts
        base, factor = UNITS.get(unit.lower(), (unit.lower(), 1))
        key = (" ".join(name.lower().split()), base)
        totals[key] = totals.get(key, 0) + float(qty) * factor
    for (name, unit), qty in sorted(totals.items()):
        print(f"{qty:g} {unit} {name}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
```

**Test run** it with three arguments, `"1 cup Flour"`, `"2 cups flour"` and `"2 tbsp Olive Oil"`:

```text
3 cup flour
6 tsp olive oil
```

A script in `scripts/` is picked up by its folder, so the skill is now **With scripts**, and the agent can run it with `run_skill_script` instead of adding numbers itself. A new or changed script is marked **Scripts to review**. Read it and choose **Trust these scripts**. Until then the agent asks before each run, and an unattended run is refused. This one uses only Python's standard library. A script that needs packages lists them in the Scripts tab's **Packages** (its `requirements.txt`), and they're installed in the skill's own environment once you approve.

Back on **General**, add the agents it's offered to (`search-perf-reviewer`), so the reviewer checks aggregation code against the same rules the script follows. In any chat, `\search-and-shopping-aggregation` in a prompt uses the skill for that request. More in [Skills](../guide/skills.md).

## 6. Secrets

The workflow in step 9 calls the staging server, which needs a token. A token pasted into a prompt or a file ends up in transcripts, logs and git. A **secret** stays in your OS keychain and is named instead: `{secrets:staging_token}`.

**Studio › Secrets** › add one: name `staging_token`, then the value. The list shows each secret's name (as you'd write it) and when it was set, never the value. **Replace** sets a new value without showing the old one.

`{secrets:staging_token}` now works in prompts, agent instructions, MCP server settings and workflow steps. The value goes in only as the model or the service is called. The transcript, the audit log and the workflow file keep the placeholder, and if a model ever echoes the value back, it's shown masked. A workspace's secrets are its own. **Settings › Secrets** holds global ones for every workspace, and the workspace's own wins a name clash.

## 7. Rules: the work's quality

Instructions ask; a **rule** checks. A rule with `when` is judged by a fast model at a fixed point in every turn: `before_agent` on your prompt, or `after_agent` on the answer and the files it changed. For Recipe Books, the spec requires tests for every backend change (`OVW-11`), so make that a rule the agent can't skip.

**Studio › Rules** › **+**, named `backend-tests`:

```markdown
---
description: Backend changes come with their tests
when: after_agent
action: revise
order: 10
---
When the change touches Go code under backend/, it adds or updates a Testify test for it, and the answer says which test ran.
```

`action` is what a failure does:

- `revise` sends the reason back for the agent to fix its work, twice at most;
- `warn` only says so in the chat;
- `block` marks the answer as failing.

Before you rely on it, use **Try it**. Paste an answer like "Added the `/api/v1/books/:id/share` endpoint" and it fails ("no test named"). Add "`bazel test //backend/internal/handlers:handlers_test` passes" and it passes. Rules run in order (drag to reorder), and `agents: [name]` limits one to some agents. More in [Extending](../guide/extending.md).

## 8. Guardrails: safety and limits

Rules are about quality. **Guardrails** are about what must never happen, and they watch every step: your prompt, each tool call before it runs, what each tool brings back, and the answer. **Studio › Guardrails** shows the three built-ins, on by default:

- **limits** refuses force pushes and `rm -rf` of `/` or `~`, and can cap cost, model calls and files changed per turn;
- **prompt_injection** watches what web pages, the browser and MCP servers return for text aimed at the agent;
- **secrets** masks credentials in answers.

Tune two of them for this project. The app holds people's names and email addresses, so turn on **PII** for `secrets`. Deploys go through CI, never the agent, so add `gcloud run deploy*` to the commands `limits` refuses. The list you give replaces the defaults, so keep force pushes in it:

```markdown
---
builtin: limits
max_cost_usd: 2
commands: ["git push*--force*", "git push* -f*", "gcloud run deploy*"]
---
```

Then add your own, in plain English. `003-data-model.md` keeps data changes in migrations, so a shell command that drops a table is always a mistake:

```markdown
---
description: Tables change only through migrations
stage: action
action: block
tools: [run_shell_command]
---
Dropping or truncating a database table, or deleting a Cloud Storage bucket.
```

And one on prompts, which stops anyone (or anything pasted in) from asking for keys:

```markdown
---
description: Stops prompts asking to reveal keys
stage: input
action: block
---
The prompt asks to reveal, print or send an API key, token or password.
```

**Try it** checks each against a sample: `psql -c 'DROP TABLE recipes'` is blocked, and `bazel run //backend/cmd/migrate` isn't. A guardrail that can't be checked, say because the model is unreachable, stops what it guards rather than letting it through.

## 9. A workflow, composed with AI

The reviewer is useful when you remember to ask it. A **workflow** makes it a routine: steps joined on a canvas and run as one job. Here the routine is a daily check that staging's search is still under `SRCH-06`'s 300 ms.

You could drag the steps onto the canvas yourself. Describing it is faster. **Studio › Workflows** › **+** › **Compose with AI**:

> Every weekday morning, call staging's search health endpoint, https://staging.recipes.example/api/v1/search/health, with the staging_token secret. Have search-perf-reviewer compare the numbers with the 300 ms p95 target in SRCH-06 and say what to fix. Save the review to reports/search-latency.md.

The composer knows the workspace: its agents, skills, rules, other workflows, and the names (never the values) of its secrets. It asks only what the request leaves open, one question at a time, with likely answers as buttons. Here it might ask whether a failed call should still write a report. Then it drafts the workflow and checks it with the same rules the canvas uses. When those rules find problems, it fixes them before you see the draft. You get its name, a summary and the steps in order:

```text
search-latency-check · 3 steps
1. Call the health endpoint · Call an API
2. Review against SRCH-06 · Agent (search-perf-reviewer)
3. Save the review · Save to a file
```

Your draft may differ. **Ask for a change** redrafts it ("also keep last week's numbers for comparison"). **Open on the canvas** opens it as a new, unsaved workflow. Click each step to read its settings. The API call should send `Authorization: Bearer {secrets:staging_token}`, and the save step should write `reports/search-latency.md`. Then **Save**. The file is `.agents/workflows/search-latency-check.flow.yaml`:

```yaml
description: Checks staging's search latency against SRCH-06 and saves a review
nodes:
  - id: health
    type: api_call
    title: Call the health endpoint
    url: https://staging.recipes.example/api/v1/search/health
    auth: {type: bearer, token: "{secrets:staging_token}"}
    position: {x: 0, y: 80}
  - id: review
    type: agent
    title: Review against SRCH-06
    agent: search-perf-reviewer
    instructions: Compare these numbers with the 300 ms p95 target in SRCH-06. Say what passes, what doesn't, and what to fix first.
    position: {x: 280, y: 80}
  - id: save
    type: save_file
    title: Save the review
    path: reports/search-latency.md
    position: {x: 560, y: 80}
edges:
  - {from: health, to: review}
  - {from: review, to: save}
```

**Run** it, with the input left empty (its first step needs none). It runs in the background, so you can close Studio, and the status bar shows its progress. The run panel under the canvas marks each step as it finishes, with the model it used, its tokens and its cost. The workflow runs in **Auto** mode by default: a reviewer model decides what would otherwise ask you, such as reaching a new host or writing the report, so the run doesn't stop for each one. The run keeps a **receipt** of what each step did, which model did it, when, and what it cost. In a chat, `/search-latency-check` runs it too. More in [Workflows](../guide/workflows.md).

## 10. Run it every weekday

The composer doesn't set schedules, because a schedule lives outside the workflow's file. Select nothing on the canvas, then **Schedule** › **Add a schedule**: *every weekday at 7:00*.

The schedule is a **job**, `search-latency-check-schedule`, created and enabled for you. It runs the workflow's steps exactly as they are, and it may do without asking only what those steps do: reach `staging.recipes.example` and write `reports/search-latency.md`. Open it in **Studio › Jobs** to read those permissions. Limits on model calls, cost (a dollar by default) and time stop a run that goes wrong. Editing the job pauses it until you enable it again, so what runs is always what you read. When a scheduled run fails, the app tells you, or the tray does if the app is closed. More in [Jobs](../guide/workers.md).

## 11. Grade the whole workspace

Back on **Overview**, the facts have filled in: the workflow's runs and cost on the chart, eval coverage for the reviewer, and fewer items needing attention. Now grade it. **Re-check** says how many items it will judge and about what that will cost, then judges them one at a time while the rings fill in.

A judge model rates each item against a rubric for its kind. An agent is rated on how clear its instructions are, whether its tools fit, whether its evals cover it and whether its runs succeed. A workflow is rated on how it handles failure, what it costs and how often it succeeds, and so on. The judge sees the item's file and what the records say about it, so a grade rests on evidence. Items get **A** to **D**, shown as badges in every Studio list.

Open a low grade. Each rubric line has a score from 1 to 5 and a reason, plus up to three suggested fixes. For this workspace, expect something like:

- *search-latency-check*: "a failed API call stops the run with no report; add an on-error link to a step that saves what failed".

**Improve** on a fix puts that request in the chat for the agent to make. You review the edit like any other: its diff, approvals, undo.

After that first check by hand, the workspace is re-checked **every night at 02:00**, judging only what changed. The gear on the Overview changes when it runs, the most a scheduled check may spend, and the judge's model. More in [Workspace health](../guide/health.md).

## What you have

```text
recipes/
└── .agents/
    ├── AGENTS.md                         # the project, for every agent
    ├── specs/                            # 001–008: what it must do
    ├── agents/                           # four, each with evals
    ├── evals/search-perf-reviewer.md     # its cases, run on every change
    ├── skills/search-and-shopping-aggregation/
    │   ├── SKILL.md
    │   └── scripts/normalize.py          # SHOP-02, exactly
    ├── rules/backend-tests.md            # every backend change tested
    ├── guardrails/                       # limits, secrets (PII), no dropped tables, no key requests
    ├── workflows/search-latency-check.flow.yaml
    └── workers/                          # its weekday schedule
```

Commit it. Everyone who opens the project gets the same agents, evals, skills, rules, guardrails and workflows. Secrets stay in each person's keychain, and the job runs only where someone has enabled it. From here, build the app the [software developer tutorial](software-developer.md)'s way. Each feature then gets written in a workspace that tests its own agents, checks every turn and grades itself every night.
