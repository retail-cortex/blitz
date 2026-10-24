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
import { mdiAlertCircleOutline, mdiCalendarClock, mdiCheck, mdiChevronLeft, mdiChevronRight } from "@mdi/js";
import { workers } from "./api";
import { message } from "./errors";
import { language, t } from "./i18n";
import { describe, fromNow, fromSchedule, monthGrid, runDays, toSchedule, zoneGroups, zoneOffset, type Repeat, type SchedulePick } from "./scheduleForm";
import { Chip, Icon, IconButton } from "./ui/controls";

const repeats: Repeat[] = ["hourly", "daily", "weekdays", "weekly", "monthly", "custom"];

/** The machine's own time zone, for the dropdown's first choice. */
const localZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

/** The weekdays' names, Sunday first, in the page's language. */
function weekdayNames(width: "short" | "narrow"): string[] {
  const f = new Intl.DateTimeFormat(language(), { weekday: width });
  return [0, 1, 2, 3, 4, 5, 6].map((d) => f.format(new Date(2026, 9, 18 + d))); // 18 Oct 2026 is a Sunday
}

/** The time zones the browser knows, for the time zone dropdown. */
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
      workers.previewSchedule({ schedule: text, timezone: timezone.trim(), count: 20 }).then(
        (r) => live && setPreview({ runs: r.nextRuns.map((x) => timestampDate(x)), zone: r.timezone, problem: r.problem }),
        (e: unknown) => live && setPreview({ runs: [], zone: "", problem: message(e) }),
      );
    }, 250);
    return () => {
      live = false;
      clearTimeout(id);
    };
  }, [text, timezone]);

  const dayNames = useMemo(() => weekdayNames("short"), []);
  const now = useMemo(() => new Date(), []);
  const groups = useMemo(() => zoneGroups(zones, now, timezone), [now, timezone]);

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
            <input className="input mono" value={p.text} spellCheck={false} placeholder={t("desktop.schedule.custom_placeholder")} onChange={(e) => change({ ...p, text: e.target.value })} />
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
        <select className="select" value={timezone} onChange={(e) => onTimezone(e.target.value)}>
          <option value="">{t("desktop.schedule.zone_service", { offset: zoneOffset(localZone, now) })}</option>
          {groups.map((g) => (
            <optgroup key={g.region} label={g.region}>
              {g.zones.map((z) => (
                <option key={z.value} value={z.value}>
                  {z.label}
                </option>
              ))}
            </optgroup>
          ))}
        </select>
      </label>
      {preview?.problem ? (
        <p className="card warn row t-body-sm" role="alert">
          <Icon path={mdiAlertCircleOutline} size="sm" />
          <span>{t("desktop.schedule.problem", { problem: preview.problem })}</span>
        </p>
      ) : (
        preview && <SchedulePreview pick={p} cron={text} runs={preview.runs} zone={preview.zone} now={now} />
      )}
    </div>
  );
}

/**
 * What the schedule does (WK-72): a sentence and its cron form, the next
 * runs as dated rows (when, and how far off), and a month with their
 * days marked, today ringed, stepped month by month.
 */
function SchedulePreview({ pick, cron, runs, zone, now }: { pick: SchedulePick; cron: string; runs: Date[]; zone: string; now: Date }) {
  const lang = language();
  const said = describe(pick, lang);
  const first = runs[0] ?? now;
  const [shown, setShown] = useState({ y: first.getFullYear(), m: first.getMonth() });
  useEffect(() => setShown({ y: first.getFullYear(), m: first.getMonth() }), [first.getFullYear(), first.getMonth()]);
  const step = (n: number) => setShown(({ y, m }) => ({ y: y + Math.floor((m + n) / 12), m: (((m + n) % 12) + 12) % 12 }));
  // The runs' days as the job's time zone has them, as the list says them.
  const ymd = new Intl.DateTimeFormat("en-CA", { year: "numeric", month: "numeric", day: "numeric", timeZone: zone || undefined });
  const marks = runDays(
    runs.map((d) => {
      const [y, m, dd] = ymd.format(d).split("-").map(Number);
      return new Date(y, m - 1, dd);
    }),
    shown.y,
    shown.m,
  );
  const isToday = (d: number) => d === now.getDate() && shown.m === now.getMonth() && shown.y === now.getFullYear();
  const narrow = weekdayNames("narrow");
  const fmt = (o: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat(lang, { ...o, timeZone: zone || undefined });
  const day = fmt({ day: "numeric" });
  const month = fmt({ month: "short" });
  const weekday = fmt({ weekday: "short" });
  const time = fmt({ hour: "numeric", minute: "2-digit" });
  return (
    <section className="card outlined schedule-preview" aria-label={t("desktop.schedule.next_runs")}>
      <div className="row schedule-summary">
        <Icon path={mdiCalendarClock} />
        <span className="t-title-sm spacer">{t(said.key, said.values)}</span>
        <code className="chip static mono" title={t("desktop.schedule.cron_title")}>
          {cron}
        </code>
      </div>
      <div className="schedule-body">
        <ol className="schedule-runs">
          {runs.slice(0, 5).map((d) => (
            <li key={d.getTime()} className="schedule-run">
              <span className="schedule-date" aria-hidden="true">
                <span className="schedule-date-month">{month.format(d)}</span>
                <span className="schedule-date-day">{day.format(d)}</span>
              </span>
              <span className="stack" style={{ gap: 0 }}>
                <span className="t-title-sm">
                  {weekday.format(d)} · {time.format(d)}
                </span>
                <span className="t-body-sm muted">{fromNow(d, now, lang)}</span>
              </span>
            </li>
          ))}
        </ol>
        <div className="schedule-calendar">
          <div className="row schedule-calendar-head">
            <IconButton icon={mdiChevronLeft} label={t("desktop.schedule.month_before")} small onClick={() => step(-1)} />
            <span className="t-title-sm spacer" style={{ textAlign: "center" }}>
              {new Date(shown.y, shown.m).toLocaleString(lang, { month: "long", year: "numeric" })}
            </span>
            <IconButton icon={mdiChevronRight} label={t("desktop.schedule.month_after")} small onClick={() => step(1)} />
          </div>
          <div className="schedule-grid" role="grid">
            {narrow.map((n, i) => (
              <span key={`h${i}`} className="schedule-weekday t-label muted" role="columnheader">
                {n}
              </span>
            ))}
            {monthGrid(shown.y, shown.m)
              .flat()
              .map((d, i) => (
                <span key={i} role="gridcell" aria-selected={!!d && marks.has(d)} className={`schedule-day ${d && marks.has(d) ? "has-run" : ""} ${d && isToday(d) ? "today" : ""}`}>
                  {d || ""}
                </span>
              ))}
          </div>
          <span className="t-body-sm muted">{zone ? t("desktop.schedule.in_zone", { zone, offset: zoneOffset(zone, now) }) : ""}</span>
        </div>
      </div>
    </section>
  );
}
