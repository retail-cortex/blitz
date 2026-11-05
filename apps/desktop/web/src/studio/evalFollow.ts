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

// Following an eval run (spec_042 EV-07a), for the Evals pane and a
// skill's Scenarios tab alike: started (with its results so far), each
// result as it's scored, then finished.

import { useCallback, useEffect, useRef, useState } from "react";
import type { EvalResult, EvalRun, RunEvalResponse, WatchEvalRunResponse } from "../gen/blitz/v1/workspace_pb";

/** A run being followed: the run (once started), its results so far, and how many there will be. */
export interface LiveRun {
  run: EvalRun | null;
  results: EvalResult[];
  total: number;
}

/** Whether a run's status is an end. */
export const isEnded = (status: string | undefined) => status === "passed" || status === "failed" || status === "stopped";

/** The live run as an event of the run's stream leaves it. */
export function liveAfter(l: LiveRun, ev: (RunEvalResponse | WatchEvalRunResponse)["event"]): LiveRun {
  switch (ev.case) {
    case "started":
      return { ...l, run: { ...ev.value, results: [] }, results: ev.value.results };
    case "result":
      return { ...l, results: [...l.results, ev.value] };
    case "finished":
      return { ...l, run: ev.value, results: ev.value.results };
  }
  return l;
}

/**
 * Follows one eval run at a time into live. onFinished hears the run as it
 * ends, onError a stream that broke, and onDone that the following ended
 * either way (for lists to load again); leaving (another follow, stop or
 * unmounting) stops only the following: the run goes on in the service.
 */
export function useEvalFollow(on: { onFinished: (run: EvalRun) => void; onError: (e: unknown) => void; onDone?: () => void }) {
  const [live, setLive] = useState<LiveRun | null>(null);
  const following = useRef<AbortController | null>(null);
  const handlers = useRef(on);
  useEffect(() => {
    handlers.current = on;
  });
  useEffect(() => () => following.current?.abort(), []);
  const stopFollowing = useCallback(() => following.current?.abort(), []);
  const follow = useCallback(async (total: number, events: (signal: AbortSignal) => AsyncIterable<RunEvalResponse | WatchEvalRunResponse>) => {
    following.current?.abort();
    const ctl = new AbortController();
    following.current = ctl;
    setLive({ run: null, results: [], total });
    try {
      for await (const res of events(ctl.signal)) {
        const ev = res.event;
        setLive((l) => l && liveAfter(l, ev));
        if (ev.case === "finished") handlers.current.onFinished(ev.value);
      }
    } catch (e) {
      if (ctl.signal.aborted) return; // left: the run goes on
      setLive(null);
      handlers.current.onError(e);
    } finally {
      if (following.current === ctl) following.current = null;
    }
    handlers.current.onDone?.();
  }, []);
  return { live, setLive, follow, stopFollowing, running: !!live && !isEnded(live.run?.status) };
}
