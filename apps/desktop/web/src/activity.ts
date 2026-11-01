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

import { useEffect } from "react";
import { markUsed } from "./desktop";

/** How often, at most, the page tells the host it's in use. */
export const USED_EVERY_MS = 10 * 60 * 1000;

/**
 * A function that marks use (mark), at most every USED_EVERY_MS and only
 * while the page is shown (hidden): the hook's logic, apart for tests.
 */
export function usedThrottle(now: () => number, hidden: () => boolean, mark: () => void) {
  let last = -Infinity;
  return () => {
    if (hidden() || now() - last < USED_EVERY_MS) return;
    last = now();
    mark();
  };
}

/**
 * Tells the host the window is in use when it opens and each time it comes
 * forward (focus, or shown again), at most every USED_EVERY_MS: the host
 * counts the install as active once a day (spec_054_active_installs AI-01).
 */
export function useCountUse() {
  useEffect(() => {
    const used = usedThrottle(Date.now, () => document.visibilityState === "hidden", markUsed);
    const shown = () => {
      if (document.visibilityState === "visible") used();
    };
    used();
    window.addEventListener("focus", used);
    document.addEventListener("visibilitychange", shown);
    return () => {
      window.removeEventListener("focus", used);
      document.removeEventListener("visibilitychange", shown);
    };
  }, []);
}
