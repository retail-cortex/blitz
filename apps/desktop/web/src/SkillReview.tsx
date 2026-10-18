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

import { useState } from "react";
import { createPortal } from "react-dom";
import { mdiCodeBraces, mdiPackageVariantClosed, mdiShieldCheckOutline } from "@mdi/js";
import { config } from "./api";
import { message } from "./errors";
import { t } from "./i18n";
import { Button, Dialog, Icon, useSnackbar } from "./ui/controls";

/** A skill's scripts to review, as ConfigService.GetSkillReview has them (or Studio's SkillInfo). */
export interface ScriptsToReview {
  dir: string;
  hash: string;
  scripts: { name: string; language: string; source: string; deps: string[] }[];
}

/**
 * Reviewing a skill's scripts before they run unasked (spec_skills_013
 * SK-108): after a registry install, or when Studio shows a skill whose
 * scripts are new or changed. Each script with its language, file and
 * the packages it installs, and where the skill is; **Trust these
 * scripts** records the content shown (TrustSkillScripts: an edit since
 * is refused, and needs review again), **Not now** leaves each run asking
 * first. onDone hears whether they were trusted.
 */
export function SkillReviewDialog({ name, review, onDone }: { name: string; review: ScriptsToReview; onDone: (trusted: boolean) => void }) {
  const snack = useSnackbar();
  const [busy, setBusy] = useState(false);
  const trust = async () => {
    setBusy(true);
    try {
      await config.trustSkillScripts({ dir: review.dir, hash: review.hash });
      snack(t("desktop.review.trusted", { name }));
      onDone(true);
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy(false);
    }
  };
  return createPortal(
    <Dialog
      title={t("desktop.review.title", { name })}
      icon={mdiShieldCheckOutline}
      onClose={() => onDone(false)}
      footer={
        <>
          <Button onClick={() => onDone(false)}>{t("desktop.review.later")}</Button>
          <Button variant="filled" icon={mdiShieldCheckOutline} disabled={busy} onClick={() => void trust()}>
            {t("desktop.review.trust")}
          </Button>
        </>
      }
    >
      <div className="stack skill-review" style={{ gap: 10 }}>
        <p>{t("desktop.review.body")}</p>
        <code className="t-body-sm muted ellipsis" title={review.dir}>
          {review.dir}
        </code>
        <div className="list">
          {review.scripts.map((s) => (
            <div key={s.name} className="list-item static">
              <Icon path={mdiCodeBraces} />
              <span className="lines spacer">
                <span className="ellipsis">
                  {s.name} <small className="muted">· {s.language || "?"}</small>
                </span>
                <small className="ellipsis">{s.source}</small>
                {s.deps.length > 0 && (
                  <small className="ellipsis">
                    <Icon path={mdiPackageVariantClosed} size="sm" /> {s.deps.join(", ")}
                  </small>
                )}
              </span>
            </div>
          ))}
        </div>
        <p className="t-body-sm muted">{t("desktop.review.hint")}</p>
      </div>
    </Dialog>,
    document.body,
  );
}
