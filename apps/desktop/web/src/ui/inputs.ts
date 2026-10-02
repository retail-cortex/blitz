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

// Single-line fields hold names, paths, models and commands, which the
// system's automatic capitals and corrections break ("gemini-3.8-flash"
// typed on macOS became "Gemini-3.8-flash"). They're turned off for every
// such field as it gets focus; text areas (the composer, prompts) and
// fields marked data-prose keep them.

/** The input types that take free text. */
const textTypes = new Set(["", "text", "search", "url", "email"]);

/** What a field looks like to quiet: its tag, type and attributes. */
export interface FieldLike {
  tagName: string;
  type?: string;
  hasAttribute(name: string): boolean;
}

/** Whether el should have the system's capitals and corrections turned off. */
export function shouldQuiet(el: FieldLike): boolean {
  if (el.tagName !== "INPUT" || !textTypes.has((el.type ?? "").toLowerCase())) return false;
  return !el.hasAttribute("data-prose");
}

/** Turns capitals and corrections off on el, unless it sets them itself. */
export function quiet(el: FieldLike & { setAttribute(name: string, value: string): void }) {
  if (!shouldQuiet(el)) return;
  for (const name of ["autocapitalize", "autocorrect"]) {
    if (!el.hasAttribute(name)) el.setAttribute(name, "off");
  }
}

/** Quiets each single-line field as it gets focus, from now on. */
export function installQuietInputs(doc: Pick<Document, "addEventListener"> = document) {
  doc.addEventListener("focusin", (e) => {
    const el = e.target;
    if (el && typeof (el as Element).setAttribute === "function") quiet(el as HTMLInputElement);
  });
}
