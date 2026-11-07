---
title: Blitz 0.9.4
weight: 1
---

**Edit your agents like documents.** Every long field in Studio (an agent's instructions, a skill's, a rule, a guardrail, a job's prompt, a workflow step's prompt) now opens in the visual editor: headings, lists, tables and code as they'll read, or the plain text with one click. And Studio has a new **Specs** section, where your project's specification is listed in order and edited in place.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.4).
2. Open **Studio › Agents** and pick an agent: its **System instructions** show rendered. Switch to **Source** at the field's top right for the text.
3. Open **Studio › Specs** to read, check and refine what `/spec` wrote. No specs yet? Run `/spec` in a chat, or press **+** to add one. In CI, run `blitz spec review`.

### What's new

**Long fields, edited as documents.** Agent instructions, skill instructions, rules, guardrails, job prompts and workflow steps open in the same visual editor as your Markdown files, with its toolbar. **Visual** or **Source** sits beside each field's name, and the choice carries to the next field you open. **Redraft** now sits under the box on the right, where you finish writing, and a workflow's agent steps have it too.

**Your specification in Studio, checked as you write it.** **Studio › Specs** lists `.agents/specs` in order ("001 · Overview"), each with its status and a warning count. Open one and Blitz checks it as you type: requirements without IDs, IDs used twice, a spec that depends on one that doesn't exist or comes later, a loop in the order, words like "fast" or "secure" that can't be tested, a missing "Done when". **Review with AI** adds what only a reader catches (what can't be checked, what reads two ways, contradictions with your other specs, missing cases) and tells you what it cost. **Refine with AI** is a short conversation about what should change; then you see each change and keep or drop it. **+** adds the next numbered spec, ready to fill in.

**Know what's done.** Approve a spec and Blitz remembers each requirement as you approved it, in `.agents/.ledger` beside your work. **Verify** finds every requirement's ID in your code, tests and commit messages, for free, and marks it traced or untraced with the file and line. **Verify with AI** goes further: the model reads that evidence and says whether each requirement is implemented, partial, missing or contradicted, and why, after showing you what it will cost. Change an approved spec and the changed requirements are flagged until you approve and verify again; each change is kept as a diff. **Show every requirement** puts the whole specification in one table.

**Change a spec, see what it breaks.** Save a change to an approved spec and Blitz marks it changed, keeps the diff, and offers to scan the impact. The scan is free: it follows each changed requirement from where it was implemented to the function that holds it and every place that uses it, through your language server. **Write the fix with AI** (after showing its cost) turns that into steps, tests first, and points out other specs the change now contradicts. Copy the steps, or take them straight to a chat.

**From spec to done.** **Implement** turns what's left of a spec into a prompt for the agent: in order, tests first, and every requirement's ID cited in the code, the tests and the commit, so Verify finds the work. The list shows which spec is next up and which wait for others. And if a chat changes code that an approved requirement depends on, Blitz says so at the end of the turn.

**Specs in CI.** `blitz spec list` prints the specification in order, and `blitz spec review` checks every spec and fails when one has an error, so a pull request can't merge a spec that contradicts itself. `blitz spec approve`, `verify`, `impact` and `implement` do the rest from the terminal, and `blitz spec verify --ci` fails a pull request when approved requirements drift; `--judge` adds the model, and prints the cost before it spends anything.

### Fixes

- Python projects in subfolders get their own language server even when they're known only by a `requirements.txt`, a `Pipfile` or a `venv` folder. Before, their files were served from the workspace's root, and imports of the project's own modules showed as errors.
- The Python language server now uses the project's virtual environment (`.venv` or `venv`, in the project or a folder above it), so the packages you installed there are found. A `pyrightconfig.json` that names its own environment still wins.
