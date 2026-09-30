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

import { useEffect, useState } from "react";
import { mdiShieldAlertOutline } from "@mdi/js";
import { workspaces } from "./api";
import { message } from "./errors";
import type { ProjectItem, ProjectSettings } from "./gen/blitz/v1/workspace_pb";
import { t } from "./i18n";
import { projectItemText, projectReason, projectState } from "./project";
import { Button, Dialog, useSnackbar } from "./ui/controls";

/**
 * A workspace's project settings (.blitz/settings.toml): what they would
 * do that needs trust, what applies without it, and what was ignored,
 * with Trust and Don't trust (spec_project_config_031 PRJ-33). Shown when
 * a workspace opens with settings waiting for a decision, from the bar
 * above the conversation, and from Settings › Workspaces.
 */
export function ProjectDialog({ dir, onClose, onDecided }: { dir: string; onClose: () => void; onDecided?: () => void }) {
  const snack = useSnackbar();
  const [p, setP] = useState<ProjectSettings>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    workspaces.getProjectSettings({ workspace: dir }).then(
      (r) => setP(r.settings),
      (e) => setError(message(e)),
    );
  }, [dir]);

  const decide = async (trusted: boolean) => {
    if (!p) return;
    setBusy(true);
    setError("");
    try {
      const r = await workspaces.trustProject({ workspace: dir, hash: p.hash, trusted });
      snack(trusted ? (r.reopened ? t("desktop.project.trusted_now") : t("desktop.project.trusted_later")) : t("desktop.project.declined"));
      onDecided?.();
      onClose();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };

  const canDecide = !!p && p.pending.length > 0 && p.hash !== "";
  const footer = canDecide ? (
    <>
      <Button disabled={busy} onClick={() => decide(false)}>
        {t("desktop.project.decline")}
      </Button>
      <Button variant="filled" disabled={busy} onClick={() => decide(true)}>
        {t("desktop.project.trust")}
      </Button>
    </>
  ) : (
    <Button onClick={onClose}>{t("desktop.done")}</Button>
  );
  return (
    <Dialog title={t("desktop.project.title")} icon={mdiShieldAlertOutline} onClose={onClose} wide footer={footer}>
      <div className="stack" style={{ gap: 12 }}>
        {error && <p className="error-text">{error}</p>}
        {!p && !error && <p className="muted">{t("desktop.checking")}</p>}
        {p && p.files.length === 0 && p.pending.length === 0 && <p className="muted">{t("project.none")}</p>}
        {p && p.files.length > 0 && <p className="mono muted">{p.files.join(", ")}</p>}
        {p && p.pending.length > 0 && (
          <section>
            <p>{t("desktop.project.intro")}</p>
            <Items items={p.pending} />
            <p className="muted">{projectState(p)}</p>
          </section>
        )}
        {p && p.applied.length > 0 && (
          <section>
            <h3 className="t-title">{t("project.applied")}</h3>
            <Items items={p.applied} />
          </section>
        )}
        {p && p.ignored.length > 0 && (
          <section>
            <h3 className="t-title">{t("project.ignored")}</h3>
            <Items items={p.ignored} reasons />
          </section>
        )}
        {p?.problems.map((prob) => (
          <p key={prob} className="error-text">
            {prob}
          </p>
        ))}
      </div>
    </Dialog>
  );
}

function Items({ items, reasons }: { items: ProjectItem[]; reasons?: boolean }) {
  return (
    <ul className="project-items">
      {items.map((it, i) => (
        <li key={i}>
          <span className="mono">{projectItemText(it)}</span>
          {reasons && it.reason && <span className="muted"> ({projectReason(it.reason)})</span>}
        </li>
      ))}
    </ul>
  );
}
