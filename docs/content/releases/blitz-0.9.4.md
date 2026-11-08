---
title: Blitz 0.9.4
weight: 4
---

**Make pictures in their own place, and edit your agents like documents.** Every long field in Studio (an agent's instructions, a skill's, a rule, a guardrail, a job's prompt, a workflow step's prompt) now opens in the visual editor: headings, lists, tables and code as they'll read, or the plain text with one click. And Studio has a new **Specs** section, where your project's specification is listed in order and edited in place.

### Get started

1. Update Blitz (it updates itself) or download it from the [GitHub release](https://github.com/retail-cortex/blitz/releases/tag/v0.9.4).
2. Open **Studio › Agents** and pick an agent: its **System instructions** show rendered. Switch to **Source** at the field's top right for the text.
3. Click the brush in the chat box to open **Pictures** (a Gemini key, or your organization's image models, makes pictures).
4. Open **Studio › Workflows**, drag **Data** onto the canvas, choose one of your databases and press **Try it**.
5. Open **Studio › Specs** to read, check and refine what `/spec` wrote. No specs yet? Run `/spec` in a chat, or press **+** to add one. In CI, run `blitz spec review`.

### What's new

**Pictures, a place to make them.** The new brush in the chat box opens **Pictures**: choose Nano Banana 2.1, 3.1 Flash Image, 3 Pro Image or Imagen, write what you want, and drop in pictures to start from or match, each marked as a character to keep, an object to keep faithful or a style to borrow. Pick the shape from a grid (panoramic 4:1 and 8:1 too), the size up to 4K, how hard the model thinks, and whether it searches the web first; Imagen makes up to four at once, with what to avoid and a seed. Every control follows what the chosen model can do, and each turn shows its cost before you make it. Then talk to it: "make the sky darker" changes the last picture, **Start from this** picks any other, and **Save** puts the one you like into your workspace's `pictures/` folder, ready to open, mention in a chat or use again. Start from the file tree too: **Edit in Pictures** on any picture.

**Workflows that start from your data.** The new **Data** step runs a query on one of your databases and hands the next step a table: every column named, typed and described from your schema, every row in order. Pick one of the connection's saved queries or write the SQL, or describe what you want and let Blitz draft it from the schema for you to review. Feed it the run's input or an earlier step's answer through its parameters (never into the SQL itself, so nothing can rewrite your query), and press **Try it** to see the first rows before you save. An agent step after it reads the table as Markdown; conditions read it as data (`output.data.total > 0`). Big results are cut at 1,000 rows unless you say otherwise, and the step tells you so, or fails if you'd rather it did. A query that writes needs **Allow writes** and asks you first, and a BigQuery query is priced before it runs and stops if it would cost more than $1, or the most you set.

**Bring your data into a chat.** The database icon in the chat box (or `/data`) now lists your databases and their saved queries. Add a database and the agent gets its tables and columns with your message, ready to query as it needs. Pick a saved query, write SQL, or describe what you're after, and the result is added as a chip: the agent reads the SQL, the columns with their descriptions and up to 200 rows. Click the chip to see the table. Or just type `@d`: the chat box offers your databases, then their saved queries, as you type.

**Choices you don't need to look up.** When the agent asks you to pick between technical options (three frameworks, two databases), each choice now explains itself: hover it for a one-line summary, open its details for what picking it means for you, in plain words. The choices come in recommended order, the first marked **Recommended**, with the reason for the order stated (easiest to maintain with AI, the biggest community, what your project's decisions require). The terminal shows the same.

**Long fields, edited as documents.** Agent instructions, skill instructions, rules, guardrails, job prompts and workflow steps open in the same visual editor as your Markdown files, with its toolbar. **Visual** or **Source** sits beside each field's name, and the choice carries to the next field you open. **Redraft** now sits under the box on the right, where you finish writing, and a workflow's agent steps have it too.

**Your specification in Studio, checked as you write it.** **Studio › Specs** lists `.agents/specs` in order ("001 · Overview"), each with its status and a warning count. Open one and Blitz checks it as you type: requirements without IDs, IDs used twice, a spec that depends on one that doesn't exist or comes later, a loop in the order, words like "fast" or "secure" that can't be tested, a missing "Done when". **Review with AI** adds what only a reader catches (what can't be checked, what reads two ways, contradictions with your other specs, missing cases) and tells you what it cost. **Refine with AI** is a short conversation about what should change; then you see each change and keep or drop it. **+** adds the next numbered spec, ready to fill in.

**Know what's done.** Approve a spec and Blitz remembers each requirement as you approved it, in `.agents/.ledger` beside your work. **Verify** finds every requirement's ID in your code, tests and commit messages, for free, and marks it traced or untraced with the file and line. **Verify with AI** goes further: the model reads that evidence and says whether each requirement is implemented, partial, missing or contradicted, and why, after showing you what it will cost. Change an approved spec and the changed requirements are flagged until you approve and verify again; each change is kept as a diff. **Show every requirement** puts the whole specification in one table.

**Change a spec, see what it breaks.** Save a change to an approved spec and Blitz marks it changed, keeps the diff, and offers to scan the impact. The scan is free: it follows each changed requirement from where it was implemented to the function that holds it and every place that uses it, through your language server. **Write the fix with AI** (after showing its cost) turns that into steps, tests first, and points out other specs the change now contradicts. Copy the steps, or take them straight to a chat.

**From spec to done.** **Implement** turns what's left of a spec into a prompt for the agent: in order, tests first, and every requirement's ID cited in the code, the tests and the commit, so Verify finds the work. The list shows which spec is next up and which wait for others. And if a chat changes code that an approved requirement depends on, Blitz says so at the end of the turn.

**Specs in CI.** `blitz spec list` prints the specification in order, and `blitz spec review` checks every spec and fails when one has an error, so a pull request can't merge a spec that contradicts itself. `blitz spec approve`, `verify`, `impact` and `implement` do the rest from the terminal, and `blitz spec verify --ci` fails a pull request when approved requirements drift; `--judge` adds the model, and prints the cost before it spends anything.

### Fixes

- In auto mode, what you choose in the agent's questions now counts as what you asked for. Before, ticking options and pressing Send could still get the next change blocked, because the reviewer only read the messages you typed. Your answers to a plan review count too.

- Python projects in subfolders get their own language server even when they're known only by a `requirements.txt`, a `Pipfile` or a `venv` folder. Before, their files were served from the workspace's root, and imports of the project's own modules showed as errors.
- The Python language server now uses the project's virtual environment (`.venv` or `venv`, in the project or a folder above it), so the packages you installed there are found. A `pyrightconfig.json` that names its own environment still wins.
