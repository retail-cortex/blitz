---
title: Blitz 0.9.4
weight: 1
---

**Edit your agents like documents.** Every long field in Studio (an agent's instructions, a skill's, a rule, a guardrail, a job's prompt, a workflow step's prompt) now opens in the visual editor: headings, lists, tables and code as they'll read, or the plain text with one click. And Studio has a new **Specs** section, where your project's specification is listed in order and edited in place.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.4).
2. Open **Studio › Agents** and pick an agent: its **System instructions** show rendered. Switch to **Source** at the field's top right for the text.
3. Open **Studio › Specs** to read and edit what `/spec` wrote. No specs yet? Run `/spec` in a chat, or press **+** to add one.

### What's new

**Long fields, edited as documents.** Agent instructions, skill instructions, rules, guardrails, job prompts and workflow steps open in the same visual editor as your Markdown files, with its toolbar. **Visual** or **Source** sits beside each field's name, and the choice carries to the next field you open. **Redraft** now sits under the box on the right, where you finish writing, and a workflow's agent steps have it too.

**Your specification in Studio.** **Studio › Specs** lists `.agents/specs`: the index, then each spec by number ("001 · Overview"). Open one to edit it, with **Redraft** to have the model rewrite it from what you ask. **+** adds the next numbered spec from a title. Saving checks that nobody changed the file since you opened it.

### Fixes

- Python projects in subfolders get their own language server even when they're known only by a `requirements.txt`, a `Pipfile` or a `venv` folder. Before, their files were served from the workspace's root, and imports of the project's own modules showed as errors.
- The Python language server now uses the project's virtual environment (`.venv` or `venv`, in the project or a folder above it), so the packages you installed there are found. A `pyrightconfig.json` that names its own environment still wins.
