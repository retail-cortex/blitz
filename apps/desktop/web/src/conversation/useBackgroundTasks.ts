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

import { useEffect, useRef, useState, type Dispatch, type MutableRefObject, type RefObject, type SetStateAction } from "react";
import { sessions, workspaces } from "../api";
import { runInSession } from "../backgroundRuns";
import { configChanged } from "../events";
import type { ApprovalRequest, Decision, Question } from "../gen/blitz/v1/turn_pb";
import { t } from "../i18n";
import type { Entry } from "../turns";

/** A background task's approval request or question, waiting for an answer. */
export type TaskRequest = { id: string; who: string; approval?: ApprovalRequest; question?: Question };

/**
 * Watches background runs and background tasks in the current session,
 * surfacing their pending approval/question requests and Ctrl+J cycling.
 */
export function useBackgroundTasks({
  dir,
  sessionId,
  visible,
  running,
  continueOn,
  latestRunning,
  run,
  setEntries,
  scroller,
  fail,
}: {
  dir: string;
  sessionId?: string;
  visible: boolean;
  running: boolean;
  continueOn: boolean;
  latestRunning: MutableRefObject<boolean>;
  run: (text: string, opts?: { follow?: string }) => Promise<void>;
  setEntries: Dispatch<SetStateAction<Entry[]>>;
  scroller: RefObject<HTMLDivElement | null>;
  fail: (e: unknown) => void;
}) {
  const [taskRequests, setTaskRequests] = useState<TaskRequest[]>([]);
  const latest = useRef({ running, run, continueOn });
  latest.current = { running, run, continueOn };

  // A background run going on in this session (blitz --bg, the inbox;
  // spec_027_parity PAR-PAR-20) shows as a turn from its start: its
  // requests wait here, and Stop stops it.
  useEffect(() => {
    if (!sessionId) return;
    let gone = false;
    sessions
      .listBackground({})
      .then((r) => {
        const bg = runInSession(r.runs, sessionId);
        if (bg && !gone && !latest.current.running) latest.current.run("", { follow: bg.id });
      })
      .catch(() => {});
    return () => {
      gone = true;
    };
  }, [sessionId]);

  useEffect(() => {
    if (!sessionId) return;
    setTaskRequests([]);
    const ctl = new AbortController();
    (async () => {
      while (!ctl.signal.aborted) {
        try {
          for await (const res of workspaces.watchTasks({ workspace: dir, sessionIds: [sessionId] }, { signal: ctl.signal })) {
            const k = res.event?.kind;
            if (k?.case === "approvalRequest" || k?.case === "question") {
              const r: TaskRequest = k.case === "approvalRequest" ? { id: k.value.requestId, who: `${k.value.agent} · ${k.value.taskId}`, approval: k.value } : { id: k.value.requestId, who: `${k.value.agent} · ${k.value.taskId}`, question: k.value };
              setTaskRequests((list) => (list.some((x) => x.id === r.id) ? list : [...list, r]));
            } else if (k?.case === "settingsChanged") {
              configChanged({ dir }); // rules or approvals changed on disk: views refresh
            } else if (k?.case === "resolved") {
              setTaskRequests((list) => list.filter((x) => x.id !== k.value));
            } else if (k?.case === "task" && ["done", "failed", "stopped"].includes(k.value.state)) {
              const task = k.value;
              setEntries((e) => [...e, { kind: "notice", text: t("desktop.task.ended", { id: task.id, agent: task.agent, state: t(`tasks.state.${task.state}`) }), tone: task.state === "failed" ? "error" : "info" }]);
              // The ref, not the rendered state: run sets it at once, so two
              // tasks ending together start one turn, not two.
              const { run: go, continueOn: cont } = latest.current;
              if (cont && !latestRunning.current) go(t("desktop.task.continue_prompt", { id: task.id }));
            }
          }
        } catch {
          // The service went away, or the stream broke: follow again soon.
        }
        if (!ctl.signal.aborted) await new Promise((r) => setTimeout(r, 3000));
      }
    })();
    return () => ctl.abort();
  }, [dir, sessionId, latestRunning, setEntries]);

  const answerTask = async (r: TaskRequest, reply: { decision?: Decision; text?: string }) => {
    setTaskRequests((list) => list.filter((x) => x.id !== r.id));
    const call = reply.text !== undefined ? sessions.answer({ workspace: dir, requestId: r.id, answer: reply.text }) : sessions.approve({ workspace: dir, requestId: r.id, decision: reply.decision! });
    await call.catch(fail);
  };

  // Ctrl+J: the next background task's request.
  useEffect(() => {
    if (!visible) return;
    const key = (e: KeyboardEvent) => {
      if (!e.ctrlKey || e.metaKey || e.key.toLowerCase() !== "j") return;
      const cards = [...(scroller.current?.querySelectorAll<HTMLElement>(".task-request") ?? [])];
      if (!cards.length) return;
      e.preventDefault();
      const at = cards.findIndex((c) => c.contains(document.activeElement));
      const next = cards[(at + 1) % cards.length];
      next.scrollIntoView({ block: "center" });
      next.querySelector<HTMLElement>("button, input")?.focus();
    };
    document.addEventListener("keydown", key);
    return () => document.removeEventListener("keydown", key);
  }, [visible, scroller]);

  return { taskRequests, answerTask };
}
