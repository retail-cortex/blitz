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

import { useEffect, useMemo, useRef, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { mdiCheck } from "@mdi/js";
import { workers } from "./api";
import { message } from "./errors";
import { language, t } from "./i18n";
import { fromSchedule, monthGrid, runDays, toSchedule, type Repeat, type SchedulePick } from "./scheduleForm";
import { Chip } from "./ui/controls";

const repeats: Repeat[] = ["hourly", "daily", "weekdays", "weekly", "monthly", "custom"];

/** The time zones the browser knows, for the time zone field's suggestions. */
const zones: string[] = (() => {
  try {
    return (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.("timeZone") ?? [];
  } catch {
    return [];
  }
})();

/**
 * When a job runs (spec_workers_023 WK-72): what repeats (every hour, day,
 * weekday, chosen weekdays or a day of the month) and at what time, or a
 * Custom schedule as text or cron; the time zone; and the next runs as the
 * service reads them (PreviewSchedule), listed and marked on a month.
 * schedule is the job's, which it sets as cron for the picker's own forms.
 */
export function SchedulePicker({ schedule, timezone, onSchedule, onTimezone }: { schedule: string; timezone: string; onSchedule: (s: string) => void; onTimezone: (tz: string) => void }) {
  const [p, setP] = useState<SchedulePick>(() => fromSchedule(schedule));
  // Read again only when the schedule changes from outside (a reload).
  const sent = useRef(schedule);
  useEffect(() => {
    if (schedule !== sent.current) {
      sent.current = schedule;
      setP(fromSchedule(schedule));
    }
  }, [schedule]);
  const change = (q: SchedulePick) => {
    setP(q);
    sent.current = toSchedule(q);
    onSchedule(sent.current);
  };
  // A new job starts at the picker's default, every day at 9:00.
  useEffect(() => {
    if (!schedule.trim()) change(p);
    // Once, for a form that came with no schedule.
  }, []);

  // The service's reading of it, as it's changed.
  const [preview, setPreview] = useState<{ runs: Date[]; zone: string; problem: string }>();
  const text = toSchedule(p);
  useEffect(() => {
    if (!text) return setPreview(undefined);
    let live = true;
    const id = setTimeout(() => {
      workers.previewSchedule({ schedule: text, timezone: timezone.trim(), count: 8 }).then(
        (r) => live && setPreview({ runs: r.nextRuns.map((x) => timestampDate(x)), zone: r.timezone, problem: r.problem }),
        (e: unknown) => live && setPreview({ runs: [], zone: "", problem: message(e) }),
      );
    }, 250);
    return () => {
      live = false;
      clearTimeout(id);
    };
  }, [text, timezone]);

  const weekday = useMemo(() => new Intl.DateTimeFormat(language(), { weekday: "short" }), []);
  const dayNames = [0, 1, 2, 3, 4, 5, 6].map((d) => weekday.format(new Date(2026, 9, 18 + d))); // 18 Oct 2026 is a Sunday
  const when = (d: Date) => d.toLocaleString(language(), { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", timeZone: preview?.zone || undefined });
  const first = preview?.runs[0];
  const month = first ? { y: first.getFullYear(), m: first.getMonth() } : undefined;

  return (
    <div className="stack schedule-picker" style={{ gap: 8 }}>
      <div className="pair">
        <label className="field">
          <span className="t-label">{t("desktop.schedule.repeats")}</span>
          <select className="select" value={p.repeat} onChange={(e) => change({ ...p, repeat: e.target.value as Repeat, text: p.repeat === "custom" ? p.text : text })}>
            {repeats.map((r) => (
              <option key={r} value={r}>
                {t(`desktop.schedule.repeat.${r}`)}
              </option>
            ))}
          </select>
        </label>
        {p.repeat === "custom" ? (
          <label className="field">
            <span className="t-label">{t("desktop.newworker.schedule")}</span>
            <input className="input mono" value={p.text} spellCheck={false} placeholder="Weekdays at 9:30" onChange={(e) => change({ ...p, text: e.target.value })} />
            <span className="t-body-sm muted">{t("desktop.newworker.schedule_hint")}</span>
          </label>
        ) : (
          <label className="field">
            <span className="t-label">{t(p.repeat === "hourly" ? "desktop.schedule.minute" : "desktop.schedule.time")}</span>
            {p.repeat === "hourly" ? (
              <input className="input" type="number" min={0} max={59} value={Number(p.time.split(":")[1] ?? 0)} onChange={(e) => change({ ...p, time: `00:${String(Math.max(0, Math.min(59, Number(e.target.value) || 0))).padStart(2, "0")}` })} />
            ) : (
              <input className="input" type="time" value={p.time} onChange={(e) => change({ ...p, time: e.target.value || p.time })} />
            )}
          </label>
        )}
      </div>
      {p.repeat === "weekly" && (
        <div className="row wrap" style={{ gap: 6 }} role="group" aria-label={t("desktop.schedule.days")}>
          {dayNames.map((name, d) => {
            const on = p.days.includes(d);
            return (
              <Chip key={d} selected={on} icon={on ? mdiCheck : undefined} aria-pressed={on} onClick={() => change({ ...p, days: on ? p.days.filter((x) => x !== d) : [...p.days, d] })}>
                {name}
              </Chip>
            );
          })}
        </div>
      )}
      {p.repeat === "monthly" && (
        <label className="field">
          <span className="t-label">{t("desktop.schedule.day_of_month")}</span>
          <input className="input" type="number" min={1} max={28} value={p.day} onChange={(e) => change({ ...p, day: Number(e.target.value) || 1 })} />
          <span className="t-body-sm muted">{t("desktop.schedule.day_hint")}</span>
        </label>
      )}
      <label className="field">
        <span className="t-label">{t("desktop.newworker.timezone")}</span>
        <input className="input" list="schedule-zones" value={timezone} placeholder={t("desktop.schedule.zone_placeholder")} spellCheck={false} onChange={(e) => onTimezone(e.target.value)} />
        <datalist id="schedule-zones">
          {zones.map((z) => (
            <option key={z} value={z} />
          ))}
        </datalist>
        <span className="t-body-sm muted">{t("desktop.newworker.timezone_hint")}</span>
      </label>
      {preview?.problem ? (
        <p className="t-body-sm error-text" role="alert">
          {t("desktop.schedule.problem", { problem: preview.problem })}
        </p>
      ) : (
        preview &&
        month && (
          <div className="schedule-preview">
            <div className="stack" style={{ gap: 2 }}>
              <span className="t-label">{t("desktop.schedule.next_runs")}</span>
              {preview.runs.slice(0, 5).map((d) => (
                <span key={d.getTime()} className="t-body-sm">
                  {when(d)}
                </span>
              ))}
              <span className="t-body-sm muted mono">{text}</span>
            </div>
            <table className="schedule-month" aria-label={new Date(month.y, month.m).toLocaleString(language(), { month: "long", year: "numeric" })}>
              <caption className="t-label-sm">{new Date(month.y, month.m).toLocaleString(language(), { month: "long", year: "numeric" })}</caption>
              <thead>
                <tr>
                  {dayNames.map((n) => (
                    <th key={n} className="t-label-sm muted">
                      {n.slice(0, 2)}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {monthGrid(month.y, month.m).map((week, i) => {
                  const marks = runDays(preview.runs, month.y, month.m);
                  return (
                    <tr key={i}>
                      {week.map((d, j) => (
                        <td key={j} className={d && marks.has(d) ? "has-run" : ""}>
                          {d || ""}
                        </td>
                      ))}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )
      )}
    </div>
  );
}
