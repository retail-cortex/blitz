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

// The log viewer's formatting.

/** A record's time of day, to the millisecond. */
export function logTime(d: Date): string {
  const p = (n: number, w = 2) => String(n).padStart(w, "0");
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
}

/**
 * Whether day's log (YYYY-MM-DD) can be deleted: a day before now's.
 * Today's is the one the service is writing, and "" is the newest.
 */
export function canDelete(day: string, now: Date): boolean {
  const p = (n: number) => String(n).padStart(2, "0");
  return /^\d{4}-\d{2}-\d{2}$/.test(day) && day < `${now.getFullYear()}-${p(now.getMonth() + 1)}-${p(now.getDate())}`;
}
