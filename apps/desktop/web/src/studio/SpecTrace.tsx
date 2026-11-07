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

import { mdiAlertCircleOutline, mdiCheckCircleOutline, mdiCircleOutline, mdiCircleSlice4, mdiCloseCircleOutline, mdiLinkVariant, mdiLinkVariantOff, mdiSourceCommit } from "@mdi/js";
import { Icon } from "@blitz/webui";
import type { SpecEvidence, SpecRequirement } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";

// A requirement's states (spec_057 SDD-12): the check's, then the model's.
const stateIcons: Record<string, string> = {
  traced: mdiLinkVariant,
  untraced: mdiLinkVariantOff,
  implemented: mdiCheckCircleOutline,
  partial: mdiCircleSlice4,
  missing: mdiCircleOutline,
  contradicted: mdiCloseCircleOutline,
};

/** A requirement's state, as a short label with its icon ("Not verified" for none). */
export function StateLabel({ r }: { r: SpecRequirement }) {
  const bad = r.state === "missing" || r.state === "contradicted" || r.state === "untraced";
  return (
    <span className={`row spec-state ${bad ? "error-text" : r.state === "implemented" ? "ok-text" : "muted"}`} style={{ gap: 4 }} title={r.judged ? t("desktop.specs.by_model") : r.state ? t("desktop.specs.by_check") : ""}>
      <Icon path={stateIcons[r.state] ?? mdiCircleOutline} size="sm" />
      {r.state ? t(`desktop.specs.state.${r.state}`) : t("desktop.specs.state.none")}
      {r.stale && <span className="t-body-sm muted">{t("desktop.specs.stale")}</span>}
    </span>
  );
}

/** Where a requirement was found: a file's line, or a commit. */
function where(e: SpecEvidence): string {
  return e.kind === "commit" ? e.path : e.line > 0 ? `${e.path}:${e.line}` : e.path;
}

/** One piece of evidence, as a line. */
function EvidenceLine({ e }: { e: SpecEvidence }) {
  return (
    <span className="row t-body-sm ellipsis" style={{ gap: 4 }} title={e.text}>
      {e.kind === "commit" && <Icon path={mdiSourceCommit} size="sm" />}
      <code className="ellipsis">{where(e)}</code>
      <span className="muted">· {t(`desktop.specs.kind.${e.kind}`)}</span>
    </span>
  );
}

/**
 * A spec's requirements as traced (SDD-10, SDD-12): each one's ID and text,
 * what changed since the spec was approved, its last verification and the
 * evidence. With spec, the matrix's column naming the spec, each row opening
 * it (onOpen).
 */
export function RequirementTable({ requirements, matrix, onOpen }: { requirements: SpecRequirement[]; matrix?: boolean; onOpen?: (spec: string) => void }) {
  if (requirements.length === 0) return <p className="t-body-sm muted">{t("desktop.specs.no_requirements")}</p>;
  return (
    <div className="table-wrap">
      <table className={`run-table spec-table ${matrix ? "is-matrix" : ""}`}>
        <thead>
          <tr>
            {matrix && <th>{t("desktop.specs.col_spec")}</th>}
            <th>{t("desktop.specs.col_requirement")}</th>
            <th>{t("desktop.specs.col_state")}</th>
            <th>{t("desktop.specs.col_evidence")}</th>
          </tr>
        </thead>
        <tbody>
          {requirements.map((r) => (
            <tr key={`${r.spec}/${r.id}`} className={onOpen ? "clickable" : ""} onClick={onOpen ? () => onOpen(r.spec) : undefined}>
              {matrix && (
                <td>
                  <code className="ellipsis">{r.spec}</code>
                </td>
              )}
              <td>
                <span className="stack" style={{ gap: 2 }}>
                  <span className="row" style={{ gap: 6 }}>
                    <strong>{r.id}</strong>
                    {r.change && (
                      <span className={`t-body-sm ${r.change === "removed" ? "error-text" : "warn-text"}`}>
                        <Icon path={mdiAlertCircleOutline} size="sm" /> {t(`desktop.specs.change.${r.change}`)}
                      </span>
                    )}
                  </span>
                  <span className={`t-body-sm ${r.change === "removed" ? "muted strike" : ""}`}>{r.text}</span>
                </span>
              </td>
              <td>
                <span className="stack" style={{ gap: 2 }}>
                  <StateLabel r={r} />
                  {r.reason && <span className="t-body-sm muted wrap-text">{r.reason}</span>}
                </span>
              </td>
              <td>
                <span className="stack" style={{ gap: 2 }}>
                  {r.evidence.slice(0, 3).map((e, i) => (
                    <EvidenceLine key={i} e={e} />
                  ))}
                  {r.evidence.length > 3 && <span className="t-body-sm muted">{t("desktop.specs.more_evidence", { count: r.evidence.length - 3 })}</span>}
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
