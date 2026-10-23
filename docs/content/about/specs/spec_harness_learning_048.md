---
title: "048 · Harness learning"
weight: 48
---

*A harness that learns from its runs: experience drawn from what happened, run evidence in `/skills update` and `/agents update`, invariant and project experience kept apart, and setup that covers guardrails and rules* (`spec_harness_learning_048`)

| | |
|---|---|
| Status | **Built** (2026-10-22, the owner's request): §8's steps 1 to 6, in one branch. The owner chose to go on without the step 2 checkpoint (the app is 0.x); the owner checked it on a real workspace (Recipe book) before the merge; the open questions (§9) are measured as it is used. Phase E (§7) waits for its decision. Written after comparing `/setup`, `/init` and `/spec` with Google's Harness² framework (§1). |
| Source | Today's harness: `pkg/engine/agents/builtin/project-setup.md`, `pkg/engine/agents/builtin/project-spec.md`, `pkg/engine/agents/builtin/harness-update.md`, `pkg/engine/commands/builtin/setup.md`, `pkg/engine/commands/builtin/spec.md`, `pkg/engine/commands/builtin/skills-update.md`, `pkg/engine/commands/builtin/agents-update.md`, `HarnessStale` in `pkg/engine/suggestions.go`; the evidence it would read: `pkg/engine/health/facts.go`. The evidence: `pkg/engine/harness/evidence.go`, `pkg/engine/harness/corrections.go`, `pkg/engine/harness.go` (`HarnessEvidence`), the entries it reads in `pkg/engine/audit/audit.go`, `pkg/engine/turn.go` and `pkg/engine/runtime/engine.go`. The reflector: `pkg/engine/tools/harness_evidence.go`, `pkg/engine/agents/builtin/harness-reflect.md`, `pkg/engine/commands/builtin/reflect.md`, `isReflect` and the reflection's state in `pkg/engine/harness.go`. The tile and the schedule: `Reflection`, `ReflectDue` and `ScheduledReflect` in `pkg/engine/harness.go`, `audit.Stamp`, `pkg/engine/tools/harness_propose.go`, `api.Reflection` in `pkg/api/suggestions.go`, `GetReflection` and `reflectDue` in `apps/service/internal/server/agents.go` and `apps/service/internal/server/health.go`, `apps/desktop/web/src/suggestions.ts`, the Overview's settings in `apps/desktop/web/src/studio/OverviewPane.tsx`, `reflectHint` in `apps/cli/internal/tui/repl.go`, `[harness]` in `pkg/config/features.go`. Phase B: `pkg/engine/agents/builtin/harness-update.md`, `harnessFailing` in `pkg/engine/harness.go`. Phases C and D: `pkg/engine/agents/builtin/project-setup.md`, `pkg/engine/commands/builtin/rules-update.md`, `globalExperienceText` and `saveGlobalExperience` in `pkg/engine/harness.go` |
| Tests | `pkg/engine/harness/evidence_test.go`, `pkg/engine/harness_test.go`, `pkg/engine/audit/audit_test.go` (`TestRead`), `pkg/engine/runtime/features_test.go` (`TestEngineAuditsWhatFailed`, `TestEngineSaysWhenAFailureRepeats`), `pkg/engine/tools/harness_evidence_test.go`, `pkg/engine/agents/frontmatter_test.go` and `pkg/engine/commands/commands_test.go` (the agent and the command), `pkg/engine/harness_test.go` (`TestReflectionTile`, `TestReflectDue`, `TestScheduledReflect`, `TestScheduledReflectSkips`), `pkg/api/suggestions_test.go`, `pkg/engine/audit/audit_test.go` (`TestStamp`), `pkg/engine/tools/harness_evidence_test.go` (`TestHarnessProposeTool`), `pkg/config/providers_test.go` (`TestSetScheduleValues`), `apps/cli/internal/tui/init_test.go` (`TestReflectHint`), `pkg/engine/harness_test.go` (`TestHarnessFailing`, `TestGlobalExperience`, `TestAddToSection`), `pkg/engine/tools/harness_evidence_test.go` (`TestSaveGlobalExperienceTool`), `pkg/engine/audit/audit_test.go` (`TestLogContextNamesTheAgent`), `apps/desktop/web/src/suggestions.test.ts` |
| Depends on | [spec_memory_012](spec_memory_012.md) (MEM-06 imports, MEM-22 `/setup`), [spec_sessions_017](spec_sessions_017.md) (transcripts), [spec_observability_003](spec_observability_003.md) (the audit log), [spec_gates_041](spec_gates_041.md) (rules and guardrails), [spec_evals_042](spec_evals_042.md) (evals), [spec_health_044](spec_health_044.md) (facts and verdicts), [spec_background_agents_032](spec_background_agents_032.md) (jobs) |

## 1. Purpose

`/setup` (and `/init`, `blitz init`) and `/spec` build a workspace's harness from what the user says and what the files show: a specification in `.agents/specs/`, then `.agents/AGENTS.md`, skills and agents ([spec_memory_012](spec_memory_012.md) MEM-22). `/skills update` and `/agents update` (the `harness-update` agent) revisit it when the specification changes. The harness never learns from what happens when agents use it. Nothing turns a failed command, a user's correction or an approval denied three times into a better instruction, and nothing checks that a skill or an instruction helped.

Google Cloud AI Research's **Harness²** ("recursive agent harnessing", v0.1.0, Apache-2.0; [MarkTechPost, 2026-10-18](https://www.marktechpost.com/2026/10/18/google-harness2-recursive-agent-harnessing-framework/)) improves the same thing, the harness and not the model, the other way round:

- It edits eight parts, each a file: system prompt, guardrails, memory, tool guidance, subagents, skills, scripts, plugins.
- **Per task:** run with the base harness; an editor proposes an *invariant* candidate (general failures: planning, tool use) and a *domain* candidate (the field's knowledge and procedures); both run; a composer keeps only the edits their trajectories support. No labels or verifier.
- **Across tasks:** a reflector keeps the adopted harness and adds lessons to `EXPERIENCE.md`, which guide later proposals. Each task still starts from the base harness.
- Reported gains: one step (k=1) improved every executor on WorkBuddy-Bench, JobBench and Harvey's LAB, the weaker executors most (GLM-5 under OpenCode, WorkBuddy 64.2 → 75.7), often with fewer steps. The cost is extra runs on every task.

How the two compare:

| | Blitz today | Harness² |
|---|---|---|
| Evidence | The user's answers, the files | The agent's behaviour on the task |
| Human in the loop | Interview, the plan of specs, picking skills and agents | None |
| When | Once, and again when the specification changes | Every task |
| Lifetime | Files in the repository, shared with Claude Code, Gemini CLI and Codex | A harness per task, and a library of lessons |
| Target | The specification (`Done when`, numbered IDs) | None: candidates against each other |
| Parts written | Instructions, skills, agents, `.gitignore` | All eight |
| Learning across tasks | Only by hand (`/memory add`) | `EXPERIENCE.md` and the library |
| Cost | One interview | k extra runs and compositions per task |

This spec takes Harness²'s ideas that fit Blitz, and keeps what Blitz does well: the harness is the user's, in their repository, and **nothing changes without their choosing it**. Everything below proposes, cites evidence and waits for a pick, as `harness-update` does already.

The comparison is from the article, not the paper or its code; check both before phase E (§7).

## 2. Goals and non-goals

**Goals**

- Turn what happens in a workspace's sessions into proposed changes to its harness, each citing the runs it comes from.
- Keep that experience in a file the user owns, reads and commits, loaded like the instructions.
- Give `/skills update` and `/agents update` the same evidence, so they compare the harness with what happened as well as with the specification.
- Keep general working experience apart from this project's knowledge.
- Have `/setup` cover the parts of the harness Blitz supports and it doesn't write yet: guardrails and path-scoped rules.

**Non-goals**

- Rewriting the harness per task, or without the user's pick.
- Training or fine-tuning any model.
- Reading anything outside the workspace and its own sessions.
- Replacing the health judge ([spec_health_044](spec_health_044.md)): this spec uses its facts and verdicts, it doesn't grade.

## 3. Phase A: experience and the reflector

- **HE-01** **The experience file** is `.agents/EXPERIENCE.md`, Harness²'s name: what agents found in this workspace, not rules handed down (the instructions stay in `.agents/AGENTS.md`). `/setup` adds the line `@EXPERIENCE.md` to `.agents/AGENTS.md` (an import, MEM-06), so every agent reads it, in Blitz and in the tools that follow imports. It is Markdown under two headings, `## How to work` and `## This project` (HE-20), one entry per bullet: the entry in one sentence, then its evidence in brackets (dates and session titles, at most three). It holds at most 60 entries; past that, the reflector proposes merging or retiring the oldest before adding one. A missing file is created on the first entry chosen; an existing `.agents/AGENTS.md` without the import gets it then, after asking.
- **HE-02** **Evidence** (`harness.Build`, from `Workspace.HarnessEvidence`), no model call, from the workspace's audit log ([spec_observability_003](spec_observability_003.md)) since a date (default: the last reflection, else 14 days). The audit log is the one source: it's dated, kept per workspace and redacted as it's written (`SecretRedactor`), so no transcript is read. Each item is one thing that happened, grouped by kind, subject and target, with a count, the first and last time, and its latest three occurrences (the session's title, whether it was a job's or a detached run, the turn, the time and an excerpt):
  - tool calls that failed, by tool and by command or path, and commands that exited nonzero, with the end of their output;
  - actions denied by the user, the auto reviewer or a permission rule, and guardrail blocks;
  - rules that sent work back (revise or block) ([spec_gates_041](spec_gates_041.md));
  - user corrections: a steer while an agent works, a stopped turn, `/undo` or a rewind, and a later prompt that rejects the last answer (`harness.IsCorrection`: a short list of how such prompts start and phrases they use, in en-US, es and fr-CA, not a model call);
  - turns that reached a limit (steps, cost, time);
  - skills activated and agents invoked, counted per turn, and an item for each one whose turns went wrong (any of the above in the same turn);
  - the health judge's latest fixes for the skills, agents, rules and guardrails involved (HL-10).
  Evals' cases and snapshots are left out. A pattern (HE-03 step 1) is an item seen at least twice, or once with a cost: an undo, a limit, an action the user refused. Items are capped at 200, patterns first, then the most frequent and most recent. With the audit log off (`[audit] enabled = false`) there's no evidence, and it says so.

  A refused action is counted once, as a denial, not again as its tool's failure; a tool that failed because its turn was stopped isn't counted (the stop is). Entries that name neither a session nor a workspace (written before 2026-10-22) are skipped: whose they were isn't known.

  Every entry of a turn names its session and the workspace: the session travels in the turn's context (`audit.WithSession`, `Logger.LogContext`) to the tools, approvals, rules and guardrails, and each workspace's logger names its workspace from when it opens. Before, only prompts named their session, and entries named the workspace only once a session had been opened in the process (the service opens them otherwise), so most named neither.

  What the audit log records for it: a failed tool call's result entry carries the call's arguments, and a command that exited nonzero is recorded as `exit` with its code and the last 300 bytes of its output (a call that worked repeats nothing); a steer's prompt entry has the decision `steer`; a turn that was stopped or reached a limit gets a `turn_end` entry (`stopped`, or `limit` with steps, cost or time), in chats, detached runs and jobs alike. A finished turn, or one that failed otherwise, records nothing more.
- **HE-03** **The reflector**, a built-in agent `harness-reflect` (tools: `harness_evidence`, `read_file`, `list_files`, `glob`, `grep`, `create_file`, `replace_in_file`, `ask_user_question`, `todo`, `list_or_search_skills`, `list_agents`). Given the evidence, `.agents/AGENTS.md`, `.agents/EXPERIENCE.md`, the specs' README and the skills' and agents' descriptions, it:
  1. groups the evidence into patterns: something that happened at least twice, or once with a cost (a revert, a denied destructive command);
  2. for each pattern, picks the smallest change that would have prevented it: an entry; a line in `.agents/AGENTS.md` (a command, a boundary, a gotcha); a step in a skill; a change to an agent's tools or instructions; a rule or guardrail (HE-30);
  3. drops what the harness already says, and says so when a pattern happened in spite of an instruction (the instruction may be unclear: propose rewording it, not repeating it);
  4. offers at most five changes in one `multi_select` question, each "add|update|retire <where>: <the change> — <evidence>", the most frequent first;
  5. writes only what was chosen, reading each file first, keeping what's still true; retiring an entry removes its bullet;
  6. finishes with what changed and what it left out, and why.
  It never invents an entry the evidence doesn't show, and writes only inside `.agents/`. With no patterns, it says so and stops. Without `.agents/AGENTS.md` it suggests /setup and stops: the experience belongs to a harness.

  It reads the evidence with **`harness_evidence(since?, about?)`**, a read-only tool (allowed in plan mode) that only agents naming it get: `since` is a date or a number of days (empty: since the last reflection, else 14 days), `about` narrows it to `skills`, `agents` or `rules` (with guardrails) for HE-10 and HE-31. It returns the evidence as text: one line per item (`[pattern]`, kind, subject, target, count, first and last day), its occurrences (time, session title, `[worker run]`, turn, excerpt) and the health judge's fixes, then how many turns each skill and agent was in.
- **HE-04** **`/reflect [since]`** runs the reflector (`agent: harness-reflect`) on the evidence since a date or a number of days. It is a bundled command, so the CLI, the desktop app's chat and the VS Code view offer it like `/skills update`. After a `/reflect` turn the engine reloads memory, as after `/setup` (MEM-22), and, when the turn finished, keeps the time it began as the last reflection (`harness/<workspace>.json` in the service's storage), where the next one's evidence starts.
- **HE-05** **A welcome tile**, like `HarnessStale` (MEM-24): when the evidence since the last reflection has at least five patterned items (`api.ReflectAt`; HE-03 step 1's rule, counted without a model by `Workspace.Reflection`, and counted again only when the audit log's files changed, `audit.Stamp`), the welcome screen shows **Agents hit the same problems: review what to learn** (`SUGGESTION_KIND_REFLECT`, the count in its detail) and sends `/reflect`. Only in a workspace with `.agents/AGENTS.md` and the audit log on. The terminal says the same under its banner (`reflectHint`; the CLI asks through `GetReflection`). The last reflection's time is kept in the service's storage per workspace (`harness/<id>.json`), not in the repository; the default evidence never reaches back more than 90 days.
- **HE-06** **Scheduled reflection** is off by default. Turned on in the workspace's user-side settings (`[harness] reflect_schedule`, in words or cron as job schedules are; `[harness] max_cost_usd`, default $0.50; never from a project's files, as `[health]`; both in the Overview's settings dialog beside the health schedule), the service's scheduler (its minute rescan, `Server.reflectDue`, one at a time per workspace) runs it when due: the schedule counts from when it's first seen, and from the last reflection, scheduled or not. It runs only when the evidence holds a pattern newer than its last run; otherwise it records why not. The reflector runs read-only and unattended (writes and questions refused) in a session of its own, deleted after, within the cost limit, and keeps the changes it would offer with **`harness_propose`** (a tool only it names, refused in a conversation) in the service's storage instead of asking. The tile then says **N changes to review** (`SUGGESTION_KIND_REFLECT_REVIEW`) and sends `/reflect`, whose `harness_evidence` starts with them, so they're offered in the same `multi_select` with any newer ones; a `/reflect` that finishes clears them. Nothing is written to the workspace unattended. Why the last scheduled one didn't run or stopped shows in the settings dialog.

- **HE-07** *2026-10-22.* **Within a turn**, what the reflector would learn later is said at once: the third time the same call (tool and arguments) fails with the same error in a run, its result carries `repeated_failure`, telling the agent not to make it again but to read the error, change the call, try another way or ask (`repeatedFailure` in `pkg/engine/runtime/engine.go`). It warns, it doesn't refuse. The count is per run: a sub-agent's or the next turn's starts afresh. Seen in the audit log before this spec: one agent read the same missing file over and over in minutes.

## 4. Phase B: run evidence in `/skills update` and `/agents update`

- **HE-10** `harness-update` gets the HE-02 evidence for the skills or agents it compares, besides the specification: it names `harness_evidence` and reads the last 30 days of it `about` skills or agents in its step 1. Step 2 (Compare) adds: a skill whose runs fail at the same step, a skill nothing activated in 30 days, an agent whose turns are often stopped or corrected, an agent denied the same tool repeatedly (its `tools` or `permission_mode` don't fit what it's asked to do); only patterns count. So an agent can be blamed, the audit log names the agent on a denial and on a failed tool result (`Entry.Agent`, taken from the agent's context; not on other entries, which don't need it), and the evidence prints it with each occurrence.
- **HE-11** Each option cites its source: a spec (`004-authentication`), the evidence ("3 failed runs of deploy since 2026-10-01: `make push` not found"), or a health verdict's fix (HL-10). Options citing both a spec and evidence come first.
- **HE-12** The update tile (`SUGGESTION_KIND_HARNESS_UPDATE`) also shows when the evidence since the project's skills (or agents) last changed holds a pattern about one of them (its name, or the agent refused or failing): its detail is `skills` or `agents` instead of `spec`, its text **Skills went wrong in recent runs: update them** (or Agents), and it sends `/skills update` or `/agents update`. A changed specification comes first. The evidence behind the welcome's tiles is cached per window until the audit log changes (`cachedEvidence`).

## 5. Phase C: invariant and project experience

- **HE-20** The reflector sorts each entry as Harness² does: **How to work** (invariant: planning, tool use, verification, how to ask; true in any project) or **This project** (domain: its commands, data, conventions, people's preferences). `.agents/AGENTS.md`, as `/setup` writes it, keeps the same split: a short "How to work here" section first (three to five habits the user asked for or the answers imply), apart from the project's facts.
- **HE-21** A "How to work" entry that has held in two or more workspaces (the same entry, by meaning, in their `EXPERIENCE.md`) may be offered for the user's global file (`memory.global`, default `~/.blitz/BLITZ.md`): one option at most in the reflector's question, naming the workspaces, never without a yes. The reflector's evidence (`harness_evidence` without `about`) ends with the "How to work" entries of the user's other workspaces (those with sessions in the service's storage; 20 at most, 30 entries each) and what the global file holds; a chosen entry goes under its `## How to work` heading with **`save_global_experience`**, a tool only the reflector names, refused unattended and in read-only turns, that adds an entry once and reloads the instructions. Lessons in "This project" are never offered globally, and nothing else is read from the other workspaces.
- **HE-22** The reflector's prompt asks for both kinds of candidate before choosing (Harness²'s propose step), so a failure gets considered as a general habit and as a project fact; it offers the one the evidence fits best, not both.

## 6. Phase D: setup covers guardrails and rules

- **HE-30** `project-setup`, after the agents, offers up to three **guardrails** and **path-scoped rules** drawn from the specification's boundaries and the interview, as one `multi_select` (its step 9, skipped unattended), only where the project gave a reason: for example a rule scoped to the migrations' folder (`paths`, "never edit an applied migration"), a gate checked after each answer (`when: after_agent`, `action: revise`), the commands the project must never run in the built-in `limits` guardrail (its `commands`, keeping the default ones), `secrets` with `pii: true` when the specs name personal data. Their formats are [spec_gates_041](spec_gates_041.md)'s; they go in `.agents/rules/` and `.agents/guardrails/`.
- **HE-31** `harness-update` gains a third mode, **`/rules update`** (the bundled `/rules-update`; `commands.Alias`, the REPL and the window take the two-word spelling as for skills and agents), comparing rules and guardrails with the specification and the evidence (HE-02's blocks, revisions and denials) as HE-10 does for skills and agents: a rule that keeps sending work back for the same thing, a guardrail blocking what the user then allowed, the same action denied again and again, a boundary in the specs nothing enforces.
- **HE-32** `/setup` writes `.agents/EXPERIENCE.md` empty (a title and its two headings) and ends `.agents/AGENTS.md` with its import, so the file is there from the start and committed with the rest; an existing one is kept.

## 7. Phase E: probing a change against evals (later; needs a decision)

Harness²'s step that Blitz lacks is checking a candidate by running it. Blitz has a verifier where Harness² doesn't: evals ([spec_evals_042](spec_evals_042.md)).

- **HE-40** When a proposed change (HE-03, HE-10) touches an agent or skill that has evals, the option may say **Try it**: the engine runs those evals with the change applied in a scratch copy of `.agents/` and without, and shows both pass rates and costs before the user picks. Nothing in the workspace changes until the pick.
- **HE-41** Off by default (`[harness] probe = false`); each run shows an estimate first (as `EstimateHealthCheck`, HL-13) and stops at `[harness] max_cost_usd`.
- **HE-42** Without evals, no probing: the change is offered on its evidence alone. Running candidate harnesses on the user's real tasks (Harness²'s way, with no verifier) is out of scope: it doubles the cost of every task and would act on the user's files twice.

Decide before building: whether probing is worth its cost against what phases A–D give, after they've been used for a while; read the Harness² paper and code first.

## 8. Order and checkpoints

1. **Evidence:** `harness.Evidence` with table tests per source (transcripts, audit log, gates, health facts), and the pattern count for HE-05. No model.
2. **Experience and the reflector:** `.agents/EXPERIENCE.md` and its import, `harness-reflect`, `/reflect`, the reload after it; tests on the mock model (proposals cite evidence, nothing is written unchosen). **Checkpoint with the owner**, on a real workspace's month of sessions.
3. **The tile and the schedule:** HE-05, HE-06, in the desktop app and the CLI's welcome.
4. **Phase B:** `harness-update` with evidence, `HarnessStale`'s new reason.
5. **Phase C and D:** the split in `/setup`'s `.agents/AGENTS.md`, global entries, guardrails and rules in `/setup`, `/rules update`. **Checkpoint with the owner.**
6. **The end:** docs (the [spec, then setup tutorial](../../tutorials/spec-then-setup.md), [Extending](../../guide/extending.md), the command reference), this spec's status and paths, manual checks, i18n (en-US, es, fr-CA), the full suite and coverage.
7. **Phase E**, only after the decision in §7.

## 9. Open questions

- Detecting corrections without a model (HE-02) will miss some and catch some that aren't; a cheap model call per candidate may be worth it. Measure on real sessions in step 2.
- A rewind of the conversation truncates its transcript; the audit log keeps the prompts it dropped, which is one reason the evidence reads the audit log only.
- Whether entries should expire (unused evidence for 90 days) or only be retired when chosen.
- Sessions in other front ends (Claude Code, Gemini CLI) aren't Blitz's and aren't read; what they'd teach is lost.
- Whether a team's entries should merge: `EXPERIENCE.md` is committed, so two people's reflections conflict like any file.
