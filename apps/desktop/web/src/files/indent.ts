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

// The indentation a file uses, for the editor: tabs, or the most common
// step between the space indents of consecutive lines (2, 4 or 8).

export function detectIndent(text: string, fallback = "  "): string {
  let tabs = 0;
  let spaced = 0;
  const steps = new Map<number, number>();
  let prev = 0;
  for (const line of text.split("\n", 2000)) {
    if (!line.trim()) continue;
    if (line.startsWith("\t")) {
      tabs++;
      continue;
    }
    const n = line.length - line.trimStart().length;
    if (n > 0) spaced++;
    const step = Math.abs(n - prev);
    if (step === 2 || step === 4 || step === 8) steps.set(step, (steps.get(step) ?? 0) + 1);
    prev = n;
  }
  if (tabs > spaced) return "\t";
  if (spaced === 0) return tabs > 0 ? "\t" : fallback;
  let best = 0;
  let count = 0;
  for (const [step, c] of steps) if (c > count || (c === count && step < best)) [best, count] = [step, c];
  return best ? " ".repeat(best) : fallback;
}
