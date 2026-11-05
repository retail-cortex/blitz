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
import { followDrag } from "./ResizeHandle";

// A drag, with the window and the handle as plain event targets.
function drag() {
  const win = new EventTarget();
  const handle = new EventTarget();
  const classes = new Set<string>();
  const shown: number[] = [];
  const ended: number[] = [];
  followDrag({
    win,
    handle,
    body: { classList: { add: (c) => classes.add(c), remove: (c) => classes.delete(c) } },
    start: 300,
    widthAt: (x) => 1000 - x,
    show: (w) => shown.push(w),
    end: (w) => ended.push(w),
  });
  const move = (x: number) => win.dispatchEvent(Object.assign(new Event("pointermove"), { clientX: x }));
  return { win, handle, classes, shown, ended, move };
}

describe("followDrag", () => {
  it.each([
    { name: "let go", end: (d: ReturnType<typeof drag>) => d.win.dispatchEvent(new Event("pointerup")) },
    { name: "cancelled by the system", end: (d: ReturnType<typeof drag>) => d.win.dispatchEvent(new Event("pointercancel")) },
    { name: "the pointer lost", end: (d: ReturnType<typeof drag>) => d.handle.dispatchEvent(new Event("lostpointercapture")) },
  ])("ends once when $name, and stops listening", ({ end }) => {
    const d = drag();
    expect(d.classes.has("resizing")).toBe(true);
    d.move(600);
    expect(d.shown).toEqual([400]);
    end(d);
    d.win.dispatchEvent(new Event("pointerup"));
    d.handle.dispatchEvent(new Event("lostpointercapture"));
    d.move(500);
    expect(d.ended).toEqual([400]);
    expect(d.shown).toEqual([400]);
    expect(d.classes.has("resizing")).toBe(false);
  });

  it("ends with the width it started with when nothing moved", () => {
    const d = drag();
    d.win.dispatchEvent(new Event("pointerup"));
    expect(d.ended).toEqual([300]);
  });
});
