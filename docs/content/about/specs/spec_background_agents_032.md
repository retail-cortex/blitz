---
title: "032 · Background sub-agents"
weight: 32
---

*Background sub-agents and the task view* (`spec_background_agents_032`)

| | |
|---|---|
| Status | **Draft for review** (2026-09-29). A design, not yet built. It details [spec_parity_027](spec_parity_027.md) §8.1 (PAR-PAR-01–03); §12.5 defers agent teams until this exists. |
| Depends on | [spec_agents_014](spec_agents_014.md) (`invoke_agent`), [spec_approvals_005](spec_approvals_005.md), [spec_workspace_018](spec_workspace_018.md) (steering), [spec_service_021](spec_service_021.md), [spec_client_022](spec_client_022.md) |
| Size | L, in three phases (§8): M, M and S |

## 1. Purpose

`invoke_agent` blocks the turn until the sub-agent finishes, so the main agent can't do two things at once or start a long review while the person keeps talking to it. Background sub-agents run beside the turn. The main agent gets a task ID at once and hears the result when it's ready. The person can see, follow and stop the work.

## 2. How it works today

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
- **BGA-02 Task manager.** A workspace's `TaskManager` holds its tasks:
  - each task has an ID, agent, prompt (first line), parent session, state, start and end times, final text or error, its own usage, and the last 200 events of its run;
  - at most `tools.max_background_agents` run at once (default 4); starting another returns an error the agent can act on;
  - finished tasks are kept (the last 16 per session);
  - tasks are held in memory: a restart of the process ends them (§5).
