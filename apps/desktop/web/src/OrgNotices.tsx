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
import { config } from "./api";
import { message } from "./errors";
import type { OrgNotices } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";
import { Button, Field, Switch, useSnackbar } from "@blitz/webui";

/**
 * Job notices (the enterprise spec's JB-31): how the person hears of their
 * jobs' runs on the organization's service, by email, a webhook, or both,
 * for failures and runs waiting for an answer, or every run.
 */
export function OrgNoticesCard() {
  const snack = useSnackbar();
  const [saved, setSaved] = useState<OrgNotices | null>(null);
  const [webhook, setWebhook] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    config.getOrgNotices({}).then(
      (r) => {
        setSaved(r.notices ?? null);
        setWebhook(r.notices?.webhook ?? "");
      },
      () => setSaved(null),
    );
  }, []);
  if (!saved) return null;
  const save = async (next: { email?: boolean; webhook?: string; everyRun?: boolean }) => {
    setBusy(true);
    try {
      const r = await config.setOrgNotices({ notices: { email: next.email ?? saved.email, webhook: next.webhook ?? saved.webhook, everyRun: next.everyRun ?? saved.everyRun } });
      setSaved(r.notices ?? saved);
      setWebhook(r.notices?.webhook ?? "");
      snack(t("desktop.notices.saved"));
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="card stack org-notices" style={{ gap: 10 }}>
      <span className="t-title-sm">{t("desktop.notices.title")}</span>
      <span className="t-body-sm muted">{t("desktop.notices.intro")}</span>
      <div className="setting">
        <div className="stack" style={{ gap: 2 }}>
          <span className="t-title-sm">{t("desktop.notices.email")}</span>
          <span className="t-body-sm muted">{saved.emailAvailable ? t("desktop.notices.email_detail") : t("desktop.notices.email_none")}</span>
        </div>
        <div className="setting-control">
          <Switch label={t("desktop.notices.email")} checked={saved.email} disabled={busy} onChange={(email) => void save({ email })} />
        </div>
      </div>
      <div className="setting">
        <div className="stack" style={{ gap: 2 }}>
          <span className="t-title-sm">{t("desktop.notices.every_run")}</span>
          <span className="t-body-sm muted">{t("desktop.notices.every_run_detail")}</span>
        </div>
        <div className="setting-control">
          <Switch label={t("desktop.notices.every_run")} checked={saved.everyRun} disabled={busy} onChange={(everyRun) => void save({ everyRun })} />
        </div>
      </div>
      <Field label={t("desktop.notices.webhook")} supporting={t("desktop.notices.webhook_help")}>
        {(id) => (
          <div className="row">
            <input id={id} className="input mono" value={webhook} placeholder={t("desktop.notices.webhook_placeholder")} spellCheck={false} onChange={(e) => setWebhook(e.target.value)} />
            <Button variant="filled" disabled={busy || webhook.trim() === saved.webhook} onClick={() => void save({ webhook: webhook.trim() })}>
              {t("desktop.notices.save")}
            </Button>
          </div>
        )}
      </Field>
    </section>
  );
}
