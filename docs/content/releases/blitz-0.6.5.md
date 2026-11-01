---
title: Blitz 0.6.5
weight: 11
---

**Branches on the Changes page, version control in plain words, and instructions that take effect from your next message.** Download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.6.5).

**Work on a branch without a terminal.** The Changes page now starts with the branch you're on. Click it to see your branches and the ones on the remote, each with its last commit. Switch to one, start a **New branch…**, or **Merge into *main*…**. A merge that runs into conflicts stops in the same banner as a pull, where you keep yours, take theirs, or ask the agent to settle them.

**Edit a rule and the agent follows it on the next message.** Change `AGENTS.md`, a rule in `.agents/rules/` or a skill's `SKILL.md`, and the next message you send uses the new text, in the conversation you're in. Before, the old text stayed until you reopened the workspace. Nothing is read again when nothing changed, so turns stay as quick and as cheap as before.

**Who should update:** everyone who uses Gemini with project rules or notes containing braces like `{user_id}`, since those could stop every turn with an error. Also anyone who works with branches, or who tunes their agent's rules as they go.

**See it in a minute:**

1. Open a folder that's under version control and go to **Changes**.
2. Click the branch name at the start of the toolbar, then **New branch…**, and give it a name.
3. Commit something, switch back to `main` from the same menu, and choose **Merge into main…**.

### What's new

- **Branches.** Switch between local branches and the remote's. Choosing a remote branch makes a local one that follows it, so Pull and Push work right away. Your uncommitted changes come along, unless the other branch would overwrite them, in which case Blitz says so and stays put.
- **Merging branches.** **Merge into *current*…**, or the merge button on a branch's row, brings that branch's work in. Blitz tells you how many commits arrived, or that you already have everything.
- **Plain words for version control.** The Changes page and the Files menus say what an action does: **Include in commit** and **Leave out of commit** instead of stage and unstage, **Do not version** and **Version it again** instead of .gitignore, **View history…**, **Commit changes…** and **Turn on version control**. The same in every language.
- **Instructions that apply right away.** Edits to instruction files, rules and skills reach the next message, whether you, your editor or the agent made them.
- **`/reload` and Agents › Reload agent files.** Read every agent, skill, instruction file and rule again on demand, and see what loaded ("Reloaded 2 agents, 1 skill and 4 instruction files and rules.") and what didn't, file by file.
- **The branch in the status bar opens Changes.** Click it to go straight to version control.

### What's fixed

- **Braces in your rules no longer break Gemini turns.** Text such as `/users/{user_id}` in `AGENTS.md`, a rule, a style or a note was taken for a placeholder and failed the turn with "state key does not exist". It now reaches the model exactly as written.

### Good to know

- Your settings and workspaces carry over: install the update over 0.6.x.
- Switching and merging take the same care as Pull: they're refused while a merge is waiting to be finished, they never ask for a password, and they won't run commands set in the repository's own `.git/config`.
- MCP servers and project settings still apply when the workspace is opened again. `/reload` covers agents, skills, instruction files and rules.
- Git is still named where you act on git itself, for example when it isn't installed.
