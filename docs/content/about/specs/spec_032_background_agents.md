---
aliases: ["/about/specs/spec_background_agents_032/"]
title: "032 · Background sub-agents"
weight: 32
---

*Background sub-agents and the task view* (`spec_032_background_agents`)

| | |
|---|---|
| Status | **Implemented** (2026-09-29), in the three phases of §8; §9 records the decisions taken. It details [spec_027_parity](spec_027_parity.md) §8.1 (PAR-PAR-01–03); §12.5 defers agent teams until this exists. |
| Source | `pkg/engine/runtime/{tasks,agentrun}.go`; `pkg/engine/tools/{tasks,agent_tools,hooks}.go`; `pkg/engine/tasks.go`; `pkg/service/server/{workspace,session}.go`; `pkg/client/processes.go`; `apps/cli/internal/tui/{tasks,exit}.go`; `apps/desktop/web/src/Conversation.tsx` (`TaskCard`, the watch) |
| Tests | `pkg/engine/runtime/tasks_test.go`, `pkg/engine/tools/permrules_test.go`, `pkg/engine/agents/frontmatter_test.go`, `pkg/client/processes_test.go`, `apps/cli/internal/tui/tasks_test.go`, `apps/desktop/web/src/turns.test.ts` |
| Depends on | [spec_014_agents](spec_014_agents.md) (`invoke_agent`), [spec_005_approvals](spec_005_approvals.md), [spec_018_workspace](spec_018_workspace.md) (steering), [spec_021_service](spec_021_service.md), [spec_022_client](spec_022_client.md) |
| Size | L, built in three phases (§8) |

## 1. Purpose

`invoke_agent` blocks the turn until the sub-agent finishes, so the main agent can't do two things at once or start a long review while the person keeps talking to it. Background sub-agents run beside the turn. The main agent gets a task ID at once and hears the result when it's ready. The person can see, follow and stop the work.

## 2. How it worked before

- **Running a sub-agent.** `invoke_agent(agent_name, prompt)` calls `Engine.InvokeSubagent`. That builds the agent and runs it on its own in-memory runner, in a session named `subagent-<name>-<n>`, nested at most 3 deep. It returns only the final text: the sub-agent's tool calls never reach a front end.
- **Cost and limits.** The sub-agent's tokens and `max_turns` budget are the parent's, because its context carries the parent's run state.
- **Cancellation.** Cancelling the turn cancels the sub-agent.
- **Approvals.** They go through the workspace's one approver. In the service they need the running turn's stream: once the turn ends, a request is refused, and with no client attached it is refused at once. Requests carry no agent name, and the desktop app holds one pending request at a time.
- **Steering.** Steer messages are queued per session and added to the next tool result (`message_from_user`). What's left when the turn ends goes back to the client, which sends it as the next turn.
- **Starting turns.** Nothing starts a turn in an idle interactive session, and a client that isn't running a turn gets no events. Jobs run in their own sessions, from the service's scheduler.
- **Concurrency.** The engine runs turns of different sessions at once (jobs already do).
- **Existing pattern.** Background shell processes show the shape to reuse: IDs, a cap on how many run at once, a lifetime, kept output, finished ones retained, listing by session.

## 3. Tasks

- **BGA-01 Starting one.** `invoke_agent(agent_name, prompt, background: true)` starts the sub-agent and returns at once: `{task_id: "task-3", agent_name, status: "running"}`. Without `background` (or `false`) it behaves as today.
- **BGA-02 Task manager.** The engine's task manager (`runtime/tasks.go`) holds a workspace's tasks:
  - each task has an ID, agent, prompt (first line), parent session, state, start and end times, final text or error, its own usage, and the last 200 events of its run;
  - at most `tools.max_background_agents` run at once (default 4); starting another returns an error the agent can act on;
  - finished tasks are kept (the last 16 per session);
  - tasks are held in memory: a restart of the process ends them (§5).
