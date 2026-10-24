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

/** What the summary says for p: a key (desktop.schedule.says.<repeat>) and its values, in locale. */
export function describe(p: SchedulePick, locale: string): { key: string; values: Record<string, string> } {
  const [h, m] = hm(p.time);
  const time = new Intl.DateTimeFormat(locale, { hour: "numeric", minute: "2-digit" }).format(new Date(2026, 0, 5, h, m));
  const weekday = new Intl.DateTimeFormat(locale, { weekday: "long" });
  const dayName = (d: number) => weekday.format(new Date(2026, 9, 18 + d)); // 18 Oct 2026 is a Sunday
  switch (p.repeat) {
    case "hourly":
      return { key: "desktop.schedule.says.hourly", values: { minute: pad(m) } };
    case "weekly": {
      const days = [...new Set(p.days)].sort((a, b) => a - b);
      const list = new Intl.ListFormat(locale, { type: "conjunction" }).format((days.length ? days : [1]).map(dayName));
      return { key: "desktop.schedule.says.weekly", values: { days: list, time } };
    }
    case "monthly":
      return { key: "desktop.schedule.says.monthly", values: { day: String(Math.max(1, Math.min(28, Math.floor(p.day) || 1))), time } };
    case "custom":
      return { key: "desktop.schedule.says.custom", values: { text: p.text.trim() } };
    default:
      return { key: `desktop.schedule.says.${p.repeat}`, values: { time } };
  }
}

/** When d is from now, in words ("tomorrow", "in 3 days", "in 5 hours"). */
export function fromNow(d: Date, now: Date, locale: string): string {
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  const minutes = Math.round((d.getTime() - now.getTime()) / 60000);
  if (Math.abs(minutes) < 60) return rtf.format(minutes, "minute");
  const midnight = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((midnight(d) - midnight(now)) / 86400000);
  if (days === 0) return rtf.format(Math.round(minutes / 60), "hour");
  return rtf.format(days, "day");
}

/** A time zone as the dropdown offers it. */
export interface ZoneOption {
  value: string;
  /** Its place ("Chicago", "Buenos Aires") and offset now ("UTC−05:00"). */
  label: string;
}

/** zone's offset from UTC at now, as "UTC+05:30" ("UTC" for none). */
export function zoneOffset(zone: string, now: Date): string {
  try {
    const part = new Intl.DateTimeFormat("en-US", { timeZone: zone, timeZoneName: "longOffset" }).formatToParts(now).find((p) => p.type === "timeZoneName")?.value ?? "";
    const off = part.replace(/^GMT/, "");
    return off ? `UTC${off.replace("-", "−")}` : "UTC";
  } catch {
    return "";
  }
}

/**
 * The time zones grouped by region (America, Europe, …), each labelled
 * by its place and offset, by place; zones without a region (UTC) under
 * "Other". current is added when it isn't among them.
 */
export function zoneGroups(zones: readonly string[], now: Date, current = ""): { region: string; zones: ZoneOption[] }[] {
  const all = [...new Set([...zones, ...(current ? [current] : [])])];
  const groups = new Map<string, ZoneOption[]>();
  for (const z of all) {
    const slash = z.indexOf("/");
    const region = slash > 0 ? z.slice(0, slash) : "Other";
    const place = (slash > 0 ? z.slice(slash + 1) : z).replace(/_/g, " ").replace(/\//g, " / ");
    const off = zoneOffset(z, now);
    if (!groups.has(region)) groups.set(region, []);
    groups.get(region)!.push({ value: z, label: off ? `${place} (${off})` : place });
  }
  return [...groups.entries()]
    .sort(([a], [b]) => (a === "Other" ? 1 : b === "Other" ? -1 : a.localeCompare(b)))
    .map(([region, zs]) => ({ region, zones: zs.sort((a, b) => a.label.localeCompare(b.label)) }));
}
