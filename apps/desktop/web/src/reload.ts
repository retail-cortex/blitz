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

// Reloading the agent files (spec_desktop_024 DSK-91d): what a reload
// loaded, in a sentence, for the snackbar and /reload.

import { t, tn } from "./i18n";
import type { ReloadAgentFilesResponse } from "./gen/blitz/v1/workspace_pb";

/** What a reload loaded: "Reloaded 2 agents, 1 skill and 4 instruction files and rules." */
export function reloadSummary(res: Pick<ReloadAgentFilesResponse, "agents" | "skills" | "files">): string {
  return t("reload.done", {
    agents: tn("reload.agents", res.agents),
    skills: tn("reload.skills", res.skills),
    files: tn("reload.files", res.files.length),
  });
}
