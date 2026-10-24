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

// The job editor's schedule picker (spec_workers_023 WK-72): what repeats
// and when, as a cron expression the service reads exactly, and back from
// a job's schedule when it's one the picker makes. Anything else (plain
// text, other cron) is Custom, kept as written.

/** How often a job runs, as the picker offers it. */
export type Repeat = "hourly" | "daily" | "weekdays" | "weekly" | "monthly" | "custom";

/** The picker's state. */
export interface SchedulePick {
  repeat: Repeat;
  /** The time, "HH:MM" (hourly: only its minutes count). */
  time: string;
  /** Weekly: the days, 0 (Sunday) to 6. */
  days: number[];
  /** Monthly: the day of the month, 1 to 28 (every month has it). */
  day: number;
  /** Custom: the schedule as written. */
  text: string;
}

/** A new job's schedule: every day at 9:00. */
export const defaultPick: SchedulePick = { repeat: "daily", time: "09:00", days: [1], day: 1, text: "" };

const hm = (time: string): [number, number] => {
  const m = /^(\d{1,2}):(\d{2})$/.exec(time.trim());
  if (!m) return [9, 0];
  return [Math.min(23, Number(m[1])), Math.min(59, Number(m[2]))];
};
const pad = (n: number) => String(n).padStart(2, "0");

/** The schedule the job's file gets for p. */
export function toSchedule(p: SchedulePick): string {
  const [h, m] = hm(p.time);
  switch (p.repeat) {
    case "hourly":
      return `${m} * * * *`;
    case "daily":
      return `${m} ${h} * * *`;
    case "weekdays":
      return `${m} ${h} * * 1-5`;
    case "weekly":
      return `${m} ${h} * * ${[...new Set(p.days)].sort((a, b) => a - b).join(",") || "1"}`;
    case "monthly":
      return `${m} ${h} ${Math.max(1, Math.min(28, Math.floor(p.day) || 1))} * *`;
    case "custom":
      return p.text.trim();
  }
}

/** The picker's state for a job's schedule: one of its own forms, else Custom with the text. */
export function fromSchedule(schedule: string): SchedulePick {
  const s = schedule.trim();
  if (!s) return { ...defaultPick };
  const custom: SchedulePick = { ...defaultPick, repeat: "custom", text: s };
  if (s === "@hourly") return { ...defaultPick, repeat: "hourly", time: "00:00" };
  if (s === "@daily" || s === "@midnight") return { ...defaultPick, repeat: "daily", time: "00:00" };
  const f = s.split(/\s+/);
  if (f.length !== 5 || !/^\d{1,2}$/.test(f[0])) return custom;
  const m = Number(f[0]);
  if (m > 59) return custom;
  if (f[1] === "*" && f[2] === "*" && f[3] === "*" && f[4] === "*") return { ...defaultPick, repeat: "hourly", time: `00:${pad(m)}` };
  if (!/^\d{1,2}$/.test(f[1]) || Number(f[1]) > 23 || f[3] !== "*") return custom;
  const time = `${pad(Number(f[1]))}:${pad(m)}`;
  if (f[2] === "*" && f[4] === "*") return { ...defaultPick, repeat: "daily", time };
  if (f[2] === "*" && f[4] === "1-5") return { ...defaultPick, repeat: "weekdays", time };
  if (f[2] === "*" && /^[0-6](,[0-6])*$/.test(f[4])) return { ...defaultPick, repeat: "weekly", time, days: f[4].split(",").map(Number) };
  if (/^\d{1,2}$/.test(f[2]) && Number(f[2]) >= 1 && Number(f[2]) <= 28 && f[4] === "*") return { ...defaultPick, repeat: "monthly", time, day: Number(f[2]) };
  return custom;
}

/** A month as the calendar shows it: its weeks (Sunday first), each a day of the month or 0 outside it. */
export function monthGrid(year: number, month: number): number[][] {
  const first = new Date(year, month, 1).getDay();
  const days = new Date(year, month + 1, 0).getDate();
  const cells = [...Array<number>(first).fill(0), ...Array.from({ length: days }, (_, i) => i + 1)];
  while (cells.length % 7) cells.push(0);
  const weeks: number[][] = [];
  for (let i = 0; i < cells.length; i += 7) weeks.push(cells.slice(i, i + 7));
  return weeks;
}

/** The days of runs (as the viewer's dates) in a year and month, for the calendar's marks. */
export function runDays(runs: Date[], year: number, month: number): Set<number> {
  return new Set(runs.filter((d) => d.getFullYear() === year && d.getMonth() === month).map((d) => d.getDate()));
}
