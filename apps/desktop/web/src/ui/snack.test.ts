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
import { snackTop } from "./controls";

type Box = { top: number; bottom: number; left: number; right: number };

// A document whose headers are these boxes.
const doc = (...boxes: Box[]) =>
  ({
    querySelectorAll: () => boxes.map((b) => ({ getBoundingClientRect: () => ({ ...b, height: b.bottom - b.top }) })),
  }) as unknown as Pick<Document, "querySelectorAll">;

const topbar = { top: 0, bottom: 64, left: 0, right: 1300 };

describe("snackTop", () => {
  it("is undefined with no top bar, leaving it to the stylesheet", () => {
    expect(snackTop(doc(), 1300)).toBeUndefined();
    expect(snackTop(undefined, 1300)).toBeUndefined();
  });
  it("sits just below the top bar", () => {
    expect(snackTop(doc(topbar), 1300)).toBe(72);
  });
  it("follows the top bar down when a banner shows", () => {
    expect(snackTop(doc({ ...topbar, top: 40, bottom: 104 }), 1300)).toBe(112);
  });
  it("clears the editor's tabs and toolbar under it", () => {
    const tabs = { top: 66, bottom: 106, left: 290, right: 866 };
    const head = { top: 106, bottom: 147.6, left: 290, right: 866 };
    expect(snackTop(doc(topbar, tabs, head), 1300)).toBe(156);
  });
  it("ignores headers outside its span, and hidden ones", () => {
    const files = { top: 66, bottom: 200, left: 0, right: 280 };
    const hidden = { top: 0, bottom: 0, left: 300, right: 900 };
    expect(snackTop(doc(topbar, files, hidden), 1300)).toBe(72);
  });
});
