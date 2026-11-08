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

import { files } from "../api";

/** What a watch tells the tree (FIL-33b). */
export interface FolderWatch {
  /** Folders whose entries changed: list them again. */
  changed: (folders: string[]) => void;
  /**
   * Whether the watch is on for every folder (live); false while it's
   * down (the service restarted) or the system can't watch them all, when
   * the tree lists on its timer.
   */
  live: (on: boolean) => void;
}

/** How long a broken watch waits before it's asked again. */
export const watchRetryMs = 2000;

/**
 * Watches a workspace's folders (the tree's open ones) through the
 * service's WatchFiles stream until stopped, asking again after the
 * stream breaks. Returns stop.
 */
export function watchFolders(dir: string, folders: string[], on: FolderWatch, retryMs = watchRetryMs): () => void {
  const ctl = new AbortController();
  void (async () => {
    while (!ctl.signal.aborted) {
      try {
        for await (const m of files.watchFiles({ workspace: dir, folders }, { signal: ctl.signal })) {
          // The first message, and the page server's keepalives, carry no
          // folders: the watch is on.
          on.live(!m.polling);
          if (m.folders.length) on.changed(m.folders);
        }
      } catch {
        // the stream broke (the service restarted): watch again shortly
      }
      if (ctl.signal.aborted) return;
      on.live(false);
      await new Promise((r) => setTimeout(r, retryMs));
    }
  })();
  return () => ctl.abort();
}
