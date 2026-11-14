---
title: Workflows
weight: 80
---

**Studio › Workflows** writes down work that's the same few steps every time (read the notes, look something up, draft, tighten, save) and runs them in order, each step's output going to the next. You build a workflow on a canvas, or describe it and have it composed for you, then run it from Studio, from the chat as `/name`, or on a schedule.

## Open it

Choose **Studio › Workflows**. Studio's menu shows once **Show advanced** is on, in the Settings title bar.

![Studio › Workflows: the workspace's workflows, each with its steps and grade](../../../../images/desktop/studio-workflows.png)

Each workflow shows how many steps it has, what it's for and, once the workspace has been checked, its grade; ▶ runs it. One that doesn't load says why in red.

## Make a workflow

1. Choose **+**. Start blank or from a template (a research brief; draft and refine; a script's report), or choose **Compose with AI**: say what it should do, answer its questions, and it drafts the workflow; **Open on the canvas** opens the draft to review.
2. Open **Steps** on the canvas for every step type by group. Drag one where you want it, or click it to add it in view.
3. Drag from a step's right edge to another's left edge to pass its output on. Select a step for its settings.
4. Select nothing for the workflow's own settings: its **Name** (you run it as `/name` in the chat), **What it's for**, and its **Permission mode** (**Auto** by default: a reviewer model decides what would ask, and rules still ask you).
5. Choose **Save**. It's saved in `.agents/workflows/<name>.flow.yaml`.

![A workflow on the canvas, with its settings beside it](../../../../images/desktop/studio-workflows-edit.png)

The canvas takes the room: Studio's list of areas shrinks to icons. **»** folds the settings away when you want the whole canvas, and their edge drags wider or narrower.

## Run it

1. Choose **Run**, and give it its input.
2. It runs in the background: close Studio and carry on. The status bar shows it while it runs; click it to come back to the canvas.
3. Under the canvas, the run panel shows each step with what it took and spent, and anything it's waiting on, such as an approval or a person's answer.

If a run fails, you hear of it wherever you are, with **Open** to see where and why.

## Run it on a schedule

In the workflow's settings, **Schedule** › **Add a schedule**, and say when, in words or as cron (*every weekday at 9:00*). Each schedule is a [job](jobs.md): what a scheduled run may do without asking, and its limits, are the job's, in **Studio › Jobs**.

More: [Workflows](../../../guide/workflows.md).
