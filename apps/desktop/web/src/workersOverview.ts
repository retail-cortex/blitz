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

// Every workspace's workers (Settings › Workers, spec_workers_023
// WK-61): what's said of each workspace, and the home workspace the
// global workers run in.

import { workers } from "./api";
import type { WorkspaceWorkers } from "./gen/blitz/v1/worker_pb";

/** What's said of a workspace with workers: its folder is gone, it isn't one of the app's, or neither. */
export type WorkspaceFlag = "missing" | "unlisted" | "listed";

/** Says whether a workspace's folder is gone, or it isn't among the app's workspaces (listed). */
export function workspaceFlag(g: Pick<WorkspaceWorkers, "workspace" | "missing">, listed: readonly { dir: string }[]): WorkspaceFlag {
  if (g.missing) return "missing";
  return listed.some((w) => w.dir === g.workspace) ? "listed" : "unlisted";
}

let home: Promise<string> | undefined;

/** The home workspace (~/.blitz/home), asked of the service once; "" when it can't say. */
export function globalHome(): Promise<string> {
  home ??= workers.listAllWorkers({}).then(
    (r) => r.home,
    () => {
      home = undefined; // asked again next time
      return "";
    },
  );
  return home;
}
