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
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { mdiAlertCircleOutline, mdiCloudUploadOutline } from "@mdi/js";
import { config } from "../api";
import { message } from "../errors";
import type { CatalogReview, PublishableItem } from "../gen/blitz/v1/config_pb";
import { language, t } from "../i18n";
import { Button, Dialog, Icon, Segmented, useSnackbar } from "@blitz/webui";

/** Whether the person is one of the organization's Admins (or its Owner), signed in. */
export function useOrgAdmin(): { admin: boolean; org: string } {
  const [state, setState] = useState({ admin: false, org: "" });
  useEffect(() => {
    config.getEnterprise({ check: false }).then(
      (r) => {
        const st = r.state;
        setState({ admin: !!st?.signedIn && !!st.roles.some((x) => x === "admin" || x === "owner"), org: st?.name || st?.url || "" });
      },
      () => undefined,
    );
  }, []);
  return state;
}

/** One item's choice: chosen, how it applies, who it's for. */
interface Choice {
  on: boolean;
  lock: "default" | "locked";
  groups: string;
}

const key = (it: PublishableItem) => `${it.kind}/${it.name}`;

/**
 * Publish to organization (spec_053 EN-36): the workspace's items, each
 * with how it'll apply (Default or Locked, and its groups), sent as one
 * review an Admin (its author too) approves in the admin console; and the catalog's
 * reviews, with their state.
 */
export function PublishDialog({ dir, org, onClose }: { dir: string; org: string; onClose: () => void }) {
  const snack = useSnackbar();
  const [items, setItems] = useState<PublishableItem[] | null>(null);
  const [choices, setChoices] = useState<Record<string, Choice>>({});
  const [title, setTitle] = useState("");
  const [reviews, setReviews] = useState<CatalogReview[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const loadReviews = () => config.listCatalogReviews({ state: "" }).then((r) => setReviews(r.reviews.slice(0, 10)), () => undefined);
  useEffect(() => {
    config.listPublishable({ workspace: dir }).then(
      (r) => {
        setItems(r.items);
        setChoices(Object.fromEntries(r.items.map((it) => [key(it), { on: false, lock: it.lock === "locked" ? "locked" : "default", groups: it.groups.join(", ") } as Choice])));
      },
      (e) => setError(message(e)),
    );
    void loadReviews();
  }, [dir]);
  const set = (k: string, c: Partial<Choice>) => setChoices((all) => ({ ...all, [k]: { ...all[k], ...c } }));
  const chosen = (items ?? []).filter((it) => choices[key(it)]?.on);
  const publish = async () => {
    setBusy(true);
    setError("");
    try {
      await config.publishToOrganization({
        workspace: dir,
        title: title.trim(),
        items: chosen.map((it) => {
          const c = choices[key(it)];
          return { kind: it.kind, name: it.name, lock: c.lock, groups: c.groups.split(",").map((g) => g.trim()).filter(Boolean) };
        }),
      });
      snack(t("desktop.publish.sent"));
      setChoices((all) => Object.fromEntries(Object.entries(all).map(([k, c]) => [k, { ...c, on: false }])));
      setTitle("");
      void loadReviews();
    } catch (e) {
      setError(message(e));
    }
    setBusy(false);
  };
  return (
    <Dialog
      wide
      title={t("desktop.publish.title", { org })}
      icon={mdiCloudUploadOutline}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" disabled={busy || chosen.length === 0} onClick={() => void publish()}>
            {t("desktop.publish.go", { count: chosen.length || "" })}
          </Button>
        </>
      }
    >
      <div className="stack" style={{ gap: 12 }}>
        <p className="t-body-sm muted">{t("desktop.publish.intro")}</p>
        {items?.length === 0 && <p className="muted">{t("desktop.publish.none")}</p>}
        <div className="list">
          {items?.map((it) => {
            const k = key(it);
            const c = choices[k];
            const blocked = it.problems.length > 0;
            return (
              <div key={k} className="list-item static-item stack" style={{ alignItems: "stretch", gap: 6 }}>
                <label className="row" style={{ gap: 8 }}>
                  <input type="checkbox" checked={!!c?.on} disabled={blocked || busy} onChange={(e) => set(k, { on: e.target.checked })} />
                  <span className="spacer">
                    <code>{k}</code> <span className="muted t-body-sm">{it.path}</span>
                  </span>
                </label>
                {blocked &&
                  it.problems.map((p) => (
                    <span key={p} className="t-body-sm error-text row" style={{ gap: 4 }}>
                      <Icon path={mdiAlertCircleOutline} size="sm" /> {p}
                    </span>
                  ))}
                {c?.on && (
                  <span className="row wrap" style={{ gap: 12 }}>
                    <Segmented
                      small
                      label={t("desktop.publish.lock_hint")}
                      value={c.lock}
                      onChange={(v) => set(k, { lock: v })}
                      options={[
                        { value: "default", label: t("desktop.publish.default") },
                        { value: "locked", label: t("desktop.publish.locked") },
                      ]}
                    />
                    <input className="input spacer" value={c.groups} placeholder={t("desktop.publish.groups")} aria-label={t("desktop.publish.groups")} onChange={(e) => set(k, { groups: e.target.value })} />
                  </span>
                )}
              </div>
            );
          })}
        </div>
        {chosen.length > 0 && (
          <>
            <p className="t-body-sm muted">{t("desktop.publish.lock_hint")}</p>
            <input className="input" value={title} placeholder={t("desktop.publish.review_title")} aria-label={t("desktop.publish.review_title")} onChange={(e) => setTitle(e.target.value)} />
          </>
        )}
        {error && (
          <p className="error-text t-body-sm" role="alert">
            {error}
          </p>
        )}
        {reviews.length > 0 && (
          <>
            <span className="t-label">{t("desktop.publish.reviews")}</span>
            <div className="list">
              {reviews.map((rv) => (
                <div key={rv.id} className="list-item static-item">
                  <span className="lines spacer">
                    <span className="ellipsis">{rv.title}</span>
                    <small className="ellipsis">
                      {[t(`desktop.publish.state.${rv.state}`, { version: rv.version }), rv.byEmail, rv.created ? timestampDate(rv.created).toLocaleString(language()) : "", rv.note].filter(Boolean).join(" · ")}
                    </small>
                  </span>
                </div>
              ))}
            </div>
          </>
        )}
      </div>
    </Dialog>
  );
}
