---
title: Blitz 0.8.10
weight: 1
---

**See your agents work together.** When Blitz hands a job to another agent (the coder for a code change, or the spec writer during `/setup`), your conversation now shows what that agent is doing as it works: the files it reads, the commands it runs and its own checklist. When it finishes, your checklist comes back, up to date.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.8.10).
2. Ask for a code change, or run `/setup` in a new folder, and watch each step as it happens.

### What's new

**Handoffs you can follow.** A handed-off agent's tool calls appear under its name while it works, and its checklist shows in place of yours until it returns. What it tells Blitz stays between the two of them, so your conversation reads as one. [Agents](../guide/agents.md#delegation-and-background-tasks)

**A setup checklist that keeps up.** `/setup` and `/spec` tick off each step as they go, with any model. Before, Claude made the list and then left it on the first step.

**Questions as forms.** The setup and the spec writer ask everything with the question form, with choices whenever there are likely answers, instead of asking in the middle of a reply.

### Good to know

- What a handed-off agent did is kept with your conversation, in `~/.blitz/sessions/<session>.subagents.jsonl`, and is deleted with it.
- This release also has 0.8.9's fix: with Claude, handing work to the coder no longer stops with "streaming is required for operations that may take longer than 10 minutes".
