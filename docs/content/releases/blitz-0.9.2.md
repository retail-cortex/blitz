---
title: Blitz 0.9.2
weight: 1
---

**Your project's specification on a dependency canvas.** **Studio › Specs** turns `.agents/specs/NNN-<name>.md` into an interactive stage graph and workbench: see what depends on what, check off requirements and acceptance criteria as they're built, answer open questions in place or with `/spec`, and launch focused build or review turns directly from any stage.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.2).
2. Open a workspace in the desktop app and choose **Studio › Specs** (or run `/spec` in the chat to write a specification first).

### What's new

**The stage dependency graph.** Every numbered specification stage in `.agents/specs/` appears on a layered canvas in execution order, joined by its `Depends on:` links. Each node shows the stage's status (`Draft`, `In progress`, `Done`), live checklist progress, requirement and open-question counts, and the workspace skills and agents that cite it. Drag between nodes (or use **Depends on** in the inspector) to link or unlink stages; self-dependencies and loops are refused.

**An interactive stage workbench.** Selecting a stage opens its inspector beside the canvas:
- Check off requirements and **Done when** criteria (`- [ ]` / `- [x]`), or add new ones (`+`) with automatic `<PREFIX>-NN` numbering, while keeping your prose, formatting and notes intact.
- Type decisions under **Open questions** and choose **Apply answers** to fold them into the stage's sections as an unsaved draft you can preview or edit as Markdown before saving, or choose **Resolve** to send them to `/spec` in the chat.
- Choose **Redraft** (or **Draft** in **+ New stage**) to have the suggestions model draft or revise a stage, then inspect or edit the draft before saving.
- Choose **▶** on a single requirement, **Build stage** for the remaining unchecked items, or **Review stage** to run `/review` against the stage.
- When a specification changes after your skills or agents were written, a banner offers **Update skills & agents** (`/skills update`).
