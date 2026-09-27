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

// Paths in the conversation that open the editor (spec_files_029 FIL-35):
// inline code naming a file in the workspace, with an optional :line or
// :line:column.

export interface FileRef {
  path: string;
  line?: number;
  column?: number;
}

/**
 * The file text refers to, relative to the workspace dir, if it looks like
 * a path: no spaces or scheme, and either a folder or an extension.
 * Absolute paths count when they're in the workspace.
 */
export function parseFileRef(text: string, dir: string): FileRef | null {
  const m = /^(.+?)(?::(\d+)(?::(\d+))?)?$/.exec(text.trim());
  if (!m) return null;
  let path = m[1];
  if (/\s|:\/\/|^[~$]/.test(path)) return null;
  const root = dir.replace(/\/+$/, "") + "/";
  if (path.startsWith("/")) {
    if (!path.startsWith(root)) return null;
    path = path.slice(root.length);
  }
  path = path.replace(/^(\.\/)+/, "");
  if (!path || path.startsWith("../") || !/[/.]/.test(path) || path.endsWith("/")) return null;
  const ref: FileRef = { path };
  if (m[2]) ref.line = Number(m[2]);
  if (m[3]) ref.column = Number(m[3]);
  return ref;
}

/** Splits "path:line[:col]" typed in Go to file. */
export function splitLine(query: string): { query: string; line?: number; column?: number } {
  const m = /^(.*?):(\d+)(?::(\d+))?$/.exec(query.trim());
  if (!m) return { query: query.trim() };
  return { query: m[1], line: Number(m[2]), column: m[3] ? Number(m[3]) : undefined };
}
