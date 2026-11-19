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

// The service restarting or starting at the app's request (spec_024_desktop
// DSK-51d): while it is, the window shows that it's on its way, never a
// button to start a service that's already starting.
import { useEffect, useState } from "react";

/** How long a restart or start is waited for before the window offers to start it again. */
export const restartWait = 60_000;
/** How long a service that starts at login gets to come up before the window offers to start it (launchd and systemd start it themselves). */
export const loginStartWait = 10_000;

let until = 0;
let kind: "restart" | "start" = "restart";
const subs = new Set<() => void>();
const changed = () => subs.forEach((f) => f());

/** Says the app has asked the service to restart (or start), for at most ms. */
export function markRestarting(what: "restart" | "start" = "restart", ms = restartWait) {
  until = Date.now() + ms;
  kind = what;
  changed();
}

/** Says the service is up again, or the request failed. */
export function restartDone() {
  if (!until) return;
  until = 0;
  changed();
}

/** What the service is doing at the app's request now: "" for nothing. */
export function restarting(now = Date.now()): "" | "restart" | "start" {
  return now < until ? kind : "";
}

/** restarting, kept current: it ends when the service is up, or when the wait runs out. */
export function useRestarting(): "" | "restart" | "start" {
  const [, render] = useState(0);
  useEffect(() => {
    const f = () => render((n) => n + 1);
    subs.add(f);
    const left = until - Date.now();
    const timer = left > 0 ? setTimeout(f, left + 10) : undefined;
    return () => {
      subs.delete(f);
      clearTimeout(timer);
    };
  });
  return restarting();
}
