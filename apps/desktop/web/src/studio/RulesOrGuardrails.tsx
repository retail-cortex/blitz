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

import { mdiScaleBalance, mdiShieldLockOutline } from "@mdi/js";
import { t } from "../i18n";
import { Button, Icon } from "../ui/controls";

/** The questions the comparison answers, in order. */
const aspects = ["for", "when", "sees", "fails", "example"] as const;

/**
 * What tells rules from guardrails, at the top of both Studio pages
 * (spec_gates_041 GT-11): rules check the work against the team's
 * expectations at the turn's edges and can send it back; guardrails keep
 * every step safe and within limits, built in or your own, and never ask
 * for a revision. The page shown (current) is marked, and the other is a
 * button away (onOther).
 */
export function RulesOrGuardrails({ current, onOther }: { current: "rules" | "guardrails"; onOther?: () => void }) {
  const other = current === "rules" ? "guardrails" : "rules";
  return (
    <section className="card outlined rules-or-guardrails" aria-label={t("desktop.gates.compare")}>
      <div className="rog-grid" role="table" aria-label={t("desktop.gates.compare")}>
        <div role="row" className="rog-row rog-head">
          <span role="columnheader" />
          {(["rules", "guardrails"] as const).map((k) => (
            <span key={k} role="columnheader" className={`rog-kind ${k === current ? "on" : ""}`}>
              <Icon path={k === "rules" ? mdiScaleBalance : mdiShieldLockOutline} size="sm" />
              {t(`desktop.gates.${k}`)}
              {k === current && <small className="muted"> · {t("desktop.gates.here")}</small>}
            </span>
          ))}
        </div>
        {aspects.map((a) => (
          <div key={a} role="row" className="rog-row">
            <span role="rowheader" className="t-label muted">
              {t(`desktop.gates.aspect.${a}`)}
            </span>
            {(["rules", "guardrails"] as const).map((k) => (
              <span key={k} role="cell" className={`t-body-sm ${k === current ? "on" : "muted"}`}>
                {t(`desktop.gates.${k}.${a}`)}
              </span>
            ))}
          </div>
        ))}
      </div>
      {onOther && (
        <div className="row" style={{ gap: 8 }}>
          <span className="t-body-sm muted spacer">{t(`desktop.gates.use_${other}`)}</span>
          <Button small icon={other === "rules" ? mdiScaleBalance : mdiShieldLockOutline} onClick={onOther}>
            {t(`desktop.gates.open_${other}`)}
          </Button>
        </div>
      )}
    </section>
  );
}
