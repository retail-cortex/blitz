---
title: "042 · Evals"
weight: 42
---

*Cases that check an agent still answers as it should: a prompt and the answer expected, run and scored* (`spec_evals_042`)

| | |
|---|---|
| Status | **Done** in v0.7.1 (the owner's request, 2026-10-17): EV-01–EV-10. |
| Source | `pkg/engine/evals/evals.go`, `pkg/engine/evals/runs.go`, `pkg/engine/evals.go`, `pkg/api/evals.go`, `apps/service/internal/server/evals.go`, `pkg/client/client.go` (`RunEval`), `apps/cli/eval.go`, `apps/desktop/web/src/studio/EvalsPane.tsx`, `apps/desktop/web/src/studio/evalForm.ts` |
| Tests | `pkg/engine/evals/evals_test.go`, `pkg/engine/evals_test.go`, `apps/service/internal/server/evals_test.go`, `pkg/client/remote_test.go` (`TestRemoteRunEval`), `apps/cli/eval_test.go`, `apps/desktop/web/src/studio/evalForm.test.ts` |
| Depends on | [spec_gates_041](spec_gates_041.md) (the judge), [spec_studio_040](spec_studio_040.md), [spec_search_035](spec_search_035.md) (embeddings) |

## 1. Purpose

An agent's instructions, skills and model change; whether it still answers as it should is worth checking, not hoping. An eval is a set of cases, each a prompt and the answer expected; Blitz runs each on the agents named, without changing anything, and scores the answers: exactly, or by meaning.

## 2. Requirements

- **EV-01** An eval is `.agents/evals/<name>.md`: front matter `description`, `agents` (each case runs on each; none: the active agent), `skills` (given to the agent with each prompt, as `\skill-name` gives them), `near` (`judge`, the default, or `embedding`) and `allow` (`network`, `mcp`), and a body whose first Markdown table holds the cases. Its columns are found by title, in any order: **ID**, **Name**, **Prompt**, **Output** (or Expected), **Output type** (or Type: `Exact` or `Near`, Near when empty), **Threshold** (0 to 1, or a percentage; 0.8 when a near case has none) and **Scope** (`Fail`, `Warn` or `Info`, Fail when empty). A cell's line break is `<br>` and a `|` is `\|`. The text before and after the table is kept as written (`evals.ParseTable`, `evals.Table`).
- **EV-02** What doesn't hold is the eval's problem, and it isn't run: a bad name, `near` or `allow` (cases never change files or run commands), a table without an ID, Prompt or Output column, and each case without an ID or a prompt, with another's ID, an unknown type or scope, or a near threshold outside (0, 1] (`evals.Check`).
- **EV-03** **Running** (`Workspace.RunEval`): each case (or those asked for), on each agent, one after another, in a session of its own (`session.OriginEval`) that's deleted once it's scored, unattended (`tools.Unattended`): file changes, deletions and commands are refused and listed with the result; web and MCP tools only when the eval allows them. Each case is held to 25 model calls and 5 minutes, and the workspace's guardrails apply. An agent that isn't defined is that result's error. A run is stopped by cancelling it; what was scored is kept.
- **EV-04** **Scoring**: an **exact** case passes when the answer is the expected text, trimmed, line endings and each line's trailing spaces aside (score 1 or 0). A **near** case's score (0 to 1) must reach its threshold: by the judge (the suggestions model, as rules are: how close the answer is in meaning, with a reason; `evals.JudgeNear`), or by the cosine similarity of the two texts' embeddings (`search.embedding_model`; an eval scored by embeddings without one fails to run, saying so). Secrets in the answer are masked before it's scored.
- **EV-05** A result is `pass`, or when it didn't pass its case's scope (`fail`, `warn`, `info`), or `error` (couldn't be run or scored). A run **fails** when a fail-scoped case fails or errors, and passes otherwise; it's `stopped` when cancelled.
- **EV-06** Finished runs are recorded in `~/.blitz/eval-runs/<workspace hash>-<eval>.jsonl` (never in the project), each with its results, cost and duration (`evals.RunLog`).
- **EV-07** `WorkspaceService.ListEvals` (each eval with its last run's status and counts), `SaveEval` (from the form, renaming when the name changes, keeping the front matter's other fields and the text around the table; problems with nothing written; `EVAL_NOT_FOUND` for an unknown previous name), `DeleteEval` (`EVAL_NOT_FOUND`), `RunEval` (streams the run as it starts, each result as it's scored, then the finished run; `EVAL_NOT_FOUND`, or `EVAL_NOT_RUNNABLE` for an eval with a problem or no case to run) and `ListEvalRuns` (newest first). The backend's `RunEval` does the same through the service (`pkg/client`).
- **EV-08** **Studio › Evals** (`EvalsPane`): the evals, each with its case count and last run, and ▶ to run it; **+** in the title bar starts one. An eval's form slides in over the list with **Run** (or **Stop** while it runs; an eval with unsaved changes or problems isn't run) and three tabs: **Cases**, each a row (its ID and name, how it's compared, its scope, its prompt's first line, and the latest result's mark) that opens into its editor (ID, name, prompt, expected answer, exact or near with the threshold, what a failure means), moved with ↑ ↓, removed, and **Add a case**; **Settings** (name, what it's for, agents, skills, how near answers are scored, whether cases may use the web or MCP tools); and **Runs**: the run going on (progress, each result as it's scored) and past runs, each result opening into the expected answer beside the agent's, the reason and what was refused. Back, Cancel, Save and Delete, unsaved changes asked about, as in the other kinds.
- **EV-09** The page's and the CLI's text is in every locale (`desktop.evals.*`, `eval.*`), counts in their plural forms.
- **EV-10** `blitz eval <name>` runs an eval for CI: each result on a line as it's scored (✓ ✗ ⚠ ℹ !, a near case's score against its threshold, why it didn't pass, what was refused), then a summary; `--case` runs some cases, `--json` prints the run. It exits 0 when the eval passes, 1 when it fails, 2 for an unknown eval or one that can't run, 130 when interrupted.

## 3. Known gaps

Cases run one after another, never in parallel. A case can't change files even in a scratch copy: a coding eval that needs to edit and test would run in a temporary worktree, which root turns can't use yet (only sub-agent tasks can). No AI draft of cases from a description yet.
