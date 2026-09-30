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
import { menuPosition, nudge } from "./controls";

describe("menuPosition", () => {
  const anchor = { top: 800, bottom: 826, left: 1200, right: 1300 };
  it.each([
    { placement: "down start", want: { bottom: 104, left: 1200, maxHeight: 788 } }, // no room below: flipped
    { placement: "up start", want: { bottom: 104, left: 1200, maxHeight: 788 } },
    { placement: "up end", want: { bottom: 104, right: 140, maxHeight: 788 } },
  ])("$placement", ({ placement, want }) => expect(menuPosition(anchor, placement, 1440, 900)).toEqual(want));

  it("keeps its side when that has room", () => {
    expect(menuPosition({ top: 40, bottom: 66, left: 0, right: 10 }, "down start", 1440, 900)).toEqual({ top: 70, left: 0, maxHeight: 822 });
  });
  it("flips up to down near the top", () => {
    expect(menuPosition({ top: 40, bottom: 66, left: 0, right: 10 }, "up start", 1440, 900)).toEqual({ top: 70, left: 0, maxHeight: 822 });
  });
  it("leaves a menu some height where there's hardly room", () => {
    expect(menuPosition({ top: 50, bottom: 60, left: 0, right: 10 }, "down start", 1440, 100).maxHeight).toBe(120);
  });
});

describe("nudge", () => {
  it.each([
    { name: "inside the window", box: { top: 100, bottom: 300, left: 100, right: 340 }, want: { x: 0, y: 0 } },
    { name: "past the right edge", box: { top: 100, bottom: 300, left: 1300, right: 1540 }, want: { x: -108, y: 0 } },
    { name: "past the bottom", box: { top: 800, bottom: 1000, left: 100, right: 340 }, want: { x: 0, y: -108 } },
    { name: "past the left and top", box: { top: -20, bottom: 180, left: -5, right: 235 }, want: { x: 13, y: 28 } },
  ])("$name", ({ box, want }) => expect(nudge(box, 1440, 900)).toEqual(want));
});
