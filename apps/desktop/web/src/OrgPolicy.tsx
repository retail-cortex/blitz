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

import type { PolicySetting } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";

/** Lists whose emptiness means every one (every kind, every model). */
const everyWhenEmpty = (path: string) => path === "connectors.kinds" || /^models\.own\.[^.]+\.models$/.test(path);

/** policyLabel is a setting of the organization's policy, for people (EN-39). */
export function policyLabel(path: string): string {
  const own = /^models\.own\.([^.]+)\.(allowed|models)$/.exec(path);
  if (own) return t(`desktop.org.policy.own_${own[2]}`, { provider: t(`desktop.org.policy.provider.${own[1]}`) });
  const item = /^(rules|guardrails)\.(.+)$/.exec(path);
  if (item) return t(`desktop.org.policy.${item[1] === "rules" ? "rule" : "guardrail"}`, { name: item[2] });
  return t(`desktop.org.policy.field.${path}`);
}

/** policyValue is a setting's value as people read it. */
export function policyValue(path: string, valueJson: string): string {
  let v: unknown;
  try {
    v = JSON.parse(valueJson);
  } catch {
    return valueJson;
  }
  if (typeof v === "boolean") return t(v ? "desktop.org.policy.on" : "desktop.org.policy.off");
  if (Array.isArray(v)) return v.length ? v.join(", ") : t(everyWhenEmpty(path) ? "desktop.org.policy.every" : "desktop.org.policy.none");
  if (v === null || v === undefined) return "—";
  return String(v);
}

/** policyDecided is who decided a setting. */
export function policyDecided(s: PolicySetting): string {
  const by = s.by.join(", ");
  const level = s.level === "group" ? t("desktop.org.policy.by_group", { groups: by }) : t(`desktop.org.policy.by_${s.level || "global"}`);
  return s.locked ? `${level} · ${t("desktop.org.policy.locked")}` : level;
}

/**
 * Your policy (spec_053 EN-39): the organization's policy as it applies to
 * the person, every setting with its value and which level decided it
 * (the organization's, a group's, one an admin set for them), read-only:
 * admins change it in the console.
 */
export function OrgPolicyCard({ settings }: { settings: PolicySetting[] }) {
  if (settings.length === 0) return null;
  const custom = settings.filter((s) => s.level !== "default").length;
  return (
    <details className="card stack org-policy">
      <summary className="t-title-sm">{t("desktop.org.policy.title", { n: custom })}</summary>
      <p className="t-body-sm muted">{t("desktop.org.policy.about")}</p>
      <table className="org-policy-table">
        <thead>
          <tr>
            <th>{t("desktop.org.policy.setting")}</th>
            <th>{t("desktop.org.policy.value")}</th>
            <th>{t("desktop.org.policy.decided")}</th>
          </tr>
        </thead>
        <tbody>
          {settings.map((s) => (
            <tr key={s.path}>
              <td>{policyLabel(s.path)}</td>
              <td>{policyValue(s.path, s.valueJson)}</td>
              <td className="muted">{policyDecided(s)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  );
}
