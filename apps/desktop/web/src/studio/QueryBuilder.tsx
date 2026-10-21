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
import { createPortal } from "react-dom";
import { mdiAlertCircleOutline, mdiCreationOutline, mdiLoading } from "@mdi/js";
import { message } from "../errors";
import { t, tn } from "../i18n";
import { Button, Dialog, Icon } from "../ui/controls";
import { agentTool, type ConnectorApi } from "./connectorApi";
import { connectionFromForm, queryForm, type ConnectorForm, type QueryForm } from "./connectorForm";

/**
 * The query builder (the query-builder capability): the user says what
 * they're looking for; the service reads the connection's tables, failing
 * when it can read none, and the sql-query skill writes a query. The draft
 * is shown with its notes, and Use adds it to the form, unsaved.
 */
export function QueryBuilder({ api, form, prefix, onClose, onUse }: { api: ConnectorApi; form: ConnectorForm; prefix: string; onClose: () => void; onUse: (q: QueryForm) => void }) {
  const [request, setRequest] = useState("");
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<{ query: QueryForm; notes: string[]; tables: number } | null>(null);
  const [error, setError] = useState("");
  const ask = async () => {
    setBusy(true);
    setError("");
    try {
      const r = await api.draft({ ...connectionFromForm(form), request: request.trim() });
      if (r.problem || !r.query) setError(r.problem);
      else setDraft({ query: queryForm(r.query), notes: r.notes, tables: r.tables });
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };
  return createPortal(
    <Dialog
      title={t("desktop.connectors.builder")}
      icon={mdiCreationOutline}
      onClose={onClose}
      wide
      footer={
        draft ? (
          <>
            <Button onClick={() => setDraft(null)}>{t("desktop.draft.again")}</Button>
            <Button variant="filled" onClick={() => onUse(draft.query)}>
              {t("desktop.draft.use")}
            </Button>
          </>
        ) : (
          <>
            <Button onClick={onClose}>{t("desktop.cancel")}</Button>
            <Button variant="filled" icon={busy ? mdiLoading : mdiCreationOutline} iconSpin={busy} disabled={busy || !request.trim()} onClick={() => void ask()}>
              {busy ? t("desktop.connectors.builder_busy") : t("desktop.connectors.builder_button")}
            </Button>
          </>
        )
      }
    >
      {draft ? (
        <div className="stack" style={{ gap: 12 }}>
          <span className="t-body-sm">{tn("desktop.connectors.builder_done", draft.tables, { name: agentTool(prefix, `${form.name.trim()}_${draft.query.name}`) })}</span>
          {draft.notes.length > 0 && (
            <ul className="t-body-sm draft-changes">
              {draft.notes.map((n) => (
                <li key={n}>{n}</li>
              ))}
            </ul>
          )}
          <span className="t-body-sm muted">{draft.query.description}</span>
          <textarea className="input mono draft-text" readOnly value={draft.query.statement} rows={8} />
          {draft.query.params.map((p) => (
            <span key={p.name} className="row t-body-sm" style={{ gap: 6 }}>
              <span className="mono">{p.name}</span>
              <span className="muted">{t(`desktop.connectors.param_type.${p.type}`)}</span>
              <span className="muted ellipsis spacer">{p.description}</span>
            </span>
          ))}
        </div>
      ) : (
        <div className="stack" style={{ gap: 8 }}>
          <span className="t-body-sm muted">{t("desktop.connectors.builder_hint")}</span>
          <label className="field">
            <span className="t-label">{t("desktop.draft.request")}</span>
            <textarea className="input" rows={3} autoFocus value={request} placeholder={t("desktop.connectors.builder_placeholder")} onChange={(e) => setRequest(e.target.value)} />
          </label>
          {error && (
            <div className="card error row small-card t-body-sm" role="alert" style={{ gap: 6 }}>
              <Icon path={mdiAlertCircleOutline} size="sm" /> {t("desktop.connectors.builder_failed", { problem: error })}
            </div>
          )}
        </div>
      )}
    </Dialog>,
    document.body,
  );
}
