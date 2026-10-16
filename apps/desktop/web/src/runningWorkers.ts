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

// The Workers menu's "Running now" section: every worker run going now, in
// any workspace, each with Stop (spec_desktop_024 DSK-91a).

import { mdiProgressClock, mdiStopCircleOutline } from "@mdi/js";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { WorkerRun } from "./gen/blitz/v1/worker_pb";
import { language, t } from "./i18n";
import type { MenuEntry } from "./ui/controls";

/** What a running row does: open the worker (in this workspace), or switch to its workspace; and stop it. */
export interface RunningActions {
  dir: string;
  /** The workspace's name as the app shows it. */
  workspaceName: (dir: string) => string;
  onShow: (run: WorkerRun) => void;
  onStop: (run: WorkerRun) => void;
}

/** The section's entries: a heading and a row per run, or nothing when none runs. */
export function runningEntries(runs: WorkerRun[], a: RunningActions): MenuEntry[] {
  if (runs.length === 0) return [];
  return [
    { heading: t("desktop.menu.workers.running") },
    ...runs.map(
      (r): MenuEntry => ({
        label: r.worker,
        detail: [
          r.workspace === a.dir ? "" : a.workspaceName(r.workspace),
          r.started ? t("desktop.menu.workers.since", { time: timestampDate(r.started).toLocaleTimeString(language(), { hour: "numeric", minute: "2-digit" }) }) : "",
        ]
          .filter(Boolean)
          .join(" · "),
        icon: mdiProgressClock,
        onSelect: () => a.onShow(r),
        actions: [{ icon: mdiStopCircleOutline, label: t("desktop.menu.workers.stop", { name: r.worker }), onSelect: () => a.onStop(r) }],
      }),
    ),
  ];
}
