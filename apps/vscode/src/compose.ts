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

// What the editor sends the chat's composer.

import { isAbsolute, relative, sep } from "node:path";

/**
 * file's path in the workspace dir (the chat's), or undefined when it
 * isn't in it: a file of another folder isn't the chat's to name (VSC-02a).
 */
export function inWorkspace(dir: string, file: string): string | undefined {
  const p = relative(dir, file);
  return p === "" || p === ".." || p.startsWith(".." + sep) || isAbsolute(p) ? undefined : p;
}

/** A selection as the composer gets it: where it's from, and the code. */
export function selectionText(path: string, from: number, to: number, language: string, text: string): string {
  const where = from === to ? `line ${from}` : `lines ${from}–${to}`;
  const fence = text.includes("```") ? "````" : "```";
  return `\`${path}\` ${where}:\n${fence}${language}\n${text.replace(/\n$/, "")}\n${fence}`;
}
