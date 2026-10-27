---
title: Blitz 0.8.4
weight: 3
---

**Your agents browse with you.** In Blitz 0.8.4, the agent's web browsing happens in the app. You can watch it in a tab and take over when you want, or let it run out of sight. It can use the sites you've signed in to, one browser profile per workspace. Ask it to open a page and it opens one for you. Files and pictures drag in and out of the file tree. Jobs can be made just by asking in the chat. And when your agents keep hitting the same problems, Blitz tells you it's time to reflect.

### Get started in three steps

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.8.4).
2. Ask the agent to "open github.com". Choose **Embedded browser**, and the page opens in a tab beside your files.
3. Ask for a job: *"Every day at 8 am, fetch the latest AI news and write a summary to reports/ai-news/ as MM-DD-YYYY.html."* Review it, then **Enable** it.

### What's new

**The agent browses in the app.** With the app open, the agent browses there instead of in a hidden Chrome. **Watch it in a tab** to see each step (**Take over** to use the page yourself, **Stop** to end it), or **Run silently**. `web_fetch` gets past bot checks and script-only pages by itself by loading them in the browser. [The browser](../guide/search.md#the-browser)

**Signed in where you are.** Make a browser profile in **Settings › Browser**, sign in to your mailbox, wiki or dashboard, and choose the profile for a workspace. Its agents then browse those sites signed in as you, and nothing leaks between workspaces.

**Pages you ask to see.** Say "open the release notes page" and the agent opens it for you to read, in the embedded browser or your system browser, whichever you choose.

**Drag and drop, both ways.** Drop files and folders from your computer onto the file tree. Drag them out to your desktop or another app to copy them. Put pictures in Markdown with a picker, a drop or a paste.

**Jobs from a sentence.** Ask in the chat for something on a schedule and the agent drafts the job, paused for you to review. New Job has **Compose with AI** too. Pick when it repeats from a calendar-style list and its time zone from a real list. Its permissions are built from simple rules, so `reports/**` covers every folder under `reports/`. [Jobs](../products/desktop/studio/jobs.md)

**See what's running, and how long.** The tray's new **Processes** menu lists every background process your agents started, in every workspace, each with **Stop**, so you can find and end a forgotten dev server without hunting for its chat. The agent's task list shows how long each step has taken, and a background agent's card counts up as it works.

**No more keys in commits.** Repositories Blitz sets up get a pre-commit hook that stops a commit adding an API key, a token or a private key. It names the file and line, with the value hidden. [Safety](../guide/safety.md)

**Agents that learn faster.** `/reflect` now also learns from what went well, and from the chat history and the scripts agents keep in `.agents/scratch`. When the problems since your last reflection add up, the chat suggests running it. Workspaces get `.agents/scripts` for support scripts and `.agents/scratch` for the agent's own working files, kept out of git.

**Smaller things you'll notice.** The status bar's branch and jobs buttons open and close their views. **Include all changes** on the Changes page includes everything, along with any files the agent's commands made. New Python projects ask which uv layout you want before they start.

### Good to know

- Job limits are now defaults rather than caps: a job that sets no limits gets 50 model calls, $1 and 30 minutes, and a job may ask for more unless **Settings › Jobs** sets a cap.
- For a repository with Blitz's older pre-commit hook, click **Protect against committing keys** again to update it.
- Your settings, workspaces and conversations carry over from 0.8.3.
