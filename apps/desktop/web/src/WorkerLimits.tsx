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
import { mdiCheck } from "@mdi/js";
import { config } from "./api";
import { message } from "./errors";
import { t } from "./i18n";
import type { WorkerPolicySettings } from "./gen/blitz/v1/worker_pb";
import { limitFields, permissionKinds, toggleKind, type LimitField } from "./workersOverview";
import { Chip, useSnackbar } from "./ui/controls";

/**
 * The global limits on every worker (Settings › Workers; spec_workers_023
 * WK-63), [workers.policy] in the global settings: how many runs go at
 * once, what a worker that sets no limit gets and the most it may ask for
 * (model calls, cost, time), and the permission kinds it may be given.
 * Each field is saved as it's left (empty: the default); the open
 * workspaces take it from their next listing or run.
 */
export function WorkerLimits({ policy, onChanged }: { policy: WorkerPolicySettings; onChanged: () => void }) {
  const snack = useSnackbar();
  const [values, setValues] = useState<Record<LimitField, string>>(() => limitFields(policy));
  useEffect(() => setValues(limitFields(policy)), [policy]);

  const save = async (key: string, value: string) => {
    try {
      await config.setConfigValue({ workspace: "", key: `workers.policy.${key}`, value });
      snack(t("desktop.workers.limits.saved"));
      onChanged();
    } catch (e) {
      snack(message(e), { error: true });
      setValues(limitFields(policy));
    }
  };
  const field = (key: LimitField, type: "number" | "text", label: string) => (
    <input
      className="input"
      // A cap left empty is none (WK-73): jobs may ask for more than the defaults.
      placeholder={key.startsWith("max_") && key !== "max_concurrent" ? t("desktop.workers.limits.no_limit") : undefined}
      type={type}
      min={type === "number" ? 0 : undefined}
      step={key.endsWith("cost_usd") ? 0.01 : type === "number" ? 1 : undefined}
      aria-label={label}
      value={values[key]}
      onChange={(e) => setValues((v) => ({ ...v, [key]: e.target.value }))}
      onBlur={() => values[key].trim() !== limitFields(policy)[key] && void save(key, values[key].trim())}
    />
  );
  const restart = policy.maxConcurrent > 0 && policy.maxConcurrent !== policy.runningMaxConcurrent;
  const row = (name: string, def: LimitField, max: LimitField, type: "number" | "text") => (
    <>
      <span className="t-label">{t(`desktop.workers.limits.${name}`)}</span>
      {field(def, type, `${t(`desktop.workers.limits.${name}`)} · ${t("desktop.workers.limits.default")}`)}
      {field(max, type, `${t(`desktop.workers.limits.${name}`)} · ${t("desktop.workers.limits.max")}`)}
    </>
  );
  return (
    <section className="card outlined stack worker-limits" style={{ gap: 12 }}>
      <div className="stack" style={{ gap: 2 }}>
        <span className="t-title-sm">{t("desktop.workers.limits")}</span>
        <span className="t-body-sm muted">{t("desktop.workers.limits.hint", { path: policy.path })}</span>
      </div>
      <div className="worker-limits-grid">
        <span />
        <span className="t-body-sm muted">{t("desktop.workers.limits.default")}</span>
        <span className="t-body-sm muted">{t("desktop.workers.limits.max")}</span>
        {row("calls", "default_max_turns", "max_turns", "number")}
        {row("cost", "default_max_cost_usd", "max_cost_usd", "number")}
        {row("time", "default_timeout", "max_timeout", "text")}
        <span className="t-label">{t("desktop.workers.limits.concurrent")}</span>
        {field("max_concurrent", "number", t("desktop.workers.limits.concurrent"))}
        <span className={`t-body-sm ${restart ? "" : "muted"}`}>{restart ? t("desktop.workers.limits.restart", { n: policy.runningMaxConcurrent }) : t("desktop.workers.limits.concurrent_hint")}</span>
      </div>
      <div className="stack" style={{ gap: 6 }}>
        <span className="t-label">{t("desktop.workers.limits.allow")}</span>
        <div className="row wrap" style={{ gap: 6 }}>
          {permissionKinds.map((k) => {
            const on = policy.allow.includes(k);
            return (
              <Chip key={k} selected={on} icon={on ? mdiCheck : undefined} aria-pressed={on} onClick={() => void save("allow", toggleKind(policy.allow, k))}>
                {t(`desktop.workers.limits.kind.${k}`)}
              </Chip>
            );
          })}
        </div>
        <span className="t-body-sm muted">{policy.allow.length ? t("desktop.workers.limits.allow_hint") : t("desktop.workers.limits.read_only")}</span>
      </div>
    </section>
  );
}
