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
import { clampZoom, stepZoom, zoomKey } from "./zoom";

describe("the editor's zoom", () => {
  it.each([
    [100, 1, 110],
    [100, -1, 90],
    [104, 1, 110],
    [104, -1, 100],
    [200, 1, 200],
    [50, -1, 50],
    [67, 1, 75],
  ] as const)("steps from %d by %d to %d", (z, dir, want) => {
    expect(stepZoom(z, dir)).toBe(want);
  });

  it("stays within 50–200, in whole percent", () => {
    expect(clampZoom(20)).toBe(50);
    expect(clampZoom(400)).toBe(200);
    expect(clampZoom(123.4)).toBe(123);
  });

  const key = (k: string, mods: Partial<{ metaKey: boolean; ctrlKey: boolean; altKey: boolean }> = {}) => ({ key: k, metaKey: false, ctrlKey: false, altKey: false, ...mods });
  it.each([
    [key("=", { metaKey: true }), true, "in"],
    [key("+", { metaKey: true }), true, "in"],
    [key("-", { metaKey: true }), true, "out"],
    [key("0", { metaKey: true }), true, "reset"],
    [key("=", { ctrlKey: true }), true, null], // Ctrl on a Mac isn't zoom
    [key("=", { ctrlKey: true }), false, "in"],
    [key("-", { ctrlKey: true }), false, "out"],
    [key("=", { metaKey: true }), false, null],
    [key("=", { metaKey: true, altKey: true }), true, null],
    [key("a", { metaKey: true }), true, null],
    [key("="), true, null],
  ])("%j on a Mac %s is %s", (e, mac, want) => {
    expect(zoomKey(e, mac)).toBe(want);
  });
});
