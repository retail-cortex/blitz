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
import { fit, wheelFactor, zoomAt } from "./panzoom";

describe("pan and zoom", () => {
  it("keeps the point under the pointer where it is", () => {
    const v = zoomAt(fit, 2, 100, 50);
    expect(v).toEqual({ scale: 2, x: -100, y: -50 });
    // The diagram's point (100, 50) is still at 100, 50: x + scale * 100.
    expect(v.x + v.scale * 100).toBe(100);
    const back = zoomAt(v, 0.5, 100, 50);
    expect(back.scale).toBe(1);
    expect(back.x).toBeCloseTo(0);
    expect(back.y).toBeCloseTo(0);
  });

  it("stays between a quarter and eight times", () => {
    expect(zoomAt(fit, 100, 0, 0).scale).toBe(8);
    expect(zoomAt(fit, 0.001, 0, 0).scale).toBe(0.25);
  });

  it("zooms in for a wheel up, out for down, in lines and pages too", () => {
    expect(wheelFactor(-100, 0)).toBeGreaterThan(1);
    expect(wheelFactor(100, 0)).toBeLessThan(1);
    expect(wheelFactor(-3, 1)).toBeCloseTo(wheelFactor(-48, 0));
    expect(wheelFactor(1, 2)).toBeCloseTo(wheelFactor(100, 0)); // capped
    expect(wheelFactor(0, 0)).toBe(1);
  });
});