- **BGA-03 Run context.** A task runs detached from the turn that started it: `context.WithoutCancel(ctx)` with its own cancel. It ends when it finishes, when `stop_task` or `/tasks stop` is used, when its lifetime (`agents.background.timeout`, default 30 min) is reached, or when the workspace closes.
- **BGA-04 Nesting.** Only the top-level agent may start background tasks. A task can still `invoke_agent` in the foreground, within the existing depth limit, but can't start background tasks of its own (a clear error).
- **BGA-05 Usage.** A task's tokens and cost are charged to its parent session, so `/cost` and the session's total include them. They are also counted on the task, so the task list shows each task's cost.
- **BGA-06 Limits.** A task has its own model-call budget (`max_turns` from its agent's frontmatter, default 50) and an optional cost cap (`agents.background.max_cost_usd`). The parent turn's limits stop at the parent turn.
- **BGA-07 Shell processes.** Background shells started by a task are tagged with the parent session, not with `subagent-…`, so the exit prompt and `ListProcesses` include them. This fixes a gap that exists today.

## 4. Tools the agent uses

- **BGA-10** `list_tasks()`: the tasks of this session, each with ID, agent, state, runtime, cost, and the first line of its result.
- **BGA-11** `task_output(task_id, wait_seconds?)`: the state and the final text, or while it runs, the last few events. `wait_seconds` (at most 300) blocks until the task ends or the wait runs out, so an agent can wait on purpose.
- **BGA-12** `stop_task(task_id)`: stops the task and returns its state.
- **BGA-13** All three only see tasks of the calling agent's session (`UNKNOWN_TASK` otherwise).

## 5. Results reaching the main agent

- **BGA-20 A turn is running in the parent session.** The engine adds a note to the next tool result, beside any steer messages. The note uses its own key (`task_updates`, not `message_from_user`, so the agent can tell it isn't the person), for example: "task-3 (qa) finished in 2m10s: <first 2,000 characters>. Use task_output for all of it."
- **BGA-21 The parent session is idle.** The note waits and goes in front of the next prompt, as hook context does today. In phase 2 an idle session can also start a turn by itself (BGA-32).
- **BGA-22 Transcript.** A finished, failed or stopped task adds a message to the parent's transcript, so the chat shows it after a restart. It uses a new message kind, `task`, with the ID, agent, state, cost and result. Tasks still running when the process ends are recorded as "stopped: Blitz exited".

## 6. The person's view

- **BGA-30 `/tasks` in the REPL.**
  - The list shows ID, agent, state (running, waiting for approval, done, failed, stopped), runtime and cost.
  - `/tasks show <id>` prints the task's events so far.
  - `/tasks stop <id>` stops it.
  - A task starting and finishing shows as a dim notice line.
- **BGA-31 Exiting.** The exit prompt counts running tasks alongside background processes, with the same kill, wait and cancel choices. Attached, they belong to the service and keep running after the client exits, as processes do, and the client lists only its own sessions' tasks.
- **BGA-32 Idle notification** (phase 2). A new server stream, `SessionService.WatchSession(workspace, session_id)`, carries events outside turns: a task started, needs approval, or finished. The REPL (while at its prompt) and the desktop app (while a chat is open) subscribe to it. When a task finishes in an idle session, the front end shows it. With `agents.background.continue = true` it also starts a turn, "Background task task-3 finished", so the main agent can act on the result (open question 2).
- **BGA-33 Desktop app.**
  - A background `invoke_agent` shows as a task card in the conversation (agent, state, live cost, **Show** and **Stop**), which updates in place until it finishes.
  - The run settings panel gets a **Tasks** section listing the session's tasks.

## 7. Approvals

The hard part: a task may need an approval while the person is doing something else, or after the turn that started it has ended.

- **BGA-40 Phase 1: unattended.** A task runs as workers do (`tools.Unattended`): what the workspace's mode and rules allow runs; anything that would ask is refused and returned to the task as a denial ("needs approval: shell `make deploy`; not asked, because it runs in the background"); `ask_user_question` is refused. The task can report what it needed, and the main agent can do that step itself in the foreground. This is safe, needs no new UI, and is useful at once for reviews, research and tests.
- **BGA-41 Phase 2: labelled approvals.**
  - Approval requests and questions gain an `agent` field (for example "qa · task-3"), in `api.ApprovalRequest` and in the protos.
  - A task's request goes to its parent session: through the running turn's stream if there is one, else through `WatchSession`.
  - While it waits, the task is `waiting_approval`, and the request is answered with the existing `Approve` and `Answer` calls.
  - With no client watching, it waits (the task's lifetime still applies) and is shown when a client next opens the session.
  - The desktop app keeps a queue of pending requests instead of one slot, and **Ctrl+J** moves to the next one (PAR-PAR-02). The REPL shows a task's request at its prompt, labelled, without interrupting a turn's own prompt.
- **BGA-42** The service's broker keys requests by ID and session rather than by turn only, so a request outlives the turn that started its task.

## 8. Phases

1. **Phase 1 (M): background tasks, unattended.** BGA-01–07, the three tools, delivery at the next tool cycle or prompt (BGA-20–22), `/tasks`, the exit prompt, the desktop task card (polled from `ListTasks`), and a `ListTasks`/`StopTask` RPC pair for the service.
2. **Phase 2 (M): watching and approvals.** `WatchSession`, labelled approvals and the queue, the broker change, idle notification and the optional continue turn.
3. **Phase 3 (S): agent defaults.** Agent frontmatter gains `permission_mode` (`default`, `plan`, `accept-edits`; `bypass` only inside the OS sandbox, as today), `max_turns` and `background` (the default for `invoke_agent`) (PAR-PAR-03). The mode is applied per run through the run's context rather than the workspace-wide mode.

Tests for each phase: the task manager's cap, lifetime and retention; stopping; delivery into a running turn and into the next prompt; usage charged to both the task and the session; nesting refused; unattended denials; and in phase 2, a request that outlives its turn and is answered through `WatchSession`, plus the service's scoping between clients.

## 9. Open questions for the owner

1. **Edits made in parallel.** A task and the main agent can edit the same files. *Recommendation:* allow it in phase 1. A task's edits become their own checkpoint entries ("task-3 (qa)") so `/undo` can revert them, and worktree isolation (§8.2, PAR-PAR-11) comes later for tasks that should be kept apart. The alternative is read-only tasks by default.
2. **Continuing when idle.** Should a finished task start a turn by itself when the session is idle (Claude Code does)? *Recommendation:* off by default (`agents.background.continue = false`), showing a notice instead, because an unexpected turn costs money.
3. **Keeping tasks across restarts.** *Recommendation:* no. A restart stops them and the transcript says so. Resuming a sub-agent run needs its session persisted, which is more than this needs.
4. **Unattended in phase 1.** Is refusing everything that would ask acceptable until phase 2? *Recommendation:* yes. Workers already work this way, and it keeps the first version small and safe.
