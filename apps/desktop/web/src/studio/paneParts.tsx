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

import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { mdiAlertCircleOutline, mdiUndoVariant } from "@mdi/js";
import { t } from "../i18n";
import { Button, Dialog, Icon } from "../ui/controls";
import { FormStamps } from "./formStamps";

// What Studio's sliding panes (Rules, Guardrails, Skills, Evals,
// Connectors) share: leaving a form with unsaved changes asks first, a
// form's problems, a confirmation, and replies that belong to the form
// they were asked for.

/**
 * Asks before leaving a form with unsaved changes (dirty): leave(go)
 * runs go at once, or once the person agrees to discard them;
 * discard(name) is the dialog, drawn where the pane draws its dialogs.
 */
export function useDiscardGuard(dirty: boolean): { leave: (go: () => void) => void; discard: (name: string) => ReactNode } {
  const [pending, setPending] = useState<(() => void) | null>(null);
  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());
  const discard = (name: string) =>
    pending && (
      <ConfirmDialog
        title={t("desktop.agents.discard_title")}
        icon={mdiUndoVariant}
        confirm={t("desktop.agents.discard")}
        onConfirm={() => {
          const go = pending;
          setPending(null);
          go();
        }}
        onClose={() => setPending(null)}
      >
        <p>{t("desktop.agents.discard_body", { name: name || t("desktop.agents.unnamed") })}</p>
      </ConfirmDialog>
    );
  return { leave, discard };
}

/** A confirmation over the page: its question (children), Cancel, and confirm (danger: in red). */
export function ConfirmDialog({ title, icon, confirm, danger = true, onConfirm, onClose, children }: { title: string; icon: string; confirm: string; danger?: boolean; onConfirm: () => void; onClose: () => void; children: ReactNode }) {
  return createPortal(
    <Dialog
      title={title}
      icon={icon}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" danger={danger} icon={danger ? undefined : icon} onClick={onConfirm}>
            {confirm}
          </Button>
        </>
      }
    >
      {children}
    </Dialog>,
    document.body,
  );
}

/** A form's problems, a line each; nothing when there are none. */
export function ProblemList({ problems }: { problems: string[] }) {
  if (problems.length === 0) return null;
  return (
    <div className="card error stack" role="alert" style={{ gap: 4 }}>
      {problems.map((p) => (
        <span key={p} className="row t-body-sm" style={{ gap: 6 }}>
          <Icon path={mdiAlertCircleOutline} size="sm" /> {p}
        </span>
      ))}
    </div>
  );
}

/** Replies that belong to the form they were asked for (FormStamps), one per pane. */
export function useFormStamp(): FormStamps {
  return useRef(new FormStamps()).current;
}

/**
 * A request about the open form (a test, a schema read): busy while it
 * runs, its result once it answers, unless the form it was asked for
 * isn't open any more (stamps.next() since), when it's dropped.
 */
export function useFormRequest<T>(stamps: FormStamps, failed: (e: unknown) => T): { busy: boolean; result: T | null; run: (ask: () => Promise<T>) => Promise<void>; reset: () => void } {
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<T | null>(null);
  const run = async (ask: () => Promise<T>) => {
    const s = stamps.stamp();
    setBusy(true);
    setResult(null);
    let r: T;
    try {
      r = await ask();
    } catch (e) {
      r = failed(e);
    }
    if (stamps.current(s)) {
      setResult(r);
      setBusy(false);
    }
  };
  const reset = () => {
    setBusy(false);
    setResult(null);
  };
  return { busy, result, run, reset };
}

