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
import { canCommit, statusKey, subject } from "./commit";

describe("statusKey", () => {
  it.each([
    ["A", "desktop.commit.status.added"],
    ["M", "desktop.commit.status.modified"],
    ["D", "desktop.commit.status.deleted"],
    ["R", "desktop.commit.status.renamed"],
    ["T", "desktop.commit.status.type"],
    ["U", "desktop.commit.status.modified"],
  ])("%s", (letter, key) => {
    expect(statusKey(letter)).toBe(key);
  });
});

describe("subject", () => {
  it.each([
    ["Fix it\n\nBecause.", "Fix it"],
    ["  \nFix it  ", "Fix it"],
    ["", ""],
  ])("%j", (message, want) => {
    expect(subject(message)).toBe(want);
  });
});

describe("canCommit", () => {
  it.each([
    [{ files: 1, message: "Fix it", busy: false }, true],
    [{ files: 0, message: "Fix it", busy: false }, false],
    [{ files: 1, message: "  ", busy: false }, false],
    [{ files: 1, message: "Fix it", busy: true }, false],
  ])("%j", (s, want) => {
    expect(canCommit(s)).toBe(want);
  });
});
