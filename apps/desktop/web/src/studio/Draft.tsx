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

import { useState } from "react";
import { mdiCreationOutline } from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import { t } from "../i18n";
import { Button, Dialog } from "../ui/controls";

/** What a draft is of (spec_studio_040 STU-05). */
export type DraftKind = "agent" | "skill" | "script" | "rule" | "guardrail" | "eval" | "workflow_step";

/**
 * Redraft: the model writes or improves a field (an agent's instructions,
 * a skill's, a script) from what's there and what the user asks; the
 * draft is shown with what changed, and Use puts it in the form. Nothing
 * is saved until the form is.
 */
export function DraftButton({
  workspace,
  kind,
  name,
  description,
  current,
  language,
  onUse,
  disabled,
}: {
  workspace: string;
  kind: DraftKind;
  name: string;
  description: string;
  current: string;
  language?: string;
  onUse: (text: string) => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [request, setRequest] = useState("");
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<{ text: string; changes: string[] } | null>(null);
  const [error, setError] = useState("");
  const close = () => {
    setOpen(false);
    setDraft(null);
    setError("");
  };
  const ask = async () => {
    setBusy(true);
    setError("");
    try {
      const r = await workspaces.draftManifest({ workspace, kind, name, description, current, request: request.trim(), language: language ?? "" });
      if (r.problem) setError(r.problem);
      else setDraft({ text: r.text, changes: r.changes });
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <Button small icon={mdiCreationOutline} disabled={disabled} onClick={() => setOpen(true)} title={t("desktop.draft.hint")}>
        {t(current.trim() ? "desktop.draft.redraft" : "desktop.draft.draft")}
      </Button>
      {open && (
        <Dialog
          title={t("desktop.draft.title")}
          icon={mdiCreationOutline}
          onClose={close}
          wide
          footer={
            draft ? (
              <>
                <Button onClick={() => setDraft(null)}>{t("desktop.draft.again")}</Button>
                <Button
                  variant="filled"
                  onClick={() => {
                    onUse(draft.text);
                    close();
                  }}
                >
                  {t("desktop.draft.use")}
                </Button>
              </>
            ) : (
              <>
                <Button onClick={close}>{t("desktop.cancel")}</Button>
                <Button variant="filled" icon={mdiCreationOutline} disabled={busy} onClick={() => void ask()}>
                  {busy ? t("desktop.draft.drafting") : t("desktop.draft.ask")}
                </Button>
              </>
            )
          }
        >
          {draft ? (
            <div className="stack" style={{ gap: 12 }}>
              {draft.changes.length > 0 && (
                <ul className="t-body-sm draft-changes">
                  {draft.changes.map((c) => (
                    <li key={c}>{c}</li>
                  ))}
                </ul>
              )}
              <textarea className="input mono draft-text" readOnly value={draft.text} rows={14} />
            </div>
          ) : (
            <label className="field">
              <span className="t-label">{t("desktop.draft.request")}</span>
              <textarea className="input" rows={3} autoFocus value={request} placeholder={t(current.trim() ? "desktop.draft.request_improve" : "desktop.draft.request_new")} onChange={(e) => setRequest(e.target.value)} />
            </label>
          )}
          {error && <p className="error-text">{error}</p>}
        </Dialog>
      )}
    </>
  );
}
