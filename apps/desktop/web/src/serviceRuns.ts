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

// The runs going now on the organization's service (spec_053 EN-42): one
// stream (WatchServiceRuns) serves the Processes view and Studio's service
// jobs, which refresh as a run of theirs changes instead of asking again
// and again.

import { useEffect, useState, useSyncExternalStore } from "react";
import { config } from "./api";
import { pause } from "./pause";
import type { GoingServiceRun, ServiceRunChange } from "./gen/blitz/v1/config_pb";

/** How long to wait before following the stream again after it ends (not connected, signed out, an older service). */
const retryMs = 30_000;

/** What the store holds: the runs going now, and the last change heard. */
export interface ServiceRunsState {
  runs: GoingServiceRun[];
  change?: ServiceRunChange;
  /** Counts the changes, so the same change twice is two. */
  changes: number;
}

let state: ServiceRunsState = { runs: [], changes: 0 };
const listeners = new Set<() => void>();
let watching: AbortController | undefined;

function take(runs: GoingServiceRun[], change?: ServiceRunChange) {
  state = { runs, change: change ?? state.change, changes: state.changes + (change ? 1 : 0) };
  for (const f of listeners) f();
}

async function watch(ctl: AbortController) {
  while (!ctl.signal.aborted) {
    try {
      for await (const r of config.watchServiceRuns({}, { signal: ctl.signal })) {
        if (r.listed) take(r.runs, r.change);
      }
    } catch {
      // Not connected or signed out, a license without jobs, or a dropped stream.
    }
    if (ctl.signal.aborted) return;
    if (state.runs.length) take([]);
    await pause(retryMs, ctl.signal);
  }
}

function subscribe(f: () => void) {
  listeners.add(f);
  if (listeners.size === 1) {
    watching = new AbortController();
    void watch(watching);
  }
  return () => {
    listeners.delete(f);
    if (listeners.size === 0) {
      watching?.abort();
    }
  };
}

/** The service's runs going now, and its last change, kept up to date while in use. */
export function useServiceRuns(): ServiceRunsState {
  return useSyncExternalStore(subscribe, () => state);
}

/**
 * The service's runs paused for the person's answer (spec_053 EN-43),
 * asked again as the runs' stream says one changed; reload asks now.
 */
export function useServiceApprovals(): { runs: GoingServiceRun[]; reload: () => void } {
  const { changes } = useServiceRuns();
  const [runs, setRuns] = useState<GoingServiceRun[]>([]);
  const [n, setN] = useState(0);
  useEffect(() => {
    let live = true;
    config.listServiceApprovals({}).then(
      (r) => live && setRuns(r.runs),
      () => live && setRuns([]), // not connected, signed out, or no jobs on this license
    );
    return () => {
      live = false;
    };
  }, [changes, n]);
  return { runs, reload: () => setN((x) => x + 1) };
}
