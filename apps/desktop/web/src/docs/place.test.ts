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
import { firstPage, lastPlace, savePlace } from "./place";

// A stand-in for the window's storage.
function memory(): Storage {
  const m = new Map<string, string>();
  return { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => void m.set(k, v), removeItem: (k) => void m.delete(k), clear: () => m.clear(), key: () => null, length: 0 } as Storage;
}

describe("where the guide was left", () => {
  it("is Tutorials the first time", () => {
    expect(lastPlace(memory())).toEqual({ page: firstPage });
    expect(firstPage).toBe("tutorials");
  });
  it("is the page and scroll last saved", () => {
    const s = memory();
    savePlace({ page: "guide/models", scroll: 412.6 }, s);
    expect(lastPlace(s)).toEqual({ page: "guide/models", scroll: 413 });
    savePlace({ page: "" }, s);
    expect(lastPlace(s)).toEqual({ page: "", scroll: undefined });
  });
  it("starts over on what it can't read, and without storage", () => {
    const s = memory();
    s.setItem("blitz.desktop.docs", "{not json");
    expect(lastPlace(s)).toEqual({ page: firstPage });
    s.setItem("blitz.desktop.docs", JSON.stringify({ page: 7 }));
    expect(lastPlace(s)).toEqual({ page: firstPage });
    expect(lastPlace(undefined)).toEqual({ page: firstPage });
    const refusing = { setItem: () => { throw new Error("quota"); } };
    expect(() => savePlace({ page: "guide" }, refusing)).not.toThrow();
    expect(lastPlace({ getItem: () => { throw new Error("denied"); } })).toEqual({ page: firstPage });
  });
});
