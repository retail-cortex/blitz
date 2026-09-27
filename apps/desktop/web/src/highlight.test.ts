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
import { isValidElement } from "react";
import { highlight, languageFor } from "./highlight";

describe("highlight", () => {
  it("finds a grammar from a fence or a file name", () => {
    expect(languageFor("go")).toBe("go");
    expect(languageFor("sh")).toBe("bash");
    expect(languageFor("internal/cart/discount.go")).toBe("go");
    expect(languageFor("web/App.tsx")).toBe("typescript");
    expect(languageFor("Makefile")).toBe("makefile");
    expect(languageFor("notes.unknownext")).toBeUndefined();
    expect(languageFor("")).toBeUndefined();
  });
  it("returns elements for known languages and text otherwise", () => {
    expect(isValidElement(highlight("func main() {}", "go"))).toBe(true);
    expect(highlight("plain", undefined)).toBe("plain");
    expect(highlight("<script>alert(1)</script>", "nosuchlang")).toBe("<script>alert(1)</script>");
  });
});
