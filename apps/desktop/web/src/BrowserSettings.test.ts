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
import { domainsText, parseDomains, validProfileName } from "./BrowserSettings";

describe("browser profile names", () => {
  it.each([
    ["work", true],
    ["client-a_2", true],
    ["Work", false],
    ["-x", false],
    ["a b", false],
    ["x".repeat(33), false],
  ])("%s", (name, want) => expect(validProfileName(name)).toBe(want));
});

describe("a profile's sites", () => {
  it("reads one a line or commas, tidied, each once", () => {
    expect(parseDomains(" Mail.Example.com\n*.example.com, docs.example.com\n\nmail.example.com ")).toEqual(["mail.example.com", "*.example.com", "docs.example.com"]);
    expect(parseDomains("")).toEqual([]);
  });
  it("shows them one a line", () => expect(domainsText(["a.example", "b.example"])).toBe("a.example\nb.example"));
});
