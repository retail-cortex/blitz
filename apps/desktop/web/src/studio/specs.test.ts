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
import { newSpecFile, newSpecText, specFiles } from "./specs";

describe("the specs", () => {
  it("lists the index first, then the specs in order, and only Markdown", () => {
    const specs = specFiles(["010-data_model.md", "notes.txt", "002-users.md", "README.md", "001-overview.md", "ideas.md"]);
    expect(specs.map((s) => s.file)).toEqual(["README.md", "001-overview.md", "002-users.md", "010-data_model.md", "ideas.md"]);
    expect(specs[3]).toEqual({ file: "010-data_model.md", path: ".agents/specs/010-data_model.md", number: 10, title: "Data model" });
    expect(specs[4]).toMatchObject({ number: 0, title: "Ideas" });
  });

  it("names a new spec after the last one, from its title", () => {
    const specs = specFiles(["001-overview.md", "007-api.md", "README.md"]);
    expect(newSpecFile("Café orders & Payments!", specs)).toBe("008-cafe-orders-payments.md");
    expect(newSpecFile("Overview", [])).toBe("001-overview.md");
    expect(newSpecFile(" ?! ", specs)).toBe("");
    expect(newSpecFile("last", specFiles(["999-end.md"]))).toBe("999-last.md");
    expect(newSpecText(" Payments ")).toBe("# Payments\n\n");
  });
});
