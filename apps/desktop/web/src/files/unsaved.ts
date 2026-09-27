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

// Unsaved changes across the window's workspaces, for closing the window
// (the app asks first, spec_files_029 FIL-45).
import { setUnsaved } from "../desktop";
import { t, tn } from "../i18n";

const counts = new Map<string, number>();

/** Records a workspace's number of files with unsaved changes. */
export function reportUnsaved(dir: string, count: number) {
  if ((counts.get(dir) ?? 0) === count) return;
  if (count) counts.set(dir, count);
  else counts.delete(dir);
  const total = [...counts.values()].reduce((a, b) => a + b, 0);
  setUnsaved(total ? tn("desktop.files.quit_unsaved", total) : "", t("desktop.files.quit"), t("desktop.cancel"));
}

/** The number of files with unsaved changes in a workspace. */
export function unsavedIn(dir: string): number {
  return counts.get(dir) ?? 0;
}
