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

// A workspace's project settings (.blitz/settings.toml) in words, as the
// REPL says them (spec_project_config_031), from the same catalogs.
import type { ProjectItem, ProjectSettings } from "./gen/blitz/v1/workspace_pb";
import { t } from "./i18n";

const itemKinds = new Set(["hook", "mcp", "allow", "writable", "model", "agent_model", "worker_allow", "skill_scripts", "deny", "ask", "blocked_path"]);

/** One project setting in words: "hook stop runs ./scripts/stop.sh". */
export function projectItemText(it: Pick<ProjectItem, "kind" | "key" | "value" | "file">): string {
  let kind = it.kind;
  if (kind === "limit" || kind === "skill_policy" || kind === "worker_limit") kind = "limit";
  else if (!itemKinds.has(kind)) kind = "setting";
  return t(`project.item.${kind}`, { key: it.key, value: it.value, file: it.file });
}

/** Why a project setting was ignored, in words. */
export function projectReason(reason: string): string {
  return t(`project.reason.${reason}`);
}

/** The project settings' trust state in words. */
export function projectState(p: Pick<ProjectSettings, "state" | "loaded">): string {
  if (p.loaded && p.state !== "trusted") return t("project.state.for_run");
  if (p.state === "trusted" && !p.loaded) return t("project.state.trusted_next_open");
  return t(`project.state.${p.state || "none"}`);
}

/** The settings wait for a first or new decision. */
export function needsDecision(p?: Pick<ProjectSettings, "state" | "loaded">): boolean {
  return !!p && !p.loaded && (p.state === "new" || p.state === "changed");
}

/** How many settings are off for want of trust (0 when they're in force). */
export function waiting(p?: Pick<ProjectSettings, "loaded" | "pending">): number {
  return p && !p.loaded ? p.pending.length : 0;
}
