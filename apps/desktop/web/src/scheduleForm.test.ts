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

import { describe, expect, it } from "vitest";
import { defaultPick, fromSchedule, monthGrid, runDays, toSchedule, type SchedulePick } from "./scheduleForm";

const pick = (p: Partial<SchedulePick>): SchedulePick => ({ ...defaultPick, ...p });

describe("toSchedule", () => {
  it.each([
    ["every hour at :15", pick({ repeat: "hourly", time: "10:15" }), "15 * * * *"],
    ["every day at 8:00", pick({ repeat: "daily", time: "08:00" }), "0 8 * * *"],
    ["weekdays at 9:30", pick({ repeat: "weekdays", time: "09:30" }), "30 9 * * 1-5"],
    ["Mondays and Fridays, days sorted and once", pick({ repeat: "weekly", time: "20:00", days: [5, 1, 5] }), "0 20 * * 1,5"],
    ["a week without days: Monday", pick({ repeat: "weekly", days: [] }), "0 9 * * 1"],
    ["the 15th of each month", pick({ repeat: "monthly", time: "06:45", day: 15 }), "45 6 15 * *"],
    ["a day past the 28th: the 28th", pick({ repeat: "monthly", day: 31 }), "0 9 28 * *"],
    ["custom, as written", pick({ repeat: "custom", text: "  every two hours " }), "every two hours"],
    ["a bad time: 9:00", pick({ repeat: "daily", time: "soon" }), "0 9 * * *"],
  ])("%s", (_name, p, want) => expect(toSchedule(p)).toBe(want));
});

describe("fromSchedule", () => {
  it.each([
    ["", pick({})],
    ["0 8 * * *", pick({ repeat: "daily", time: "08:00" })],
    ["15 * * * *", pick({ repeat: "hourly", time: "00:15" })],
    ["30 9 * * 1-5", pick({ repeat: "weekdays", time: "09:30" })],
    ["0 20 * * 1,5", pick({ repeat: "weekly", time: "20:00", days: [1, 5] })],
    ["45 6 15 * *", pick({ repeat: "monthly", time: "06:45", day: 15 })],
    ["@daily", pick({ repeat: "daily", time: "00:00" })],
    ["@hourly", pick({ repeat: "hourly", time: "00:00" })],
    ["Daily at 6 AM", pick({ repeat: "custom", text: "Daily at 6 AM" })],
    ["*/5 * * * *", pick({ repeat: "custom", text: "*/5 * * * *" })],
    ["0 8 31 * *", pick({ repeat: "custom", text: "0 8 31 * *" })],
    ["0 8 * 6 *", pick({ repeat: "custom", text: "0 8 * 6 *" })],
  ])("%j", (schedule, want) => expect(fromSchedule(schedule)).toEqual(want));

  it("round-trips the picker's own schedules", () => {
    for (const p of [pick({ repeat: "hourly", time: "00:05" }), pick({ repeat: "weekly", time: "07:00", days: [0, 6] }), pick({ repeat: "monthly", day: 3 })]) {
      expect(fromSchedule(toSchedule(p))).toEqual(p);
    }
  });
});

describe("the calendar", () => {
  it("lays a month out in weeks from Sunday", () => {
    const oct = monthGrid(2026, 9); // October 2026 starts on a Thursday
    expect(oct[0]).toEqual([0, 0, 0, 0, 1, 2, 3]);
    expect(oct.flat().filter(Boolean)).toHaveLength(31);
    expect(oct.every((w) => w.length === 7)).toBe(true);
  });
  it("marks the days that have runs", () => {
    const runs = [new Date(2026, 9, 24, 8), new Date(2026, 9, 25, 8), new Date(2026, 10, 1, 8)];
    expect([...runDays(runs, 2026, 9)]).toEqual([24, 25]);
    expect([...runDays(runs, 2026, 10)]).toEqual([1]);
  });
});
