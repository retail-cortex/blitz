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
// go on in it: stopping it stops them (spec_workflows_043 WF-19).

import { useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { mdiAlertOutline } from "@mdi/js";
import { t, tn } from "./i18n";
import { Button, Dialog } from "./ui/controls";
import { isGoing, useWorkflowRuns } from "./workflowRuns";

/**
 * guard runs action at once when no run is going, else after the user
 * says to stop the runs; dialog is what asks (render it).
 */
export function useStopGuard(): { guard: (action: () => void) => void; dialog: ReactNode } {
  const { runs } = useWorkflowRuns();
  const going = runs.filter(isGoing);
  const [pending, setPending] = useState<(() => void) | null>(null);
  const guard = (action: () => void) => (going.length ? setPending(() => action) : action());
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
        <p>{tn("desktop.service.stop_runs_body", going.length)}</p>
        <ul>
          {going.map((r) => (
            <li key={r.id} className="mono">
              /{r.workflow}
            </li>
          ))}
        </ul>
      </Dialog>,
      document.body,
    );
  return { guard, dialog };
}
