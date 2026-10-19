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

// The arithmetic of ui/charts.tsx, kept apart to be tested.

/** A score ring's dash and gap for a score out of 5 on a circle of radius r. */
export function ringDash(score: number, r: number): { dash: number; gap: number } {
  const c = 2 * Math.PI * r;
  const dash = (Math.max(0, Math.min(5, score)) / 5) * c;
  return { dash, gap: c - dash };
}

/**
 * Where columns of the totals go and the line through the values, in a
 * width × height box: each column's step, the scale that fits the tallest
 * total, and the line's points ("" without values), on its own scale.
 */
export function chartGeometry(totals: number[], values: number[], width: number, height: number): { step: number; columnScale: number; line: string } {
  const n = Math.max(totals.length, values.length, 1);
  const step = width / n;
  const top = Math.max(...totals, 0);
  const columnScale = top > 0 ? (height * 0.95) / top : 0;
  const hi = Math.max(...values, 0);
  const line =
    values.length > 1 && hi > 0
      ? values.map((v, i) => `${(i * step + step / 2).toFixed(1)},${(height - (v / hi) * height * 0.95).toFixed(1)}`).join(" ")
      : values.length > 1
        ? values.map((_, i) => `${(i * step + step / 2).toFixed(1)},${height.toFixed(1)}`).join(" ")
        : "";
  return { step, columnScale, line };
}
