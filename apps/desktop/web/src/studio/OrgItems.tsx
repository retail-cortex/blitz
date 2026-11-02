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
import { mdiContentCopy, mdiDomain, mdiLockOutline } from "@mdi/js";
import { config } from "../api";
import { message } from "../errors";
import type { OrgItem } from "../gen/blitz/v1/config_pb";
import { t } from "../i18n";
import { Button, Icon, useSnackbar } from "@blitz/webui";

/**
 * From your organization (spec_053 EN-34): the catalog's items of one kind
 * this workspace gets, under the workspace's own in a Studio list. A
 * locked one says its Admins change it, through a review; a default one
 * can be copied into the workspace to change there, and once it is, says
 * the workspace's own takes its place. Shows nothing without a catalog.
 */
export function OrgItems({ workspace, kind, onCopied }: { workspace: string; kind: string; onCopied?: () => void }) {
  const snack = useSnackbar();
  const [items, setItems] = useState<OrgItem[]>([]);
  const [copying, setCopying] = useState("");
  const load = useCallback(() => {
    config.listOrgItems({ workspace, kind }).then(
      (r) => setItems(r.items),
      () => setItems([]),
    );
  }, [workspace, kind]);
  useEffect(load, [load]);
  if (items.length === 0) {
    return null;
  }
  const copy = async (it: OrgItem) => {
    setCopying(it.name);
    try {
      await config.copyOrgItem({ workspace, kind: it.kind, name: it.name });
      snack(t("desktop.orgitems.copied", { name: it.name }));
      load();
      onCopied?.();
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setCopying("");
    }
  };
  return (
    <section className="stack rule-section org-items" style={{ gap: 4 }}>
      <span className="t-title-sm">{t("desktop.orgitems.title")}</span>
      <span className="t-body-sm muted">{t("desktop.orgitems.hint")}</span>
      <div className="list">
        {items.map((it) => {
          const locked = it.lock === "locked";
          const note = locked ? t("desktop.orgitems.locked") : it.overridden ? t("desktop.orgitems.overridden") : t("desktop.orgitems.default");
          return (
            <div key={it.path} className="list-item" title={it.path}>
              <Icon path={locked ? mdiLockOutline : mdiDomain} />
              <span className="lines spacer">
                <span className="ellipsis">{it.name}</span>
                <small className="ellipsis">{[note, it.groups.length ? t("desktop.orgitems.for", { groups: it.groups.join(", ") }) : ""].filter(Boolean).join(" · ")}</small>
              </span>
              {!locked && !it.overridden && (
                <span className="trailing">
                  <Button small icon={mdiContentCopy} disabled={copying !== ""} onClick={() => void copy(it)} title={t("desktop.orgitems.copy_hint")}>
                    {t("desktop.orgitems.copy")}
                  </Button>
                </span>
              )}
            </div>
          );
        })}
      </div>
    </section>
  );
}
