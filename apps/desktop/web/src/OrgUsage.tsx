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
import { mdiEyeOutline } from "@mdi/js";
import { config } from "./api";
import { message } from "./errors";
import type { GetOrgUsageResponse, OrgLimit, OrgRateLimit, OrgUsed } from "./gen/blitz/v1/config_pb";
import { language, t } from "./i18n";
import { Icon } from "@blitz/webui";

/** A count as people read it: 1.2M, 340K, 512. */
export function count(n: bigint | number): string {
  const v = Number(n);
  if (v >= 1e6) return `${(v / 1e6).toFixed(1)}M`;
  if (v >= 1e3) return `${Math.round(v / 1e3)}K`;
  return String(v);
}

/** What part of a limit is used, 0 to 1. */
export function usedShare(l: OrgLimit): number {
  if (l.unit === "tokens") {
    const amount = Number(l.tokens || l.inputTokens || l.outputTokens);
    const used = Number(l.used?.inputTokens ?? 0n) + Number(l.used?.outputTokens ?? 0n);
    return amount > 0 ? Math.min(1, used / amount) : 0;
  }
  return l.amountUsd > 0 ? Math.min(1, (l.used?.costUsd ?? 0) / l.amountUsd) : 0;
}

/** A limit in words: whose, which period, on what, how much is used and left. */
export function limitText(l: OrgLimit): string {
  const whose = t(`desktop.usage.scope.${l.scopeKind}`);
  const period = t(`desktop.usage.period.${l.period}`);
  const model = l.model || t("desktop.usage.every_model");
  if (l.unit === "tokens") {
    const amount = l.tokens || l.inputTokens || l.outputTokens;
    return t("desktop.usage.quota", { whose, period, model, used: count(Number(l.used?.inputTokens ?? 0n) + Number(l.used?.outputTokens ?? 0n)), amount: count(amount) });
  }
  const used = l.used?.costUsd ?? 0;
  return t("desktop.usage.budget", { whose, period, model, used: used.toFixed(2), amount: l.amountUsd.toFixed(2), left: Math.max(0, l.amountUsd - used).toFixed(2) });
}

/** A model's rate limits in words. */
export function rateText(r: OrgRateLimit): string {
  const parts = (
    [
      ["person_rpm", r.personRpm],
      ["person_tpm", r.personTpm],
      ["rpm", r.rpm],
      ["tpm", r.tpm],
    ] as const
  )
    .filter(([, n]) => n > 0)
    .map(([k, n]) => t(`desktop.usage.rate.${k}`, { n: count(n) }));
  return t("desktop.usage.rate", { model: r.model, limits: parts.join(", ") });
}

function UsedRows({ rows }: { rows: OrgUsed[] }) {
  if (rows.length === 0) return <p className="t-body-sm muted">{t("desktop.usage.none")}</p>;
  return (
    <dl className="facts">
      {rows.map((r) => (
        <div key={r.model} style={{ display: "contents" }}>
          <dt>{r.model}</dt>
          <dd className="t-body-sm">{t("desktop.usage.row", { requests: count(r.requests), tokens: count(r.inputTokens + r.outputTokens), cost: r.costUsd.toFixed(4) })}</dd>
        </div>
      ))}
    </dl>
  );
}

/**
 * The person's own use of the organization's models (spec_053 EN-24):
 * today's and this month's by model, each budget and quota with what's
 * used and left and when it resets, the rate limits, and whether full
 * prompt logs keep their requests. Asked again when shown.
 */
export function OrgUsageCard() {
  const [u, setU] = useState<GetOrgUsageResponse | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    config.getOrgUsage({}).then(setU, (e) => setError(message(e)));
  }, []);
  if (error) return <p className="t-body-sm error-text">{error}</p>;
  if (!u) return null;
  return (
    <section className="card provider-choice stack" style={{ gap: 10 }}>
      <span className="t-title-sm">{t("desktop.usage.title")}</span>
      {u.promptLogs && (
        <p className="t-body-sm row" style={{ gap: 6 }}>
          <Icon path={mdiEyeOutline} size="sm" /> {t("desktop.usage.prompt_logs")}
        </p>
      )}
      <span className="t-label">{t("desktop.usage.today")}</span>
      <UsedRows rows={u.today} />
      <span className="t-label">{t("desktop.usage.month")}</span>
      <UsedRows rows={u.month} />
      {u.limits.length > 0 && <span className="t-label">{t("desktop.usage.limits")}</span>}
      {u.limits.map((l, i) => (
        <div key={i} className="stack" style={{ gap: 4 }}>
          <span className="t-body-sm">
            {limitText(l)}
            {l.resets && <span className="muted"> · {t("desktop.usage.resets", { time: timestampDate(l.resets).toLocaleString(language()) })}</span>}
          </span>
          <div className="progress-bar" role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(usedShare(l) * 100)} aria-label={limitText(l)}>
            <span style={{ width: `${Math.round(usedShare(l) * 100)}%`, background: usedShare(l) >= 0.9 ? "var(--md-error)" : undefined }} />
          </div>
        </div>
      ))}
      {u.rateLimits.length > 0 && <span className="t-label">{t("desktop.usage.rates")}</span>}
      {u.rateLimits.map((r) => (
        <span key={r.model} className="t-body-sm muted">
          {rateText(r)}
        </span>
      ))}
    </section>
  );
}
