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

/** One cap a limit sets: it in words, and what part is used, 0 to 1. */
export interface LimitCap {
  text: string;
  share: number;
}

const share = (used: number, amount: number) => (amount > 0 ? Math.min(1, used / amount) : 0);

/**
 * A limit's caps as the gateway counts them: a budget's dollars; a token
 * quota's total (input and output together), input alone and output
 * alone, each it sets, with what's used and left.
 */
export function limitCaps(l: OrgLimit): LimitCap[] {
  const whose = t(`desktop.usage.scope.${l.scopeKind}`);
  const period = t(`desktop.usage.period.${l.period}`);
  const model = l.model || t("desktop.usage.every_model");
  if (l.unit !== "tokens") {
    const used = l.used?.costUsd ?? 0;
    return [{ text: t("desktop.usage.budget", { whose, period, model, used: used.toFixed(2), amount: l.amountUsd.toFixed(2), left: Math.max(0, l.amountUsd - used).toFixed(2) }), share: share(used, l.amountUsd) }];
  }
  const input = Number(l.used?.inputTokens ?? 0n);
  const output = Number(l.used?.outputTokens ?? 0n);
  return (
    [
      ["desktop.usage.quota", l.tokens, input + output],
      ["desktop.usage.quota_input", l.inputTokens, input],
      ["desktop.usage.quota_output", l.outputTokens, output],
    ] as const
  )
    .filter(([, amount]) => amount > 0n)
    .map(([key, amount, used]) => ({ text: t(key, { whose, period, model, used: count(used), amount: count(amount), left: count(Math.max(0, Number(amount) - used)) }), share: share(used, Number(amount)) }));
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
      {u.limits.flatMap((l, i) =>
        limitCaps(l).map((c, j) => (
          <div key={`${i}.${j}`} className="stack" style={{ gap: 4 }}>
            <span className="t-body-sm">
              {c.text}
              {l.resets && <span className="muted"> · {t("desktop.usage.resets", { time: timestampDate(l.resets).toLocaleString(language()) })}</span>}
            </span>
            <div className="progress-bar" role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(c.share * 100)} aria-label={c.text}>
              <span style={{ width: `${Math.round(c.share * 100)}%`, background: c.share >= 0.9 ? "var(--md-error)" : undefined }} />
            </div>
          </div>
        )),
      )}
      {u.rateLimits.length > 0 && <span className="t-label">{t("desktop.usage.rates")}</span>}
      {u.rateLimits.map((r) => (
        <span key={r.model} className="t-body-sm muted">
          {rateText(r)}
        </span>
      ))}
    </section>
  );
}
