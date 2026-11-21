/**
 * Copyright 2026 Retail Cortex
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import { useCallback, useEffect, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import {
  mdiCalendarClock,
  mdiCheckboxBlankOutline,
  mdiCheckboxMarked,
  mdiChevronDown,
  mdiChevronRight,
  mdiClose,
  mdiFormatListChecks,
  mdiHeadLightbulbOutline,
  mdiProgressClock,
  mdiRobotOutline,
} from "@mdi/js";
import { Button, Icon, IconButton, useSnackbar } from "@blitz/webui";
import { workers, workspaces } from "../api";
import { clock } from "../attachments";
import { message } from "../errors";
import { showView, startReflect } from "../events";
import type { Task } from "../gen/blitz/v1/turn_pb";
import { WorkerState, type Worker } from "../gen/blitz/v1/worker_pb";
import type { BackgroundTask } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";
import { Markdown } from "../Markdown";
import { taskTimes, useNow, type TaskTime } from "../taskTimes";

/**
 * A background task (invoke_agent with background: true): its agent,
 * state, runtime and cost, followed until it ends, with Show (its latest
 * events and result) and Stop (spec_032_background_agents BGA-33).
 */
export function TaskCard({ dir, sessionId, id }: { dir: string; sessionId: string; id: string }) {
  const snack = useSnackbar();
  const [task, setTask] = useState<BackgroundTask>();
  const [events, setEvents] = useState<string[]>([]);
  const [open, setOpen] = useState(false);
  const [gone, setGone] = useState(false);
  const load = useCallback(async () => {
    try {
      const r = await workspaces.getTask({ workspace: dir, sessionIds: [sessionId], id });
      setTask(r.task);
      setEvents(r.events);
    } catch {
      setGone(true); // the service restarted, or it's another client's
    }
  }, [dir, sessionId, id]);
  useEffect(() => {
    load();
  }, [load]);
  const runningNow = !task || task.state === "running" || task.state === "waiting";
  useEffect(() => {
    if (!runningNow || gone) return;
    const timer = setInterval(load, 2000);
    return () => clearInterval(timer);
  }, [runningNow, gone, load]);
  const now = useNow(runningNow && !gone);
  if (gone) return null;
  const stop = async () => {
    try {
      const r = await workspaces.stopTask({ workspace: dir, sessionIds: [sessionId], id });
      setTask(r.task);
    } catch (e) {
      snack(message(e));
    }
  };
  const started = task?.started ? timestampDate(task.started).getTime() : now;
  const ended = task?.ended ? timestampDate(task.ended).getTime() : now;
  const seconds = Math.max(0, (ended - started) / 1000);
  return (
    <div className={`card task-card ${task?.state ?? "running"}`}>
      <div className="row">
        <Icon path={mdiRobotOutline} size="sm" />
        <span className="ellipsis" style={{ flex: 1 }}>
          <b>{task?.agent}</b> · {id} · {t(`tasks.state.${task?.state ?? "running"}`)} · <span className="mono">{clock(seconds)}</span>
          {task?.usage?.costUsd ? ` · $${task.usage.costUsd.toFixed(2)}` : ""}
        </span>
        {runningNow && <Icon path={mdiProgressClock} size="sm" className="pulse" />}
        <Button small onClick={() => setOpen((o) => !o)}>
          {open ? t("desktop.task.hide") : t("desktop.task.show")}
        </Button>
        {runningNow && (
          <Button small onClick={stop}>
            {t("desktop.task.stop")}
          </Button>
        )}
      </div>
      {task?.prompt && <div className="muted ellipsis">{task.prompt}</div>}
      {task?.branch && <div className="muted ellipsis mono">{t("tasks.worktree", { path: task.worktree, branch: task.branch })}</div>}
      {open && (
        <div className="task-body">
          {events.length > 0 && <pre className="json">{events.join("\n")}</pre>}
          {task?.result && <Markdown text={task.result} />}
          {task?.error && <p className="error-text">{task.error}</p>}
        </div>
      )}
    </div>
  );
}

/**
 * The chat's suggestion to reflect (spec_048_harness_learning HE-63): the
 * runs since the last reflection hit the same problems. Reflect runs
 * /reflect in a new chat; Not now holds the suggestion off for a week.
 */
