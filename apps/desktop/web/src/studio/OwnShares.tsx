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

import { useCallback, useEffect, useState } from "react";
import { config } from "../api";
import type { ListSubmissionsResponse, OwnItem, Submission } from "../gen/blitz/v1/config_pb";
import { t } from "../i18n";
import { Button, Dialog, useSnackbar } from "@blitz/webui";

/** shareable are the person's own assets of the kinds the organization takes. */
export function shareable(own: OwnItem[], contribute: string[]): OwnItem[] {
  return own.filter((o) => !o.skipped && contribute.includes(o.kind));
}

/** verdictLine is a submission's state and why, as a row says it. */
export function verdictLine(s: Pick<Submission, "state" | "reason" | "publishedVersion">): string {
  const parts = [t(`desktop.ownshares.state.${s.state}`)];
  if (s.publishedVersion) parts.push(s.publishedVersion);
  if (s.reason) parts.push(s.reason);
  return parts.join(" · ");
}

/**
 * Yours, with your organization (spec_053 EN-93, EN-95): the person's own
 * agents, skills, workflows, rules and guardrails sync with the
 * organization (Sync now does it at once), and those of the kinds it takes
 * can be shared: its evaluator judges each one, and the submissions say
 * what it found. Shows nothing the organization's service can't answer.
 */
export function OwnShares() {
  const [data, setData] = useState<ListSubmissionsResponse | null>(null);
  const [sharing, setSharing] = useState<OwnItem | null>(null);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const snack = useSnackbar();
  const load = useCallback(
    () =>
      config.listSubmissions({}).then(
        (r) => setData(r),
        () => setData(null),
      ),
    [],
  );
  useEffect(() => {
    void load();
  }, [load]);
  // While one is being judged, look again now and then.
  const judging = data?.submissions.some((s) => s.state === "judging") ?? false;
  useEffect(() => {
    if (!judging) return;
    const id = setInterval(() => void load(), 4000);
    return () => clearInterval(id);
  }, [judging, load]);
  if (!data) return null;
  const fail = (e: unknown) => snack(t("desktop.orgitems.failed", { error: e instanceof Error ? e.message : String(e) }), { error: true });
  const sync = async () => {
    setBusy(true);
    try {
      const r = await config.syncOwnItems({});
      snack(t("desktop.ownshares.synced", { sent: String(r.sent.length), received: String(r.received.length), removed: String(r.removed.length) }));
      if (r.conflicts.length) snack(t("desktop.ownshares.conflicts", { items: r.conflicts.join(", ") }));
      await load();
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  const share = async () => {
    if (!sharing) return;
    setBusy(true);
    try {
      await config.shareOwnItem({ kind: sharing.kind, name: sharing.name, note });
      snack(t("desktop.ownshares.shared", { name: sharing.name }));
      setSharing(null);
      setNote("");
      await load();
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  const can = shareable(data.own, data.contribute);
  return (
    <section className="card stack rule-section org-items" style={{ gap: 4 }}>
      <div className="row">
        <span className="t-title-sm spacer">{t("desktop.ownshares.title")}</span>
        <Button small variant="text" disabled={busy} onClick={() => void sync()}>
          {t("desktop.ownshares.sync")}
        </Button>
      </div>
      <span className="t-body-sm muted">{t(data.contribute.length ? "desktop.ownshares.hint" : "desktop.ownshares.hint_no_share")}</span>
      {can.length > 0 && (
        <div className="list">
          {can.map((o) => (
            <div key={`${o.kind}/${o.name}`} className="list-item">
              <span className="lines spacer">
                <span className="ellipsis">{`${t(`desktop.orgitems.kind.${o.kind}`)} · ${o.name}`}</span>
              </span>
              <Button small variant="text" disabled={busy} onClick={() => setSharing(o)}>
                {t("desktop.ownshares.share")}
              </Button>
            </div>
          ))}
        </div>
      )}
      {data.submissions.length > 0 && (
        <>
          <span className="t-label">{t("desktop.ownshares.submissions")}</span>
          <div className="list">
            {data.submissions.map((s) => (
              <div key={String(s.id)} className="list-item" title={s.verdicts.map((v) => `${v.passed ? "✓" : "✗"} ${v.check} ${v.score.toFixed(2)}: ${v.reason}`).join("\n")}>
                <span className="lines spacer">
                  <span className="ellipsis">{`${t(`desktop.orgitems.kind.${s.kind}`)} · ${s.name}`}</span>
                  <small className="ellipsis">{verdictLine(s)}</small>
                </span>
              </div>
            ))}
          </div>
        </>
      )}
      {sharing && (
        <Dialog
          title={t("desktop.ownshares.share_title", { name: sharing.name })}
          onClose={() => setSharing(null)}
          footer={
            <>
              <Button onClick={() => setSharing(null)}>{t("desktop.cancel")}</Button>
              <Button variant="filled" disabled={busy} onClick={() => void share()}>
                {t("desktop.ownshares.share")}
              </Button>
            </>
          }
        >
          <span className="t-body-sm">{t("desktop.ownshares.share_about")}</span>
          <textarea className="input" rows={3} value={note} placeholder={t("desktop.ownshares.note")} onChange={(e) => setNote(e.target.value)} />
        </Dialog>
      )}
    </section>
  );
}
