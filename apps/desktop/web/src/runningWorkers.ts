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

// The worker runs going now (Studio › Workers, a workspace's; Settings ›
// Workers, every workspace's), each with Stop (spec_desktop_024 DSK-91a).

import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { WorkerRun } from "./gen/blitz/v1/worker_pb";
import { language, t } from "./i18n";

/**
 * A running row's detail: the run's workspace, by the name workspaceName
 * gives it, when it isn't dir (none for ""), and since when it runs.
 */
export function runDetail(r: WorkerRun, dir: string, workspaceName: (dir: string) => string): string {
  return [
    dir && r.workspace === dir ? "" : workspaceName(r.workspace),
    r.started ? t("desktop.menu.workers.since", { time: timestampDate(r.started).toLocaleTimeString(language(), { hour: "numeric", minute: "2-digit" }) }) : "",
  ]
    .filter(Boolean)
    .join(" · ");
}