export function ReflectCard({ dir, patterns }: { dir: string; patterns: number }) {
  const snack = useSnackbar();
  const [done, setDone] = useState(false);
  if (done) return null;
  const later = async () => {
    try {
      await workspaces.snoozeReflection({ workspace: dir });
      setDone(true);
      snack(t("desktop.reflectcard.snoozed"));
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  return (
    <div className="card task-card">
      <div className="row">
        <Icon path={mdiHeadLightbulbOutline} size="sm" />
        <span style={{ flex: 1 }}>
          <b>{t("desktop.suggest.reflect")}</b>
          <span className="t-body-sm muted"> · {tn("desktop.suggest.reflect_detail", patterns)}</span>
        </span>
        <Button small onClick={() => void later()}>
          {t("desktop.reflectcard.not_now")}
        </Button>
        <Button
          small
          variant="filled"
          onClick={() => {
            setDone(true);
            startReflect(dir);
          }}
        >
          {t("desktop.reflectcard.reflect")}
        </Button>
      </div>
    </div>
  );
}

/**
 * A job the agent made from the chat (create_job, spec_023_workers WK-70):
 * paused until it's reviewed, with Review (Studio › Jobs, its details) and
 * Enable here; once enabled, when it next runs.
 */
export function JobCard({ dir, name }: { dir: string; name: string }) {
  const snack = useSnackbar();
  const [job, setJob] = useState<Worker>();
  const [gone, setGone] = useState(false);
  const load = useCallback(async () => {
    try {
      const r = await workers.listWorkers({ workspace: dir });
      const w = r.workers.find((x) => x.name === name);
      if (w) setJob(w);
      else setGone(true); // deleted since
    } catch {
      setGone(true);
    }
  }, [dir, name]);
  useEffect(() => {
    void load();
  }, [load]);
  if (gone || !job) return null;
  const enabled = job.state === WorkerState.ENABLED;
  const enable = async () => {
    try {
      const r = await workers.enableWorker({ workspace: dir, name, hash: job.hash });
      if (r.worker) setJob(r.worker);
      snack(t("desktop.jobcard.enabled", { name }));
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  return (
    <div className="card task-card">
      <div className="row">
        <Icon path={mdiCalendarClock} size="sm" />
        <span className="ellipsis" style={{ flex: 1 }}>
          <b>{name}</b> · {job.schedule}
          {" · "}
          {enabled && job.nextRun ? t("desktop.jobcard.next", { when: timestampDate(job.nextRun).toLocaleString() }) : t(enabled ? "desktop.jobcard.on" : "desktop.jobcard.paused")}
        </span>
        <Button small onClick={() => showView({ dir, view: "workers", worker: name })}>
          {t("desktop.jobcard.review")}
        </Button>
        {!enabled && job.state !== WorkerState.INVALID && (
          <Button small variant="tonal" onClick={() => void enable()}>
            {t("desktop.jobcard.enable")}
          </Button>
        )}
      </div>
      {job.permissions.length > 0 && <div className="muted ellipsis mono">{job.permissions.join("  ·  ")}</div>}
      {job.problems.length > 0 && <div className="error-text t-body-sm">{job.problems.join("; ")}</div>}
    </div>
  );
}

// How long a task list item has run, or ran, as m:ss.
const ranFor = (time: TaskTime, now: number) => clock(((time.end ?? now) - time.start) / 1000);

/** Collapsible progress checklist for the agent's current turn tasks. */
export function TaskList({ tasks, onDismiss }: { tasks: Task[]; onDismiss?: () => void }) {
  const done = tasks.filter((x) => x.status === "done").length;
  const allDone = done === tasks.length;
  const [open, setOpen] = useState(true);
  useEffect(() => setOpen(!allDone), [allDone]); // folds itself once every task is done
  // How long each item has run (in progress, ticking) or ran (done).
  const [times, setTimes] = useState<Map<string, TaskTime>>(new Map());
  useEffect(() => setTimes((prev) => taskTimes(prev, tasks, Date.now())), [tasks]);
  const now = useNow(tasks.some((x) => x.status === "in_progress"));
  return (
    <div className="tasks card outlined">
      <div className="row">
        <button className="tasks-head" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
          <Icon path={mdiFormatListChecks} size="sm" />
          <span className="t-title-sm">{t("desktop.tasks.title")}</span>
          <span className="muted t-body-sm">
            {t("desktop.tasks.progress", { done, total: tasks.length })}
          </span>
          <Icon path={open ? mdiChevronDown : mdiChevronRight} size="sm" />
        </button>
        <span className="spacer" />
        {onDismiss && <IconButton icon={mdiClose} label={t("desktop.tasks.hide")} small onClick={onDismiss} />}
      </div>
      <div className="progress-bar">
        <span style={{ width: `${(done / tasks.length) * 100}%` }} />
      </div>
      {open && (
        <ul className="task-items">
          {tasks.map((task, i) => (
            <li key={i} className={task.status}>
              <Icon path={task.status === "done" ? mdiCheckboxMarked : task.status === "in_progress" ? mdiProgressClock : mdiCheckboxBlankOutline} size="sm" className={task.status === "in_progress" ? "pulse" : ""} />
              <span>{task.content}</span>
              {times.has(task.content) && <span className="muted mono task-time">{ranFor(times.get(task.content)!, now)}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
