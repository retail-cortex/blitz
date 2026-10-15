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

// What the history view shows beside each commit (FIL-59b): which date
// group it's in, its author's colour, and how big its change is.

/** A commit's date group: today, yesterday, this week, this month, or its month. */
export type DateGroup = { kind: "today" | "yesterday" | "week" | "month" } | { kind: "older"; year: number; month: number };

/** The group a commit made at when falls in, seen at now (local time). */
export function dateGroup(when: Date, now: Date): DateGroup {
  const day = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((day(now) - day(when)) / 86_400_000);
  if (days <= 0) return { kind: "today" };
  if (days === 1) return { kind: "yesterday" };
  if (days < 7) return { kind: "week" };
  if (when.getFullYear() === now.getFullYear() && when.getMonth() === now.getMonth()) return { kind: "month" };
  return { kind: "older", year: when.getFullYear(), month: when.getMonth() };
}

/** Whether two groups are the same. */
export function sameGroup(a: DateGroup, b: DateGroup): boolean {
  return a.kind === b.kind && (a.kind !== "older" || (b.kind === "older" && a.year === b.year && a.month === b.month));
}

/** An author's colour, the same for the same address every time. */
export function authorHue(email: string): number {
  let h = 0;
  for (const ch of email.toLowerCase()) h = (h * 31 + ch.codePointAt(0)!) >>> 0;
  return h % 360;
}

/** How wide a commit's change bar is (0 to 1), against the largest change shown. */
export function changeShare(lines: number, largest: number): number {
  if (lines <= 0 || largest <= 0) return 0;
  return Math.max(0.08, Math.min(1, Math.log1p(lines) / Math.log1p(largest)));
}
