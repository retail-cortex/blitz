---
title: Blitz 0.7.1
weight: 9
---

**Studio: build your agents, test them, give them rules and guardrails, join them into workflows, and see how good all of it is, in one place.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.7.1).

**Everything your agents need, in one place.** **Studio** in the menu bar opens your workspace's whole setup: agents, evals, skills, rules, guardrails, workflows, workers and secrets, each listed and edited with a form. Everything is still a file in `.agents/`, so it goes into git and your team gets it too.

**Know your agents still work.** Write an agent, and Blitz drafts **evals** for it: the questions it should answer well, and what it should refuse. Run them and see each answer beside the one you expected. When it misses, **Refine** rewrites the agent's instructions from the cases it got wrong. Change the agent later and Blitz offers to run them again.

**Workflows you can see.** Drag steps onto a canvas and join them: agents, scripts, API calls, MCP tools, files, branches, retries, a person's approval, a timer. Then run the whole thing as one job, in the background or every weekday at 7. Or just describe it: **Compose with AI** asks a question or two and draws the workflow for you, already checked.

**A grade for everything.** Studio opens on the **Overview**, your workspace's dashboard: what it holds, how its runs go and what they cost, and what needs attention. **Re-check** has a judge model grade each agent, skill, rule and workflow from A to D, with its reasons and suggested fixes. After the first check, it re-checks what changed every night.

**Who should update:** everyone. If you write agents, skills or prompts you keep reusing, this is the release for you.

**See it in five minutes:**

1. Open a workspace and choose **Studio** in the menu bar.
2. **Agents** › open an agent, then **Evals** › **Draft evals** for it, and run them with ▶.
3. **Workflows** › **+** › **Compose with AI**, and describe a job you do every week.
4. Back on **Overview**, choose **Re-check** to see the grades.

New to Studio? The tutorial [A recipe app's harness, in Studio](../tutorials/studio-recipe-app.md) walks through every part on one real project.

### What's new

- **Evals.** Each eval is a table of prompts and expected answers. An answer passes when it matches exactly, or when it means the same thing (scored by a model or by embeddings, against your threshold). Each case runs without changing your files. `blitz eval <name>` runs one in CI and fails the build when a case fails.
- **Rules that check the work.** A rule can now be judged at a fixed point in every turn: on your prompt before the agent starts, or on its answer and changes before the turn ends. It can block, warn, or send the work back to be fixed. **Try it** tests a rule on a sample first.
- **Guardrails at every step.** Three built-ins are on by default:
  - **Prompt injection** watches what web pages and tools bring back.
  - **Secrets and personal data** are masked in answers.
  - **Limits** caps spending, model calls, files changed and domains, and refuses force pushes and `rm -rf /`.

  Add your own in plain English, such as "dropping a database table", at any step.
- **Skills with scripts.** Python, TypeScript, JavaScript, shell and PowerShell scripts in a skill's `scripts/` folder are picked up automatically. Each gets its own packages, and you can test-run it from the form. Scripts run without asking only after you've reviewed them, and a changed script is asked about again. Type `\skill-name` in a prompt to use a skill for that request.
- **Workflows.**
  - Steps can branch, join, repeat for each item in a list, retry and time out.
  - They can call APIs, MCP tools, remote agents and other workflows.
  - They can wait for a person to fill in a form, or until a given time.
  - Every run keeps a receipt: each step, its model, its time and its cost.
  - Runs use the new **Auto** permission mode by default: a reviewer model approves routine actions, so a scheduled run doesn't stop to ask.
- **Compose with AI.** Describe a workflow and answer a question or two. The composer knows your agents, skills and secrets by name, writes the workflow and fixes anything that doesn't check before you see it. Then it opens on the canvas for you to review and save.
- **Workspace health.**
  - Charts of runs and cost over 30 days, and eval coverage per agent.
  - A list of what needs attention: broken files, failing runs, agents with no evals, unused skills.
  - A–D grades with trends, as badges in every Studio list.
  - **Improve** turns a suggested fix into a request for the agent.
  - The nightly check has a cost limit you set.
- **Secrets.** Keep tokens and keys in your system's keychain and write `{secrets:name}` in prompts, agent instructions, MCP settings and workflow steps. The value is put in only at the last moment: transcripts, logs and workflow files keep the placeholder, and if a model repeats the value, it's masked. **Settings › Secrets** holds secrets for every workspace, and **Studio › Secrets** a workspace's own.
- **Settings, reorganised.**
  - Settings are grouped in four sections, with advanced options behind **Show advanced**.
  - **Settings › MCP servers** adds, edits, switches off, restarts and removes servers, and every open workspace gets the change at once.
  - **Settings › Workers** lists every workspace's workers, with limits for all of them.
  - **Global workers** live in `~/.blitz/workers` and run outside any workspace.
- **Workflow tasks wherever you are.** When a workflow waits for you, the status bar says so from any workspace. With the app closed, the tray shows a dot and opens the task.
- **HTML files preview as pages** in the editor.
- **The web tab's address bar searches** when what you type isn't an address.

### Windows

- **A setup program.** `Blitz_0.7.1_windows_x64_setup.exe` installs Blitz for you, with no administrator needed. It adds Blitz to the Start menu, the desktop and **Add or remove programs**, where you can uninstall it (your settings and conversations stay). The zip is still there if you'd rather unzip and run `Blitz.exe`.
- **Secrets in the Windows Credential Manager**, like the Keychain on macOS. Secrets saved by an older version are moved over the first time they're read.

### What's fixed

- The desktop app no longer stops the Blitz service in the middle of a running workflow.
- The Linux tray starts as soon as it's switched on, and login entries from older installs are repaired.
- The limits guardrail's command patterns now match across slashes, so `git push --force*` also catches pushes to `feature/x`.

### Good to know

- **Windows hasn't had a hands-on test for this release.** The Windows app, its setup program and Credential Manager secrets are built for every release, but our automated tests run on macOS and Linux, and nobody has tried this release on Windows by hand yet. If something's off, please [open an issue](https://github.com/retail-cortex/blitz/issues). The app isn't signed yet, so SmartScreen asks first (**More info › Run anyway**).
- Your settings, workspaces and conversations carry over from 0.7.0. The app updates itself, or install the new one over the old.
- Evals, grades and Compose with AI make small model calls. Each says what it costs before or as it runs, and a scheduled check never runs over its limit (a dollar by default).
- Compose with AI writes workflows for now. Skills, agents, evals, rules and workers come next.
- A schedule is added on the canvas after saving, even when your description says when.
- Scheduled checks and workers run while the Blitz service runs.
