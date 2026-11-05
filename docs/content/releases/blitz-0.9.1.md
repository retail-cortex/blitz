---
title: Blitz 0.9.1
weight: 1
---

**Safer by default, and steadier under load.** We went through every file in Blitz, checked each feature against its spec and its docs, and fixed what we found: the limits and safety checks now hold in the corner cases, settings tell you when they're misspelt or misplaced instead of quietly ignoring them, and a long list of rough edges in the CLI and the desktop app are smoothed out. Language servers now work in monorepos, too.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.1).
2. That's it: everything below applies as soon as it's running.

### What's new

**Long coding runs know how to keep going.** When a turn reaches its limit of model calls, Blitz now tells you which setting set it, how to allow more, and that you can send "continue" to carry on where it stopped. The limit now counts the whole turn, plans, goals and follow-ups included, as it always meant to.

**Language servers in monorepos.** Code intelligence finds each project's own root: a `frontend/` with its own `node_modules`, a Go module in a subfolder, a Python package with its own `pyproject.toml`. TypeScript is found in the project, a folder above it, or a global install.

**Mistakes in your settings show up.** A misspelt key in an agent, command, workflow, guardrail, job or settings file is now reported where you'll see it, instead of being dropped without a word. A built-in guardrail with a broken file keeps protecting you with its defaults until you fix it.

**Stronger guardrails and sandbox.**
- Sandboxed commands can no longer reach Blitz's own service.
- A shell command that's hard to read is checked against your deny rules anyway, and never runs unasked.
- Background agents keep the plan or read-only mode they were started in.
- Secrets that stream in pieces are masked whole.
- The in-app browser checks the address it actually connects to.

**For organizations.** Your organization's locked guardrails, agents and skills are enforced as locked, and its MCP policy covers plugins and trusted projects too. Sharing a folder checks its whole history for secrets first, and signing in and syncing are more dependable.

**The CLI and the desktop app.**
- `blitz service restart` is new.
- `blitz --bg` says which options it can't carry instead of dropping them.
- `/loop` keeps to its one-minute minimum.
- Workflow tasks stay put instead of vanishing and coming back.
- Editing an MCP server keeps its tool list.
- The temperature slider no longer saves a value when you only click it.

### Good to know

- Short answers (under a couple of lines) now appear once they're complete, rather than streaming in: Blitz checks them for secrets as a whole.
- Blitz now reads its settings from `.env.toml` alone; `.env.local.toml` and `.env.<runtime>.toml` beside it are no longer merged in.
- Agent, command, eval and workflow files with a key Blitz doesn't know now show a problem until the key is fixed or removed.
- With an organization set but its policy not fetched yet (before your first sign-in), Blitz applies the strictest policy until it has it.
