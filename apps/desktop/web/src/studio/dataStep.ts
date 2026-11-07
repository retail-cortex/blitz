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

// A Data step's form helpers (spec_059_data): what Try it asks for and
// how it shows a result.
import { paramValue } from "./workflowForm";

/** Whether a parameter holds a template (Try it asks for its sample). */
export const templated = (v: string) => v.includes("{{");

/** A saved query's parameters, by name, each typed as written. */
export function sampleParams(entries: [string, string][]): Record<string, string | number | boolean> {
  return Object.fromEntries(entries.map(([k, v]) => [k, paramValue(v)]));
}

/** A table's rows (JSON) as cells' text: NULL empty, JSON as written. */
export function tableRows(rowsJson: string): string[][] {
  let rows: unknown;
  try {
    rows = JSON.parse(rowsJson || "[]");
  } catch {
    return [];
  }
  if (!Array.isArray(rows)) return [];
  return rows.map((r) => (Array.isArray(r) ? r.map(cellText) : []));
}

/** One value as a cell shows it. */
function cellText(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}