- **BGA-03 Run context.** A task runs detached from the turn that started it: `context.WithoutCancel(ctx)` with its own cancel. It ends when it finishes, when `stop_task` or `/tasks stop` is used, when its lifetime (`tools.background_agent_timeout`, default 30 min) is reached, or when the workspace closes.
- **BGA-03a** *2026-11-04 (scan N26-engine-runtime#1, #7: a task's run state was built fresh, so `invoke_agent(…, background: true)` from a `/plan` turn, `/btw`, `/search` or a command with `allowed-tools` could write; and it kept the tools' notice sink of the turn that started it).* A task keeps the restrictions of the run that started it: plan-only or read-only (and the mode its refusals name) and the allowed tools, as a foreground sub-agent does (one helper, `runState.child`, for both). The turn's notices aren't its: neither the engine's nor the tools' (an MCP server failing while it runs goes to the workspace's notices).
- **BGA-04 Nesting.** Only the top-level agent may start background tasks. A task can still `invoke_agent` in the foreground, within the existing depth limit, but can't start background tasks of its own (a clear error).
- **BGA-05 Usage.** A task's tokens and cost are charged to its parent session, so `/cost` and the session's total include them. They are also counted on the task, so the task list shows each task's cost.
- **BGA-06 Limits.** A task has its own model-call budget (`max_turns` from its agent's frontmatter, else `tools.background_agent_max_turns`, default 50) and an optional cost cap (`tools.background_agent_max_cost_usd`). The parent turn's limits stop at the parent turn.
- **BGA-06a** *2026-11-04 (scan N26-engine-runtime#3, #4: the defaults were written twice, 0 or `"30 min"` quietly became the default, and running out of model calls named no setting).* The defaults are defined once, in `config` (`DefaultMaxBackgroundAgents`, `DefaultBackgroundAgentTimeout`, `DefaultBackgroundAgentMaxTurns`), and read through `ToolsConfig.BackgroundLimits`. A value that isn't one (`max_background_agents` or `background_agent_max_turns` 0 or less, a timeout that isn't a positive Go duration) fails the task's start with an error naming the key, and the settings check reports it at its line; an empty timeout is the default. A task stopped by its own model-call budget says which setting gave it: "maximum turns reached (50 model calls): tools.background_agent_max_turns sets it", or the agent's `max_turns`.
- **BGA-06b** *2026-11-04 (scan N26-engine-runtime#5: on a model without a price a task's cost stays $0 and the cap never fires, with no word; the reviewer's and hooks' calls weren't counted on the task).* When a task with a cost cap makes a call of unknown cost (an unpriced model, or a call without usage), it is told once, in its events and the workspace's notices ("tasks.cost_unpriced": the cap can't stop it; add a price under `[pricing]`). The auto mode's reviewer and the hooks' judges, when a task causes them, count toward its cost too.
- **BGA-08 Isolation.** `invoke_agent(…, background: true, isolation: "worktree")`, or an agent's `isolation: worktree`, runs the task in its own git worktree (`task-<n>-<date>-<hex>`, branch `blitz/…`, `pkg/worktree`): its tools come from a registry rooted there (files, sandbox, checkpoints; the workspace's settings and current mode, not its MCP servers or hooks, which the workspace's run provides), approvals still go to the session. The task (`api.TaskInfo.Worktree`, `Branch`), `/tasks show`, the desktop card and the note to the main agent say where its changes are, to review and merge, then `blitz worktrees remove`. Isolation without background, or outside a git repository, is refused (PAR-PAR-11).
- **BGA-08a** *2026-11-04 (scan N26-engine-runtime#1b, #2: the worktree's registry had no sub-agents, tasks or reviewer, its own empty session grants, no `permission_request` hooks, and a mode it couldn't take was dropped silently).* The worktree's gate decides as the workspace's (`tools.Hooks.Share`): its `invoke_agent` runs sub-agents (BGA-04), `list_tasks` and `task_output` work, the auto mode's reviewer judges, the `permission_request` hooks answer, and the session's grants apply there and grow from it. A session mode the worktree can't take (bypass without its OS sandbox) fails the start, naming the mode, rather than running in another.
- **BGA-07 Shell processes.** Background shells started by a task are tagged with the parent session, not with `subagent-…`, so the exit prompt and `ListProcesses` include them. This fixes a gap that exists today.

## 4. Tools the agent uses

- **BGA-10** `list_tasks()`: the tasks of this session, each with ID, agent, state, runtime, cost, and the first line of its result.
- **BGA-11** `task_output(task_id, wait_seconds?)`: the state and the final text, or while it runs, the last few events. `wait_seconds` (at most 300) blocks until the task ends or the wait runs out, so an agent can wait on purpose.
- **BGA-12** `stop_task(task_id)`: stops the task and returns its state.
- **BGA-12a** *2026-11-04 (scan N26-engine-runtime#12: the wait and the cross-session cap were in no spec).* `stop_task`, `/tasks stop` and the workspace closing wait up to 5 s (`stopWait`) for the task to end; one still ending after that is reported as still running and ends on its own. Besides the last 16 per session, at most 64 ended tasks are kept across sessions (`maxEndedTasks`; each worker run is a session), the oldest dropped first.
- **BGA-13** All three only see tasks of the calling agent's session (`UNKNOWN_TASK` otherwise).

## 5. Results reaching the main agent

- **BGA-20 A turn is running in the parent session.** The engine adds a note to the next tool result, beside any steer messages. The note uses its own key (`task_updates`, not `message_from_user`, so the agent can tell it isn't the person), for example: "task-3 (qa) finished in 2m10s: <first 2,000 characters>. Use task_output for all of it."
- **BGA-21 The parent session is idle.** The note waits and goes in front of the next prompt, as hook context does (`<background-tasks-hook-context>`). The desktop app can also start a turn by itself (BGA-32).
- **BGA-22 Transcript.** A finished, failed or stopped task adds a message to the parent's transcript, so the chat shows it after a restart. It uses a new message kind, `task`, with the note of BGA-20. Tasks still running when the workspace closes are stopped and recorded as stopped.

## 6. The person's view

- **BGA-30 `/tasks` in the REPL.**
  - The list shows ID, agent, state (running, waiting for you, done, failed, stopped), runtime and cost.
  - `/tasks show <id>` prints the task's events so far and its result.
  - `/tasks stop <id>` stops it.
  - A task starting shows as the `invoke_agent` call; one ending shows as a dim line at the next prompt.
- **BGA-31 Exiting.** The exit prompt counts running tasks alongside background processes, with the same kill, wait and cancel choices. Attached, they belong to the service and keep running after the client exits, as processes do, and the client lists only its own sessions' tasks.
- **BGA-32 Idle notification.** A server stream, `WorkspaceService.WatchTasks(workspace, session_ids)`, carries events outside turns: first the sessions' active tasks and waiting requests, then `ready` (which also sends the response headers), then each task starting, waiting, running again or ending, each request waiting, and each request resolved. The desktop app follows it while a chat is open; a task that ends adds a notice, and with Settings › **Continue after background tasks** (a preference of the app, `task_continue`, off by default) it also starts a turn, "Background task task-3 has ended: carry on with its result.", when nothing runs. The REPL, which reads a line at a time, doesn't stream: it looks at its prompt (`ListTasks`, `ListTaskRequests`).
- **BGA-33 Desktop app.**
  - A background `invoke_agent` shows as a task card under its tool group (agent, state, runtime, cost, the prompt, **Show** and **Stop**), refreshed every 2 s until it ends; *2026-10-24 (v0.8.4):* its runtime is a clock (m:ss, h:mm:ss past an hour) that ticks each second while it runs ([spec_024_desktop](spec_024_desktop.md) DSK-80a).
  - A **Tasks** section in the run settings panel wasn't built: the cards and `/tasks` cover it.

## 7. Approvals

The hard part: a task may need an approval while the person is doing something else, or after the turn that started it has ended.

- **BGA-40 Phase 1: unattended** (replaced by BGA-41 in phase 2). A task runs as jobs do (`tools.Unattended`): what the workspace's mode and rules allow runs; anything that would ask is refused and returned to the task as a denial ("needs approval: shell `make deploy`; not asked, because it runs in the background"); `ask_user_question` is refused. The task can report what it needed, and the main agent can do that step itself in the foreground. This is safe, needs no new UI, and is useful at once for reviews, research and tests.
- **BGA-41 Phase 2: labelled approvals.**
  - A task's run routes its approval requests and questions to its session's people (`tools.TaskAsker`) once the workspace's rules and mode would ask: they become the engine's waiting requests (`api.TaskRequest`: ID, task, agent, session, and the approval or the question), labelled "qa · task-3"; the protos' `ApprovalRequest` and `Question` gain `agent` and `task_id`.
  - While one waits, the task is `waiting`. `WatchTasks` and `ListTaskRequests` carry them; `SessionService.Approve` and `Answer` answer them, falling back to the tasks' requests when no running turn owns the ID.
  - With nobody watching, it waits (the task's lifetime and `stop_task` still end it) and is shown when a client next looks.
  - The desktop app shows them as labelled cards beside the turn's own, not focused, and **Ctrl+J** moves to the next (PAR-PAR-02). The REPL asks them at its prompt, labelled, so a turn's own questions are never interrupted.
- **BGA-42** The turn's broker is unchanged: tasks' requests don't go through it.

## 7a. Agent defaults

- **BGA-50** Agent frontmatter may set `permission_mode`, `max_turns` and `background` (PAR-PAR-03). Through `invoke_agent`, `plan` refuses the agent's changes (its own plan gate) and the other modes replace the workspace's for its actions (`tools.WithMode`). A mode looser than the workspace's applies only to built-in agents, the user's own and, once its settings are trusted, the project's ([spec_031_project_config](spec_031_project_config.md)); `bypass` also needs the OS sandbox; otherwise the workspace's mode stays and the log says why. `max_turns` gives the agent its own model-call budget, in the foreground and as a task. `background: true` is `invoke_agent`'s default for the agent, unless the call says otherwise. An unknown mode or a negative `max_turns` rejects the agent's file.
- **BGA-50a** *2026-11-04 (scan N26-engine-runtime#14: an agent's `max_turns` replaced the calling run's budget, so a project's agent with `max_turns: 100000` escaped the limits guardrail's 200-call safeguard).* In the foreground, `max_turns` is the agent's own cap inside the calling run's budget: each model call counts toward both (`runState.parent`), and whichever runs out first stops it. A task (BGA-06) outlives its turn, so its budget is its own.
- **BGA-50a** *2026-10-14.* `permission_mode` is parsed before it's ranked or applied (`api.ParsePermissionMode`): an alias (`acceptEdits`, `bypassPermissions`, `ask`) is its canonical mode for every check, so it can't slip past "a project's agent can't loosen the mode until trusted", and a value that doesn't parse is ignored with a warning (`runtime/agentrun.go`).

## 8. Phases (as built)

1. **Phase 1: background tasks.** BGA-01–07, the three tools, delivery at the next tool cycle or prompt (BGA-20–22), `/tasks`, the exit prompt, the desktop task card, and `ListTasks`, `GetTask`, `StopTask`.
2. **Phase 2: watching and approvals.** BGA-32, BGA-41: `WatchTasks`, `ListTaskRequests`, labelled requests, **Ctrl+J**, the REPL's prompt, the continue preference.
3. **Phase 3: agent defaults.** BGA-50.

Tests: the task manager's cap, lifetime, stop and close; delivery into a running turn and into the next prompt; usage charged to both the task and the session; nesting refused; what a background run may do without asking; requests answered, refused to another session, and ended by a stop; the service's stream, answers and scoping between clients; the REPL's list, notices, answers and exit prompt; agents' defaults, including a project's agent that may not loosen.

## 10. Background runs (spec_027_parity PAR-PAR-20)

A task is a sub-agent inside a turn. A background run is a whole turn the service runs with no client attached, started from a terminal and picked up later from any client.

- **BGA-60 Starting one.** `blitz --bg "<prompt>"` (the prompt as for a one-shot run; with `--plan`, `--max-turns`, `--max-cost-usd`, `--timeout`; not with `-i`, `--local`, `--resume`, `--continue` or `--output-format`) calls `SessionService.StartBackground` and prints the run's ID and session, and how to follow it. It needs the service (`errNeedsService` otherwise).
- **BGA-60a** *2026-11-05 (scan N26-cli-2#1: the other run flags were accepted and dropped, so `--bg --deny … --permission-mode plan` ran with neither).* A background run carries the prompt, `--image` and `@image` mentions, `--plan`, `--max-turns`, `--max-cost-usd` and `--timeout` (and `-d`, `--worktree`, which pick the workspace); every other run flag is a usage error naming it: `--agent`, `--model`, `--agency`, `--effort`, `--permission-mode`, `--allow`, `--deny`, `--trust-project`, `--add-dir`, `--plugin-dir`, `--append-system-prompt[-file]`, `--json-schema`, `--name`, `--fork`, `--no-session-persistence` and `--input-format`. The run takes the workspace's own settings in the shared service.
- **BGA-60b** *2026-11-05 (scan N26-cli-behaviour, the owner's decision: the commands refused when the service wasn't running, unlike the rest of the CLI).* `blitz --bg`, `agents`, `logs`, `stop` and `attach` start the shared service when it isn't running ([spec_020_cli](spec_020_cli.md) CLI-14, CLI-14a); `errNeedsService` is gone. A service started this way has no background runs yet, so `logs`, `stop` and `attach` with an ID say it's unknown (exit 2).
- **BGA-61 Its session.** The run's turn goes in a new session of the workspace, made in a session storage of its own as a job's is (`engine.RunDetached`), so the workspace's active session, and the clients working there, stay where they are. The run is `bg-<n>`, counted over the service's lifetime; its state is running, waiting (a request waits for an answer), done, failed or stopped.
- **BGA-62 Its events and requests.** The run's events are kept (the latest 5,000) for anyone who watches it. Its approval requests and questions go through the service's broker like a turn's, on the run's own sink: they wait, with nobody attached, until a client answers them with `Approve` or `Answer`. The run's own limits and `stop` still end it.
- **BGA-63 Watching.** `WatchBackground(id, follow)` streams the kept events from the start, leaving out requests already answered, and with `follow` those to come, ending with `TurnFinished`. `ListBackground` gives every workspace's runs, newest first (the running ones, and the latest 50 that ended), with cost and how many requests wait. `StopBackground` cancels one and waits for it to end. An unknown ID is `UNKNOWN_RUN` (`api.ErrUnknownRun`).
- **BGA-64 The CLI.** `blitz agents [--all]` lists the runs going on (or all) as a table: ID, state (with the requests waiting), age, cost, workspace, prompt. `blitz logs <id> [-f]` prints a run's events so far (following it with `-f`) without answering anything. `blitz stop <id>` stops one and says where its session is.
- **BGA-65 Attaching.** `blitz attach <id>` opens the REPL, attached to the service, in the run's workspace with its session resumed. While the run goes on the REPL follows it first (`App.Follow`, `Remote.FollowBackground`): its events print as a turn's do, and its requests are asked at the terminal. Ctrl+C stops following (the run goes on); once it ends, the session is loaded again and the prompt carries on in it. `--local` is refused.
- **BGA-66 Lifetime.** Runs live in the service's memory: closing the service stops them (their sessions keep what they did). Closing a workspace is refused while a run works in it, as for any turn.
- **BGA-67 The desktop app** shows the runs as an inbox ([spec_024_desktop](spec_024_desktop.md) DSK-80b).

Tests: the service's runs (a waiting approval seen by a watcher and answered, the session not made active, the replay without answered requests, stop, close, the bounds); the client (start, find, logs, follow answering, unknown IDs); the CLI with the service and a fake model (`--bg`, `agents`, `logs`, `stop`, `attach` errors, the arguments `--bg` refuses); the REPL's following (to the end, failing, Ctrl+C); the page's helpers.

## 9. Decisions (2026-09-29) and gaps

1. **Edits made in parallel are allowed.** A task's file edits join the latest turn's checkpoint, so `/undo` reverts them with it; separate checkpoint entries per task ("task-3 (qa)") weren't built. Worktree isolation (§8.2, PAR-PAR-11) comes later for tasks that should be kept apart.
2. **Continuing when idle is off by default**, and a preference of the desktop app rather than a setting: the REPL can't start a turn while it waits for a line.
3. **Tasks don't survive a restart.** Closing the workspace stops them, and the transcript says so.
4. **Phase 1 refused what a task would ask;** phase 2 replaced that with asking the session's people (BGA-41).
