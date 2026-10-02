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
import { allSections, menuIds, sectionShown, settingsSections, showsSignIn } from "./simpleMode";

describe("simple mode", () => {
  it("keeps what a newcomer needs in Settings, and everything with advanced", () => {
    expect(settingsSections(false)).toEqual(["appearance", "providers", "workspaces", "about"]);
    expect(settingsSections(true)).toEqual([...allSections]);
  });

  it.each([
    { asked: "logs" as const, advanced: false, want: "appearance" },
    { asked: "logs" as const, advanced: true, want: "logs" },
    { asked: "workspaces" as const, advanced: false, want: "workspaces" },
    { asked: "file" as const, advanced: false, want: "appearance" },
  ])("opens $asked as $want (advanced: $advanced)", ({ asked, advanced, want }) => {
    expect(sectionShown(asked, advanced)).toBe(want);
  });

  it("keeps Workspaces, Changes and Help in the bar", () => {
    expect(menuIds(false)).toEqual(["workspaces", "changes", "help"]);
    expect(menuIds(true)).toEqual(["workspaces", "changes", "agents", "workers", "help"]);
  });

  it.each([
    { advanced: false, method: "api_key", want: false },
    { advanced: false, method: "", want: false },
    { advanced: false, method: "adc", want: true },
    { advanced: false, method: "oauth", want: true },
    { advanced: true, method: "api_key", want: true },
  ])("shows sign-in for $method: $want (advanced: $advanced)", ({ advanced, method, want }) => {
    expect(showsSignIn(advanced, method)).toBe(want);
  });
});
