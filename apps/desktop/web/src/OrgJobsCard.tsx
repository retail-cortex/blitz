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
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { mdiDomain, mdiPlay } from "@mdi/js";
import { Button, Icon, useSnackbar } from "@blitz/webui";
import { config } from "./api";
import { message } from "./errors";
import type { ServiceJob } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";

/** orgJobDetail is what a line says of one of the organization's jobs. */
export function orgJobDetail(j: Pick<ServiceJob, "description" | "schedule" | "enabled" | "nextRun">, when: (d: Date) => string): string {
  const parts = [j.description, j.schedule || t("desktop.orgjobs.on_request")];
  if (!j.enabled) parts.push(t("desktop.orgjobs.off"));
  else if (j.nextRun) parts.push(t("desktop.orgjobs.next", { when: when(timestampDate(j.nextRun)) }));
  return parts.filter(Boolean).join(" · ");
}

/**
 * Your organization's jobs (spec_053 EN-45): the ones its policy lets the
 * person see, which its admins write and its service runs, each with Run
 * now (refused, saying why, when the policy doesn't let them). Nothing
 * without an organization or such a job.
 */
export function OrgJobsCard() {
  const snack = useSnackbar();
  const [jobs, setJobs] = useState<ServiceJob[]>([]);
  const [busy, setBusy] = useState("");
  const load = useCallback(() => {
    config.listServiceJobs({}).then(
      (r) => setJobs(r.jobs.filter((j) => j.organization)),
      () => setJobs([]),
    );
  }, []);
  useEffect(load, [load]);
  if (jobs.length === 0) return null;
  const run = async (j: ServiceJob) => {
    setBusy(j.id);
    try {
      await config.runServiceJob({ jobId: j.id });
      snack(t("desktop.orgjobs.started", { name: j.name }));
      load();
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy("");
    }
  };
  return (
    <section className="stack" style={{ gap: 8 }}>
      <span className="t-title-sm">{t("desktop.orgjobs.title")}</span>
      <span className="t-body-sm muted">{t("desktop.orgjobs.about")}</span>
      <div className="list">
        {jobs.map((j) => (
          <div key={j.id} className="list-item">
            <Icon path={mdiDomain} />
            <span className="lines spacer">
              <span className="ellipsis">{j.name}</span>
              <small className="ellipsis">{orgJobDetail(j, (d) => d.toLocaleString())}</small>
            </span>
            <span className="trailing">
              <Button small icon={mdiPlay} disabled={busy !== ""} onClick={() => void run(j)}>
                {t("desktop.orgjobs.run")}
              </Button>
            </span>
          </div>
        ))}
      </div>
    </section>
  );
}
