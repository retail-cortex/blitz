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


// Asking before the service is stopped or restarted while workflow runs
// or jobs' runs go on in it: stopping it stops them (spec_043_workflows
// WF-19). An eval run is stopped too, and recorded as stopped (EV-07a);
// the service has no list of them across workspaces to ask about.

import { useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { mdiAlertOutline } from "@mdi/js";
import { workers } from "./api";
import { t, tn } from "./i18n";
import { Button, Dialog } from "@blitz/webui";
import { isGoing, useWorkflowRuns } from "./workflowRuns";

/** The jobs whose runs are going in the service ([] when it can't say). */
export async function runningJobs(): Promise<string[]> {
  try {
    return [...new Set((await workers.listRunningWorkerRuns({})).runs.map((r) => r.worker))];
  } catch {
    return [];
  }
}

/**
 * guard runs action at once when no run is going (a workflow's or a
 * job's), else after the user says to stop the runs; dialog is what asks
 * (render it).
 */
export function useStopGuard(): { guard: (action: () => void) => void; dialog: ReactNode } {
  const { runs } = useWorkflowRuns();
  const going = runs.filter(isGoing);
  const [jobs, setJobs] = useState<string[]>([]);
  const [pending, setPending] = useState<(() => void) | null>(null);
  const guard = (action: () => void) =>
    void runningJobs().then((names) => {
      if (!going.length && !names.length) return action();
      setJobs(names);
      setPending(() => action);
    });
  const close = () => setPending(null);
  const dialog =
    pending &&
    createPortal(
      <Dialog
        title={t("desktop.service.stop_runs_title")}
        icon={mdiAlertOutline}
        onClose={close}
        footer={
          <>
            <Button onClick={close}>{t("desktop.cancel")}</Button>
            <Button
              variant="filled"
              danger
              onClick={() => {
                close();
                pending();
              }}
            >
              {t("desktop.service.stop_runs_go")}
            </Button>
          </>
        }
      >
        <p>{tn("desktop.service.stop_runs_body", going.length + jobs.length)}</p>
        <ul>
          {going.map((r) => (
            <li key={r.id} className="mono">
              /{r.workflow}
            </li>
          ))}
          {jobs.map((name) => (
            <li key={`job:${name}`}>{t("desktop.service.stop_runs_job", { name })}</li>
          ))}
        </ul>
      </Dialog>,
      document.body,
    );
  return { guard, dialog };
}
