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


// The desktop app's updates (spec_electron_038 EL-90): what the page says
// about one, and the check when the app opens that downloads it, ready to
// restart.
import { useEffect, useRef } from "react";
import { canUpdate, checkUpdate, downloadUpdate, restartToUpdate, type UpdateStatus } from "./desktop";
import { t } from "./i18n";

/** A message and its values. */
export type Line = { key: string; vars?: Record<string, string> };

/** What Settings says about the update. */
export function updateLine(s: UpdateStatus | null): Line {
  if (!s) return { key: "desktop.update.checking" };
  if (!s.can) return { key: "desktop.update.cannot", vars: { reason: s.reason ?? "" } };
  if (s.error) return { key: "desktop.update.failed", vars: { error: s.error } };
  if (s.ready) return { key: "desktop.update.ready", vars: { version: s.latest } };
  if (s.downloading) return { key: "desktop.update.downloading", vars: { version: s.latest } };
  if (s.available) return { key: "desktop.update.available", vars: { version: s.latest } };
  return { key: "desktop.update.latest", vars: { version: s.current } };
}

/** Whether to start downloading: one the app can take, not had yet. */
export const shouldDownload = (s: UpdateStatus) => s.can && s.available && !s.ready && !s.downloading && !s.error;

/**
 * When the app opens (unless updates are off), looks for a release (the
 * host asks GitHub only five days after its last check), downloads it if
 * there's one, and says when it's ready, with a Restart action. Nothing
 * repeats while the app runs.
 */
export function useAutoUpdate(on: boolean, announce: (s: UpdateStatus) => void) {
  const say = useRef(announce);
  say.current = announce;
  const done = useRef(false);
  useEffect(() => {
    if (!on || done.current || !canUpdate()) return;
    done.current = true;
    void (async () => {
      try {
        let s = await checkUpdate(false);
        if (shouldDownload(s)) s = await downloadUpdate();
        if (s.ready) say.current(s);
      } catch {
        // offline, or GitHub unreachable: the next time the app opens
      }
    })();
  }, [on]);
}

/** Restarts into the update, or says what stopped it. */
export async function restart(fail: (message: string) => void) {
  try {
    await restartToUpdate();
  } catch (e) {
    fail(t("desktop.update.failed", { error: String(e instanceof Error ? e.message : e) }));
  }
}
