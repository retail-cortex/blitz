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
import { anonymousProfile, browserSummary } from "./WorkspaceSettingsForm";

describe("what a workspace browses with (BP-03e)", () => {
  it.each([
    { name: "a profile chosen", b: { names: ["work", "home"], current: "home" }, want: "home" },
    { name: "Anonymous", b: { names: ["work"], current: anonymousProfile }, want: "Anonymous (signed out)" },
    { name: "none chosen, one profile: the default", b: { names: ["work"], current: "" }, want: "Default: work, your only profile" },
    { name: "none chosen, two profiles", b: { names: ["work", "home"], current: "" }, want: "None chosen: the agents ask to use the profile for a site" },
  ])("$name", ({ b, want }) => expect(browserSummary(b)).toBe(want));
});
