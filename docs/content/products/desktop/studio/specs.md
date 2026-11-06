---
title: Specs
weight: 15
---

**Studio › Specs** shows the workspace's numbered specification stages (`.agents/specs/NNN-<name>.md`) on a dependency canvas: what's done, what's in progress, what depends on what, which requirements and acceptance criteria are checked off, which open questions still wait for a decision, and which workspace skills and agents serve each stage.

## Open it

Choose **Studio › Specs**. Studio's menu shows once **Show advanced** is on, in the Settings title bar.

The canvas takes the room: Studio's list of areas shrinks to icons. Select a stage node to open its inspector on the right; **»** folds the inspector away when you want the whole canvas, and its left edge drags wider or narrower.

## Add or wire stages on the canvas

1. Choose **+ New stage** to add the next numbered stage in `.agents/specs/` (and list it in `.agents/specs/README.md`), or **+ Write specs** to run `/spec` in the chat so `project-spec` interviews you and drafts the whole specification.
2. In the **New stage** dialog, give the stage a title, choose any stages it depends on, and either choose **Create** or **Draft with AI** to write the initial stage from a short summary.
3. Drag from an earlier stage's right edge to a later stage's left edge (or use **Depends on** in the inspector) to link or unlink a dependency. Self-dependencies and dependency cycles are refused.

## Track progress, answer questions and build

In the stage inspector:

- **Status** (`Draft`, `In progress`, `Done`) and **Depends on** update the stage's Markdown header in place.
- **Redraft** (`Draft with AI`) drafts or rewrites the stage's Markdown from a short instruction and stages it as an unsaved draft in the inspector (with **Source** / **Preview**, **Revert** and **Save**) so nothing is written to disk until you save.
- **Requirements** and **Done when** are persistent checkboxes (`- [ ]` / `- [x]`) in `.agents/specs/NNN-<name>.md`, with **+ Add requirement** (auto-numbered `<PREFIX>-NN`) and **+ Add criterion** inputs. Each stage node on the canvas shows its live `done/total` checklist progress.
- **Open questions** has an answer box and a **×** dismiss button beside each question. Type your decisions and choose **Apply answers** to fold them into an unsaved stage draft you can review and **Save**, or **Resolve** to open `/spec` in the chat with your answers pre-filled. Unsaved answers and drafts stay in place as you move between stages or Studio tabs.
- **▶** beside a requirement builds that one requirement, **Build stage** / **Build N remaining** builds the unchecked requirements in the chat, **Review stage** runs `/review` against the stage's criteria, and **Open in editor** opens `.agents/specs/NNN-<name>.md` in Files.
- When a spec changed after your skills or agents were written, a banner at the top offers **Update skills & agents** (`/skills update`).
