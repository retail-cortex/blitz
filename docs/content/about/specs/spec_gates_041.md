---
title: "041 · Gates"
weight: 41
---

*Rules that gate an agent's turn: checked before it runs and after it answers* (`spec_gates_041`)

| | |
|---|---|
| Status | **In progress** since v0.7.1 (the owner's request, 2026-10-17). Done: rules (GT-01–GT-10). Next: guardrails, which share the judge. |
| Source | `pkg/engine/gates/gates.go`, `pkg/engine/frontmatter/frontmatter.go`, `pkg/engine/rules.go`, `pkg/engine/turn.go`, `pkg/engine/memory/memory.go` (`splitRule`), `pkg/engine/tools/checkpoint.go` (`LatestTurnDiff`), `apps/service/internal/server/rules.go`, `apps/desktop/web/src/studio/RulesPane.tsx`, `apps/desktop/web/src/studio/ruleForm.ts` |
| Tests | `pkg/engine/gates/gates_test.go`, `pkg/engine/frontmatter/frontmatter_test.go`, `pkg/engine/rules_test.go`, `pkg/engine/memory/memory_more_test.go` (`TestGateRulesAreNotInstructions`), `apps/service/internal/server/rules_test.go`, `apps/desktop/web/src/studio/ruleForm.test.ts` |
| Depends on | [spec_memory_012](spec_memory_012.md) (rule files), [spec_studio_040](spec_studio_040.md) |

## 1. Purpose

A team's expectations of its agents ("never touch production", "say how the change was tested") are more dependable checked than hoped for. A rule says one such thing in plain English; a fast model judges it on the prompt before the agent runs, or on the answer and the turn's changes before the turn ends; and the rule says what a failure does.

## 2. Requirements

- **GT-01** A rule file (`*.md` in the rule folders, `.blitz/rules`, `.agents/rules`, `.claude/rules` and the global one; MEM-07) with front matter `when: before_agent` or `when: after_agent` is a **gate**: not an instruction (the memory loader skips it), judged around the turn. Other fields: `action` (`block`, `warn` or `revise`; `warn` when unset), `order` (gates run by order, then name), `agents` (the agents whose turns it gates; none means every agent), `description`. Its body is the rule. A rule without `when` is an instruction rule, unchanged (MEM-07). An unknown `when` or `action`, or an empty gate, is the rule's problem, and it's skipped (`gates.Parse`).
- **GT-02** The judge (`gates.Judge`) asks the suggestions model (else auto mode's, else the workspace's) whether the subject passes the one rule, with a brief to fail only when the rule is clearly broken (a rule that doesn't apply passes), and reads its JSON verdict (`pass` or `fail`, a reason saying what would make it pass). Secrets in the subject are redacted first.
- **GT-03** **Before the agent**: once the prompt is accepted (after the `prompt_submit` hooks), each before-agent gate offered to the active agent judges it, in order. A failing **block** rule refuses the prompt (`*api.BlockedError`, "The rule … stopped this prompt: …"), nothing recorded, the agent not run; **warn** says so in the conversation once the prompt is recorded; **revise** gives the agent the reason with the prompt (`<rules-hook-context>`). Side questions aren't judged.
- **GT-04** **After the agent**: once the turn's passes are done (an approved plan carried out, `stop` hooks and a goal), each after-agent gate offered to the active agent judges the answer (12 KiB) and the diff of the files the latest pass changed (`Checkpoints.LatestTurnDiff`, 12 KiB), in order. A failing **revise** rule sends the agent back with the reasons ("(rules) …" in the transcript, `session.KindRule`), and the rules judge again, twice at most; then "Rules still fail after 2 revisions". **block** marks the answer as failing the rule (an error notice); **warn** a notice. A plan that wasn't carried out isn't judged.
- **GT-05** A rule the judge can't judge (no model, an answer that isn't a verdict) says so ("The rule … couldn't be checked") and lets the turn go on.
- **GT-06** Every verdict goes to the audit log (`audit.KindRule`: the rule, pass, warn, revise, block or unjudged, and the reason).
- **GT-07** `WorkspaceService.ListRules` returns the workspace's rules: gates in the order they run, then instruction rules; each with whether it's editable (in `.agents/rules`). `SaveRule` writes `.agents/rules/<name>.md` from the form, renaming when the name changes, keeping the front matter's other fields (`frontmatter.Edit`); an instruction rule keeps no gate fields, a gate no `paths`; problems (a bad name, when or action, an empty rule, a taken name) come back with nothing written. `DeleteRule` (`RULE_NOT_FOUND`), `ReorderRules` (orders 10, 20, 30… in the order given) and `TryRule` (a sample prompt or answer judged against the rule as written; a judge that fails is a `problem`).
- **GT-08** **Studio › Rules** (`RulesPane`): three lists, **Before the agent** and **After the agent** (each in the order its rules run, numbered, with the action and the agents; dragged, or moved with ↑ ↓, to reorder) and **Instructions** (always, or for the files named); a rule elsewhere than `.agents/rules` is listed with its path and opens read-only. A rule's form slides in over the list as an agent's does: name, what it's for, when (before, after, or an instruction), the action with what it does then, the agents it applies to (or an instruction's files), the rule with **Redraft** (`DraftManifest` kind `rule`), and for a gate **Try it** (a sample, **Check**, the verdict). Back, Cancel and Save; unsaved changes are asked about.
- **GT-09** Each list row is one button that opens the rule beside its own buttons (↑ ↓), never a button inside a button, so each control has its own name.
- **GT-10** `pkg/engine/frontmatter` edits Markdown front matter for Studio's forms (rules, skills): fields set, or removed when empty, every other field kept in its order.

## 3. Known gaps

Guardrails (input, tool-result, output and action checks, built in or the user's own) come next and use the same judge.
