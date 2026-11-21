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

import { useEffect, useRef, useState, type RefObject } from "react";
import type { Edge, Node, ReactFlowInstance } from "@xyflow/react";
import { sessions, workspaces } from "../api";
import { message, reasonText } from "../errors";
import { Decision, type ApprovalRequest } from "../gen/blitz/v1/turn_pb";
import type { HumanTask, WorkflowFile, WorkflowRun } from "../gen/blitz/v1/workspace_pb";
import { answerTask, useTasks } from "../humanTasks";
import type { ItemIntent } from "../intent";
import { goingRun, seeRun, useWorkflowRuns, watchedRuns } from "../workflowRuns";
import type { FormAction } from "../a2ui/model";
import type { StepData } from "./WorkflowCanvasParts";
import { formFromWorkflow, type WorkflowForm } from "./workflowForm";

/**
 * Manages the active workflow run's receipt, approval prompts, human tasks,
 * run history, and background execution streams for the Workflows pane.
 */
export function useWorkflowRunState({
  workspace,
  open,
  list,
  intent,
  runToShow,
  flowRef,
  show,
  snack,
}: {
  workspace: string;
  open: string | null;
  list: WorkflowFile[];
  intent?: ItemIntent;
  runToShow?: string;
  flowRef: RefObject<ReactFlowInstance<Node<StepData>, Edge> | null>;
  show: (name: string, f: WorkflowForm, saved: boolean) => void;
  snack: (text: string, opts?: { error?: boolean }) => void;
}) {
  const [asking, setAsking] = useState(false);
  const [input, setInput] = useState("");
  const [shownRun, setShownRun] = useState("");
  const [receipt, setReceipt] = useState<WorkflowRun | null>(null);
  const [approval, setApproval] = useState<ApprovalRequest | null>(null);
  const dismissed = useRef(new Set<string>());
  const [history, setHistory] = useState<WorkflowRun[]>([]);
  const { runs } = useWorkflowRuns();
  const { tasks } = useTasks();

  // A workflow asked for (the status bar, an alert, a notification): open
  // it, and the run named on it.
  const handled = useRef(0);
  useEffect(() => {
    if (!intent || intent.n === handled.current || !intent.id) return;
    const w = list.find((x) => x.name === intent.id);
    if (!w) return;
    handled.current = intent.n;
    show(w.name, formFromWorkflow(w), true);
    if (runToShow) {
      dismissed.current.delete(runToShow);
      setShownRun(runToShow);
    }
    // show is the pane's own, made each render
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [intent, list, runToShow]);

  // A workflow opened while a run of it goes (started anywhere) shows that
  // run, unless it was closed here.
  const going = open ? goingRun(runs, workspace, open) : undefined;
  useEffect(() => {
    if (going && !shownRun && !dismissed.current.has(going.id)) setShownRun(going.id);
  }, [going, shownRun]);

  // Follows the shown run's receipt while it runs; a paused run that
  // resumes (its task answered, its timer fired) is followed again.
  const storeStatus = runs.find((r) => r.id === shownRun)?.status;
  useEffect(() => {
    if (!shownRun) {
      setReceipt(null);
      return;
    }
    const ctl = new AbortController();
    void (async () => {
      try {
        for await (const r of workspaces.watchWorkflowRun({ workspace, id: shownRun }, { signal: ctl.signal })) if (r.run) setReceipt(r.run);
      } catch (e) {
        if (!ctl.signal.aborted) snack(message(e), { error: true });
      }
    })();
    return () => ctl.abort();
  }, [shownRun, workspace, storeStatus, snack]);

  // A run shown takes room under the canvas: the workflow is fitted again
  // in what's left.
  const hasRun = !!receipt;
  useEffect(() => {
    if (!hasRun) return;
    const id = setTimeout(() => void flowRef.current?.fitView({ padding: 0.15, maxZoom: 1, minZoom: 0.55, duration: 200 }), 300);
    return () => clearTimeout(id);
  }, [hasRun, shownRun, flowRef]);

  // While it's shown here, the app doesn't also announce its end.
  useEffect(() => {
    if (!shownRun) return;
    watchedRuns.add(shownRun);
    return () => void watchedRuns.delete(shownRun);
  }, [shownRun]);

  // A failure seen here leaves the status bar.
  const shownFailed = receipt?.status === "failed" ? receipt.id : "";
  useEffect(() => {
    if (shownFailed) seeRun(shownFailed);
  }, [shownFailed]);

  // The approvals the run's background run waits on, asked for here.
  const bgId = receipt?.status === "running" ? receipt.backgroundId : "";
  useEffect(() => {
    setApproval(null);
    if (!bgId) return;
    const ctl = new AbortController();
    void (async () => {
      try {
        for await (const res of sessions.watchBackground({ id: bgId, follow: true }, { signal: ctl.signal })) {
          const k = res.event?.kind;
          if (k?.case === "approvalRequest") setApproval(k.value);
          else if (k?.case === "finished") setApproval(null);
        }
      } catch {
        // it ended, or the service is going: the receipt says how
      }
    })();
    return () => ctl.abort();
  }, [bgId]);

  // The workflow's past runs, for the history menu, again as runs end.
  const endedCount = runs.filter((r) => r.workflow === open && r.status !== "running").length;
  useEffect(() => {
    if (!open) return setHistory([]);
    workspaces.listWorkflowRuns({ workspace, workflow: open, limit: 15 }).then(
      (r) => setHistory(r.runs),
      () => setHistory([]),
    );
  }, [open, workspace, endedCount, receipt?.status]);

  // The streams following runs just started, until their first step:
  // ended when the workflow or Studio closes, so a run that waits before
  // its first step holds no stream open (the run itself goes on).
  const following = useRef(new Set<AbortController>());
  useEffect(() => {
    const live = following.current;
    return () => {
      for (const ctl of live) ctl.abort();
      live.clear();
    };
  }, [open]);

  // Runs the workflow as saved, in the background, as /name input: its
  // receipt shows as its first step starts. It goes on with Studio closed.
  const run = async (name: string) => {
    setAsking(false);
    const ctl = new AbortController();
    following.current.add(ctl);
    try {
      const started = await sessions.startBackground({ workspace, turn: { text: `/${name} ${input}`.trim(), command: true } });
      const id = started.run?.id ?? "";
      if (!id || ctl.signal.aborted) return;
      for await (const res of sessions.watchBackground({ id, follow: true }, { signal: ctl.signal })) {
        const k = res.event?.kind;
        if (k?.case === "flowStep" && k.value.run) {
          dismissed.current.delete(k.value.run);
          setShownRun(k.value.run);
          return;
        }
        if (k?.case === "finished") {
          const err = k.value.error;
          if (err) snack(err.message || reasonText(err.reason), { error: true });
          return;
        }
      }
    } catch (e) {
      if (!ctl.signal.aborted) snack(message(e), { error: true });
    } finally {
      following.current.delete(ctl);
    }
  };

  // Answers the human task the run waits on: it resumes in the background.
  const answer = async (task: HumanTask, action: FormAction) => {
    try {
      await answerTask(task, action);
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const halt = async () => {
    try {
      await workspaces.stopWorkflowRun({ workspace, id: shownRun });
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const decide = async (d: Decision) => {
    const req = approval;
    if (!req) return;
    setApproval(null);
    try {
      await sessions.approve({ workspace, requestId: req.requestId, decision: d });
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const closeRun = () => {
    dismissed.current.add(shownRun);
    setShownRun("");
  };

  const showHistoryRun = (id: string) => {
    dismissed.current.delete(id);
    setShownRun(id);
  };

  const running = receipt?.status === "running";
  const task = receipt?.status === "waiting" ? tasks.find((x) => x.run === receipt.id) : undefined;

  return {
    runs,
    asking,
    setAsking,
    input,
    setInput,
    shownRun,
    setShownRun,
    receipt,
    approval,
    history,
    running,
    task,
    run,
    answer,
    halt,
    decide,
    closeRun,
    showHistoryRun,
  };
}
