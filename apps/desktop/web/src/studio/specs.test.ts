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
import { applyHunks, lineHunks, newSpecFile, newSpecText } from "./specs";

describe("the specs", () => {
  it("names a new spec after the last one, from its title", () => {
    expect(newSpecFile("Café orders & Payments!", [0, 1, 7])).toBe("008-cafe-orders-payments.md");
    expect(newSpecFile("Overview", [])).toBe("001-overview.md");
    expect(newSpecFile(" ?! ", [1])).toBe("");
    expect(newSpecFile("last", [999])).toBe("999-last.md");
  });

  it("starts a new spec in /spec's format", () => {
    const text = newSpecText(" Payments ", "008-payments.md");
    expect(text.startsWith("# 008. Payments\n\nStatus: draft\n")).toBe(true);
    for (const h of ["## Requirements", "## Done when", "## Open questions"]) expect(text).toContain(h);
    expect(newSpecText("Notes", "notes.md").startsWith("# Notes\n")).toBe(true);
  });
});

// Refine's changes, kept or dropped one by one (spec_057 SDD-04).
describe("a refined spec's changes", () => {
  const before = "# 002. Orders\nStatus: draft\n- **ORD-01** Checkout is fast.\n- **ORD-02** Card and cash.\n## Done when\nPaid.";
  const after = "# 002. Orders\nStatus: draft\n- **ORD-01** Checkout takes under 30 s at the till.\n- **ORD-02** Card and cash.\n- **ORD-03** Refunds go back to the card.\n## Done when\nPaid.";

  it("finds each change, with where it is", () => {
    const hunks = lineHunks(before, after);
    expect(hunks).toEqual([
      { at: 2, removed: ["- **ORD-01** Checkout is fast."], added: ["- **ORD-01** Checkout takes under 30 s at the till."] },
      { at: 4, removed: [], added: ["- **ORD-03** Refunds go back to the card."] },
    ]);
    expect(lineHunks(before, before)).toEqual([]);
    expect(lineHunks("a", "")).toEqual([{ at: 0, removed: ["a"], added: [""] }]);
  });

  it("applies the changes kept, and only those", () => {
    const hunks = lineHunks(before, after);
    expect(applyHunks(before, hunks, [true, true])).toBe(after);
    expect(applyHunks(before, hunks, [false, false])).toBe(before);
    const one = applyHunks(before, hunks, [false, true]);
    expect(one).toContain("Checkout is fast.");
    expect(one).toContain("ORD-03");
    // Lines removed at the end, and added at the start.
    const h = lineHunks("x\ny\nz", "w\nx\ny");
    expect(applyHunks("x\ny\nz", h, h.map(() => true))).toBe("w\nx\ny");
  });
});
