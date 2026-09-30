---
title: "032 · Background sub-agents"
weight: 32
---

*Background sub-agents and the task view* (`spec_background_agents_032`)

| | |
|---|---|
| Status | **Implemented** (2026-09-29), in the three phases of §8; §9 records the decisions taken. It details [spec_parity_027](spec_parity_027.md) §8.1 (PAR-PAR-01–03); §12.5 defers agent teams until this exists. |
| Source | `pkg/engine/runtime/{tasks,agentrun}.go`; `pkg/engine/tools/{tasks,agent_tools,hooks}.go`; `pkg/engine/tasks.go`; `apps/service/internal/server/{workspace,session}.go`; `pkg/client/processes.go`; `apps/cli/internal/tui/{tasks,exit}.go`; `apps/desktop/web/src/Conversation.tsx` (`TaskCard`, the watch) |
| Tests | `pkg/engine/runtime/tasks_test.go`, `pkg/engine/tools/permrules_test.go`, `pkg/engine/agents/frontmatter_test.go`, `pkg/client/processes_test.go`, `apps/cli/internal/tui/tasks_test.go`, `apps/desktop/web/src/turns.test.ts` |
| Depends on | [spec_agents_014](spec_agents_014.md) (`invoke_agent`), [spec_approvals_005](spec_approvals_005.md), [spec_workspace_018](spec_workspace_018.md) (steering), [spec_service_021](spec_service_021.md), [spec_client_022](spec_client_022.md) |
| Size | L, built in three phases (§8) |

## 1. Purpose

`invoke_agent` blocks the turn until the sub-agent finishes, so the main agent can't do two things at once or start a long review while the person keeps talking to it. Background sub-agents run beside the turn. The main agent gets a task ID at once and hears the result when it's ready. The person can see, follow and stop the work.

## 2. How it worked before

- **Running a sub-agent.** `invoke_agent(agent_name, prompt)` calls `Engine.InvokeSubagent`. That builds the agent and runs it on its own in-memory runner, in a session named `subagent-<name>-<n>`, nested at most 3 deep. It returns only the final text: the sub-agent's tool calls never reach a front end.
- **Cost and limits.** The sub-agent's tokens and `max_turns` budget are the parent's, because its context carries the parent's run state.
- **Cancellation.** Cancelling the turn cancels the sub-agent.
- **Approvals.** They go through the workspace's one approver. In the service they need the running turn's stream: once the turn ends, a request is refused, and with no client attached it is refused at once. Requests carry no agent name, and the desktop app holds one pending request at a time.
- **Steering.** Steer messages are queued per session and added to the next tool result (`message_from_user`). What's left when the turn ends goes back to the client, which sends it as the next turn.
- **Starting turns.** Nothing starts a turn in an idle interactive session, and a client that isn't running a turn gets no events. Workers run in their own sessions, from the service's scheduler.
- **Concurrency.** The engine runs turns of different sessions at once (workers already do).
- **Existing pattern.** Background shell processes show the shape to reuse: IDs, a cap on how many run at once, a lifetime, kept output, finished ones retained, listing by session.

## 3. Tasks

- **BGA-01 Starting one.** `invoke_agent(agent_name, prompt, background: true)` starts the sub-agent and returns at once: `{task_id: "task-3", agent_name, status: "running"}`. Without `background` (or `false`) it behaves as today.
- **BGA-02 Task manager.** The engine's task manager (`runtime/tasks.go`) holds a workspace's tasks:
  - each task has an ID, agent, prompt (first line), parent session, state, start and end times, final text or error, its own usage, and the last 200 events of its run;
  - at most `tools.max_background_agents` run at once (default 4); starting another returns an error the agent can act on;
  - finished tasks are kept (the last 16 per session);
  - tasks are held in memory: a restart of the process ends them (§5).
