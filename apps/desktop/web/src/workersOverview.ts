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
// WK-61–WK-63): what's said of each workspace, the home workspace the
// global workers run in, and the global limits' fields.

import { workers } from "./api";
import type { WorkerPolicySettings, WorkspaceWorkers } from "./gen/blitz/v1/worker_pb";

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

/** The permission kinds a worker may be given (workers.policy.allow), in order. */
export const permissionKinds = ["shell", "write", "delete", "web", "mcp"] as const;

/** A global limit's setting, under workers.policy. */
export type LimitField = "max_concurrent" | "default_max_turns" | "max_turns" | "default_max_cost_usd" | "max_cost_usd" | "default_timeout" | "max_timeout";

/** The limits as the form's fields show them (a number unset is empty). */
export function limitFields(p: WorkerPolicySettings): Record<LimitField, string> {
  const n = (v: number) => (v ? String(v) : "");
  return {
    max_concurrent: n(p.maxConcurrent),
    default_max_turns: n(p.defaultMaxTurns),
    max_turns: n(p.maxTurns),
    default_max_cost_usd: n(p.defaultMaxCostUsd),
    max_cost_usd: n(p.maxCostUsd),
    default_timeout: p.defaultTimeout,
    max_timeout: p.maxTimeout,
  };
}

/** workers.policy.allow with kind turned on or off, in order ("none" when none is left). */
export function toggleKind(allow: readonly string[], kind: string): string {
  const on = new Set(allow);
  if (on.has(kind)) on.delete(kind);
  else on.add(kind);
  const out = permissionKinds.filter((k) => on.has(k));
  return out.length ? out.join(", ") : "none";
}
