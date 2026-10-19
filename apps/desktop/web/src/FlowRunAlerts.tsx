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

import { mdiAlertCircleOutline, mdiLoading, mdiSitemapOutline } from "@mdi/js";
import { useCallback, useEffect, useState } from "react";
import { showView } from "./events";
import type { WorkflowRun } from "./gen/blitz/v1/workspace_pb";
import { t, tn } from "./i18n";
import { notify } from "./notify";
import { openWorkspace } from "./prefs";
import { useApp } from "./state";
import { Icon, Menu, useSnackbar } from "./ui/controls";
import { cost, failedStep, isGoing, ms, onRunEnded, progress, seeRun, took, unseenFailures, useWorkflowRuns, watchedRuns } from "./workflowRuns";

/** Opens a run on its workflow's canvas (Studio › Workflows), in its workspace. */
export function useOpenRun() {
  const { update } = useApp();
  return useCallback(
    (r: Pick<WorkflowRun, "workspace" | "workflow" | "id">) => {
      seeRun(r.id);
      update((p) => openWorkspace(p, r.workspace));
      showView({ dir: r.workspace, view: "workflows", workflow: r.workflow, run: r.id });
    },
    [update],
  );
}

/** What a run that ended says: the step it failed at and why, or that it finished. */
export function endText(r: WorkflowRun): string {
  if (r.status === "failed") {
    const step = failedStep(r);
    return t("desktop.flow_runs.failed_body", { step: step?.title || step?.id || "?", error: step?.error || r.error });
  }
  if (r.status === "stopped") return t("desktop.flow_runs.stopped_body");
  return t("desktop.flow_runs.done_body", { time: took(ms(r.started), ms(r.ended)), cost: cost(r.usage?.costUsd, r.usage?.priced) });
}

/**
 * Says when a workflow run ends, wherever it was started and whatever's
 * shown (spec_workflows_043 WF-19): a failure always (a snackbar with Open,
 * or a system notification when the window isn't in front); a run started
 * from the app (the canvas, a chat) finishing or stopping, briefly. A
 * worker's run that succeeds says nothing (its worker keeps the record).
 */
export function FlowRunAlerts() {
  useWorkflowRuns(); // keeps the stream going
  const snack = useSnackbar();
  const openRun = useOpenRun();
  useEffect(
    () =>
      onRunEnded((r) => {
        const failed = r.status === "failed";
        if (!failed && r.origin !== "background") return; // a chat shows its own; a worker keeps its record
        if (watchedRuns.has(r.id) && document.hasFocus()) return; // its canvas shows it
        const title = t(`desktop.flow_runs.${failed ? "failed" : r.status === "stopped" ? "stopped" : "done"}`, { name: r.workflow });
        if (failed && !document.hasFocus()) return notify(title, endText(r), r.workspace);
        snack(`${title} ${endText(r)}`, { error: failed, action: { label: t("desktop.flow_runs.open"), run: () => openRun(r) } });
      }),
    [snack, openRun],
  );
  return null;
}

/**
 * The status bar's workflow runs: those going in any workspace (how far
 * each has got, how long it's taken, what it's spent), each opened on its
 * canvas. A failure lately shows until it's opened or a while passes.
 */
export function FlowRunsItem() {
  const { runs } = useWorkflowRuns();
  const openRun = useOpenRun();
  const [, tick] = useState(0);
  const going = runs.filter((r) => r.status === "running");
  const failed = unseenFailures(runs);
  useEffect(() => {
    if (going.length === 0) return;
    const id = setInterval(() => tick((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, [going.length]);
  const shown = runs.filter((r) => isGoing(r) || failed.includes(r));
  if (going.length === 0 && failed.length === 0) return null;
  const now = Date.now();
  const label = going.length ? tn("desktop.flow_runs.running", going.length) : tn("desktop.flow_runs.failed_lately", failed.length);
  return (
    <Menu
      placement="up end"
      className="flow-runs-menu"
      trigger={(p) => (
        <button className={`sb-item sb-button ${going.length ? "" : "sb-error"}`} title={label} aria-label={label} {...p}>
          <Icon path={going.length ? mdiLoading : mdiAlertCircleOutline} spin={going.length > 0} size="sm" />
          {going.length === 1 ? (
            <span className="sb-flow">
              /{going[0].workflow} <span className="muted">{t("desktop.flow_runs.steps_short", progress(going[0]))}</span>
            </span>
          ) : (
            <span>{going.length || failed.length}</span>
          )}
        </button>
      )}
      items={[
        { heading: t("desktop.flow_runs.heading") },
        ...shown.map((r) => {
          const { done, started } = progress(r);
          const detail =
            r.status === "failed"
              ? endText(r)
              : [r.status === "waiting" ? t("desktop.flow_runs.waiting") : t("desktop.flow_runs.steps", { done, started }), took(ms(r.started), r.ended ? ms(r.ended) : now), cost(r.usage?.costUsd, r.usage?.priced), r.approvals ? tn("desktop.flow_runs.approvals", r.approvals) : ""]
                  .filter(Boolean)
                  .join(" · ");
          return { label: `/${r.workflow}`, detail, icon: r.status === "failed" ? mdiAlertCircleOutline : mdiSitemapOutline, danger: r.status === "failed", onSelect: () => openRun(r) };
        }),
      ]}
    />
  );
}