- **BGA-03 Run context.** A task runs detached from the turn that started it: `context.WithoutCancel(ctx)` with its own cancel. It ends when it finishes, when `stop_task` or `/tasks stop` is used, when its lifetime (`tools.background_agent_timeout`, default 30 min) is reached, or when the workspace closes.
- **BGA-04 Nesting.** Only the top-level agent may start background tasks. A task can still `invoke_agent` in the foreground, within the existing depth limit, but can't start background tasks of its own (a clear error).
- **BGA-05 Usage.** A task's tokens and cost are charged to its parent session, so `/cost` and the session's total include them. They are also counted on the task, so the task list shows each task's cost.
- **BGA-06 Limits.** A task has its own model-call budget (`max_turns` from its agent's frontmatter, else `tools.background_agent_max_turns`, default 50) and an optional cost cap (`tools.background_agent_max_cost_usd`). The parent turn's limits stop at the parent turn.
- **BGA-07 Shell processes.** Background shells started by a task are tagged with the parent session, not with `subagent-…`, so the exit prompt and `ListProcesses` include them. This fixes a gap that exists today.

## 4. Tools the agent uses

- **BGA-10** `list_tasks()`: the tasks of this session, each with ID, agent, state, runtime, cost, and the first line of its result.
- **BGA-11** `task_output(task_id, wait_seconds?)`: the state and the final text, or while it runs, the last few events. `wait_seconds` (at most 300) blocks until the task ends or the wait runs out, so an agent can wait on purpose.
- **BGA-12** `stop_task(task_id)`: stops the task and returns its state.
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
  - A background `invoke_agent` shows as a task card under its tool group (agent, state, runtime, cost, the prompt, **Show** and **Stop**), refreshed every 2 s until it ends ([spec_desktop_024](spec_desktop_024.md) DSK-80a).
  - A **Tasks** section in the run settings panel wasn't built: the cards and `/tasks` cover it.

## 7. Approvals

The hard part: a task may need an approval while the person is doing something else, or after the turn that started it has ended.

- **BGA-40 Phase 1: unattended** (replaced by BGA-41 in phase 2). A task runs as workers do (`tools.Unattended`): what the workspace's mode and rules allow runs; anything that would ask is refused and returned to the task as a denial ("needs approval: shell `make deploy`; not asked, because it runs in the background"); `ask_user_question` is refused. The task can report what it needed, and the main agent can do that step itself in the foreground. This is safe, needs no new UI, and is useful at once for reviews, research and tests.
- **BGA-41 Phase 2: labelled approvals.**
  - A task's run routes its approval requests and questions to its session's people (`tools.TaskAsker`) once the workspace's rules and mode would ask: they become the engine's waiting requests (`api.TaskRequest`: ID, task, agent, session, and the approval or the question), labelled "qa · task-3"; the protos' `ApprovalRequest` and `Question` gain `agent` and `task_id`.
  - While one waits, the task is `waiting`. `WatchTasks` and `ListTaskRequests` carry them; `SessionService.Approve` and `Answer` answer them, falling back to the tasks' requests when no running turn owns the ID.
  - With nobody watching, it waits (the task's lifetime and `stop_task` still end it) and is shown when a client next looks.
  - The desktop app shows them as labelled cards beside the turn's own, not focused, and **Ctrl+J** moves to the next (PAR-PAR-02). The REPL asks them at its prompt, labelled, so a turn's own questions are never interrupted.
- **BGA-42** The turn's broker is unchanged: tasks' requests don't go through it.

## 7a. Agent defaults

- **BGA-50** Agent frontmatter may set `permission_mode`, `max_turns` and `background` (PAR-PAR-03). Through `invoke_agent`, `plan` refuses the agent's changes (its own plan gate) and the other modes replace the workspace's for its actions (`tools.WithMode`). A mode looser than the workspace's applies only to built-in agents, the user's own and, once its settings are trusted, the project's ([spec_project_config_031](spec_project_config_031.md)); `bypass` also needs the OS sandbox; otherwise the workspace's mode stays and the log says why. `max_turns` gives the agent its own model-call budget, in the foreground and as a task. `background: true` is `invoke_agent`'s default for the agent, unless the call says otherwise. An unknown mode or a negative `max_turns` rejects the agent's file.

## 8. Phases (as built)

1. **Phase 1: background tasks.** BGA-01–07, the three tools, delivery at the next tool cycle or prompt (BGA-20–22), `/tasks`, the exit prompt, the desktop task card, and `ListTasks`, `GetTask`, `StopTask`.
2. **Phase 2: watching and approvals.** BGA-32, BGA-41: `WatchTasks`, `ListTaskRequests`, labelled requests, **Ctrl+J**, the REPL's prompt, the continue preference.
3. **Phase 3: agent defaults.** BGA-50.

Tests: the task manager's cap, lifetime, stop and close; delivery into a running turn and into the next prompt; usage charged to both the task and the session; nesting refused; what a background run may do without asking; requests answered, refused to another session, and ended by a stop; the service's stream, answers and scoping between clients; the REPL's list, notices, answers and exit prompt; agents' defaults, including a project's agent that may not loosen.

## 9. Decisions (2026-09-29) and gaps

1. **Edits made in parallel are allowed.** A task's file edits join the latest turn's checkpoint, so `/undo` reverts them with it; separate checkpoint entries per task ("task-3 (qa)") weren't built. Worktree isolation (§8.2, PAR-PAR-11) comes later for tasks that should be kept apart.
2. **Continuing when idle is off by default**, and a preference of the desktop app rather than a setting: the REPL can't start a turn while it waits for a line.
3. **Tasks don't survive a restart.** Closing the workspace stops them, and the transcript says so.
4. **Phase 1 refused what a task would ask;** phase 2 replaced that with asking the session's people (BGA-41).
