---
title: Jobs
weight: 70
---

**Studio › Jobs** runs a prompt on a schedule, unattended: a nightly dependency check, a weekly report, a morning digest. A job runs in the Blitz service, even when the app is closed, and may only do what you allowed when you enabled it.

## Open it

Choose **Studio › Jobs**. Studio's menu shows once **Show advanced** is on, in the Settings title bar.

![Studio › Jobs: the workspace's jobs with their state and schedule](../../../../images/desktop/studio-jobs.png)

Each job shows its state (**New**, **Enabled**, **Disabled**, **Changed** or **Invalid**), its schedule and what it does. Runs going on now are listed under **Running now**, each with **Stop**.

## Make a job

Ask in the chat, or use the form.

**In the chat.** Say what to do and when: *"Create a job that runs every day at 8 am to fetch the latest AI news from news.google.com and write a summary report to reports/ai-news/ as MM-DD-YYYY.html, glass morphic with a dark theme."* The agent drafts the job and shows you its file and its next runs to approve. It's created paused: the card in the chat has **Review** and **Enable**.

**With the form:**

1. Choose **New job** (the calendar icon). At its top, **Compose with AI** asks what the job should do and a few questions, then fills the form for you to review.
2. Say **What it does** (the **Name** follows from it). Choose when it **Repeats**: every hour, every day, every weekday, every week on the days you pick, or every month on a day, and at what time; or **Custom** for words (*every 2 hours*) or cron. **Time zone** is a list by region, with each zone's offset; it starts as yours. The schedule shows as a sentence, the next runs with how far off they are, and a month with their days marked.
3. Under **Runs**, keep **Its prompt, as a turn** and write the **Prompt**, as you'd type it in the chat, or choose a workflow to run.
4. Under **May do without asking**, **Add a rule** for each thing it may do: a kind (**Run commands**, **Write files**, **Delete files**, **Fetch from the web**, **Use MCP tools**) and what it covers (`reports/**`, `news.google.com`); each rule says what it allows, and its **?** shows what patterns cover (`reports/*` is only the files in `reports/`; `reports/**` is everything under it). Only what it needs. Leave **Agent**, **Model** and the limits empty for the workspace's and the defaults (50 model calls, $1, 30 minutes); a job may set more, unless Settings › Jobs sets a cap.
5. Choose **Create**. It's saved as `.agents/workers/<name>/WORKER.md`, and doesn't run yet.
6. Open it, read what it may do, and choose **Enable as shown**.

## A job's page

![A job: its schedule, what it may do, its limits, and its runs](../../../../images/desktop/studio-jobs-edit.png)

- **Schedule**: when it runs, in words and as cron, with its time zone and its next run.
- **May**: the permissions it was enabled with.
- **Limits**: the most model calls, cost and time a run may take.
- **Content**: the fingerprint of what you enabled. Edit the job and it pauses as **Changed** until you enable it again, so what runs is always what you read.
- **Run now** runs it at once; **Disable** stops its schedule; **Edit** changes it; **Delete** removes it.
- **Runs**: each run's outcome, how it started (scheduled or manual), its cost, the files it changed and, for a failed run, why. **Open its conversation** shows what the agent did.

When a run fails, the Studio menu gets a red dot and the Overview lists it under **Needs attention**. **Settings › Jobs** sets the limits every job is held to, and lists every workspace's jobs.

More: [Jobs](../../../guide/workers.md).
