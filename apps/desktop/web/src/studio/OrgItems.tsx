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
import { mdiLockOutline } from "@mdi/js";
import { config } from "../api";
import type { OrgItem } from "../gen/blitz/v1/config_pb";
import { t } from "../i18n";
import { Icon } from "@blitz/webui";

/**
 * From your organization (spec_053 EN-34, EN-39): the rules or
 * guardrails the person's organization applies to them, under the
 * workspace's own in Studio, each locked and saying who decided it. Its
 * admins change them in the organization's console. Shows nothing
 * without an organization.
 */
export function OrgItems({ kind }: { kind: "rule" | "guardrail" }) {
  const [items, setItems] = useState<OrgItem[]>([]);
  useEffect(() => {
    config.listOrgItems({ kind }).then(
      (r) => setItems(r.items),
      () => setItems([]),
    );
  }, [kind]);
  if (items.length === 0) return null;
  return (
    <section className="stack rule-section org-items" style={{ gap: 4 }}>
      <span className="t-title-sm">{t("desktop.orgitems.title")}</span>
      <span className="t-body-sm muted">{t("desktop.orgitems.hint")}</span>
      <div className="list">
        {items.map((it) => (
          <div key={it.path} className="list-item" title={it.path}>
            <Icon path={mdiLockOutline} />
            <span className="lines spacer">
              <span className="ellipsis">{it.name}</span>
              <small className="ellipsis">{it.decidedBy ? t("desktop.orgitems.decided", { by: it.decidedBy }) : it.groups.length ? t("desktop.orgitems.for", { groups: it.groups.join(", ") }) : t("desktop.orgitems.locked")}</small>
            </span>
          </div>
        ))}
      </div>
    </section>
  );
}
