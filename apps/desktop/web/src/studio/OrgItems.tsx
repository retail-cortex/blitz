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
import { mdiLockOutline } from "@mdi/js";
import { config } from "../api";
import type { OrgItem } from "../gen/blitz/v1/config_pb";
import { t } from "../i18n";
import { Icon, Switch, useSnackbar } from "@blitz/webui";

/** OrgKind is a kind of asset the organization's library offers. */
export type OrgKind = "agent" | "skill" | "workflow" | "rule" | "guardrail";

/** subtitle is what a row says under an item's name: its version, and why it's there or whose it is to switch. */
export function subtitle(it: Pick<OrgItem, "version" | "state" | "on" | "decidedBy" | "groups">): string {
  const parts: string[] = [];
  if (it.version) parts.push(t("desktop.orgitems.version", { version: it.version }));
  if (it.state === "available") parts.push(t(it.on ? "desktop.orgitems.available_on" : "desktop.orgitems.available_off"));
  else if (it.decidedBy) parts.push(t("desktop.orgitems.decided", { by: it.decidedBy }));
  else if (it.groups.length) parts.push(t("desktop.orgitems.for", { groups: it.groups.join(", ") }));
  else parts.push(t("desktop.orgitems.locked"));
  return parts.join(" · ");
}

/**
 * From your organization (spec_053 EN-34, EN-39, EN-80, EN-83): what the
 * person's organization offers them from its library, of one kind or every
 * kind: each required one locked, saying who decided it; each available one
 * with a switch, which follows them to every computer. Its admins change
 * the library in the organization's console. Shows nothing without an
 * organization.
 */
export function OrgItems({ kind, title }: { kind?: OrgKind; title?: string }) {
  const [items, setItems] = useState<OrgItem[]>([]);
  const [busy, setBusy] = useState("");
  const snack = useSnackbar();
  const load = useCallback(
    () =>
      config.listOrgItems({ kind: kind ?? "" }).then(
        (r) => setItems(r.items),
        () => setItems([]),
      ),
    [kind],
  );
  useEffect(() => {
    void load();
  }, [load]);
  if (items.length === 0) return null;
  const set = async (it: OrgItem, on: boolean) => {
    setBusy(`${it.kind}/${it.name}`);
    try {
      await config.setOrgItem({ kind: it.kind, name: it.name, on });
      snack(t(on ? "desktop.orgitems.switched_on" : "desktop.orgitems.switched_off", { name: it.name }));
      await load();
    } catch (e) {
      snack(t("desktop.orgitems.failed", { error: e instanceof Error ? e.message : String(e) }), { error: true });
    } finally {
      setBusy("");
    }
  };
  return (
    <section className="stack rule-section org-items" style={{ gap: 4 }}>
      <span className="t-title-sm">{title ?? t("desktop.orgitems.title")}</span>
      <span className="t-body-sm muted">{t("desktop.orgitems.hint")}</span>
      <div className="list">
        {items.map((it) => (
          <div key={`${it.kind}/${it.name}`} className="list-item" title={it.description || it.path}>
            {it.state === "available" ? (
              <Switch label={t("desktop.orgitems.switch", { name: it.name })} checked={it.on} disabled={busy !== ""} onChange={(on) => void set(it, on)} />
            ) : (
              <Icon path={mdiLockOutline} />
            )}
            <span className="lines spacer">
              <span className="ellipsis">{kind ? it.name : `${t(`desktop.orgitems.kind.${it.kind}`)} · ${it.name}`}</span>
              <small className="ellipsis">{subtitle(it)}</small>
            </span>
          </div>
        ))}
      </div>
    </section>
  );
}
