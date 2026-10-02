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
import { installQuietInputs, quiet, shouldQuiet } from "./inputs";

function field(tagName: string, type: string | undefined, attrs: Record<string, string> = {}) {
  const a = { ...attrs };
  return {
    tagName,
    type,
    attrs: a,
    hasAttribute: (n: string) => n in a,
    setAttribute: (n: string, v: string) => {
      a[n] = v;
    },
  };
}

describe("quiet inputs", () => {
  it.each<{ name: string; tag: string; type: string; attrs: Record<string, string>; want: boolean }>([
    { name: "a text field", tag: "INPUT", type: "text", attrs: {}, want: true },
    { name: "a field without a type", tag: "INPUT", type: "", attrs: {}, want: true },
    { name: "a search field", tag: "INPUT", type: "search", attrs: {}, want: true },
    { name: "a number field", tag: "INPUT", type: "number", attrs: {}, want: false },
    { name: "a checkbox", tag: "INPUT", type: "checkbox", attrs: {}, want: false },
    { name: "a prose field", tag: "INPUT", type: "text", attrs: { "data-prose": "" }, want: false },
    { name: "a text area (the composer)", tag: "TEXTAREA", type: "textarea", attrs: {}, want: false },
  ])("$name: $want", ({ tag, type, attrs, want }) => {
    expect(shouldQuiet(field(tag, type, attrs))).toBe(want);
  });
  it("turns capitals and corrections off, unless the field sets them", () => {
    const f = field("INPUT", "text", { autocorrect: "on" });
    quiet(f);
    expect(f.attrs).toEqual({ autocorrect: "on", autocapitalize: "off" });
    const t = field("TEXTAREA", "textarea");
    quiet(t);
    expect(t.attrs).toEqual({});
  });
  it("quiets a field as it gets focus", () => {
    let handler: ((e: { target: unknown }) => void) | undefined;
    installQuietInputs({ addEventListener: (_: string, h: unknown) => (handler = h as typeof handler) } as never);
    const f = field("INPUT", "text");
    handler?.({ target: f });
    expect(f.attrs).toEqual({ autocapitalize: "off", autocorrect: "off" });
    handler?.({ target: null });
  });
});
