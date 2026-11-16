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
import { isStudioKind, studioEntriesShown, studioEntryOf, studioGroups, studioKindShown, type StudioKind } from "./StudioDrawer";

describe("Studio's entries", () => {
  it("groups the workspace, its resources and what runs, and what it follows", () => {
    expect(studioGroups).toEqual([["overview", "settings", "specs"], ["resources", "workers", "workflows"], ["rules", "guardrails"]]);
    expect(studioEntriesShown(true)).toEqual(studioGroups.flat());
    expect(studioEntriesShown(false)).toEqual(["settings", "resources"]);
  });

  it.each([
    { asked: "resources" as StudioKind, advanced: true, want: "agents" },
    { asked: "resources" as StudioKind, advanced: false, want: "connectors" },
    { asked: "evals" as StudioKind, advanced: true, want: "evals" },
    { asked: "evals" as StudioKind, advanced: false, want: "settings" },
    { asked: "workers" as StudioKind, advanced: true, want: "workers" },
    { asked: "workers" as StudioKind, advanced: false, want: "settings" },
    { asked: "connectors" as StudioKind, advanced: false, want: "connectors" },
    { asked: "workflows" as StudioKind, advanced: false, want: "settings" },
    { asked: "settings" as StudioKind, advanced: false, want: "settings" },
    { asked: "overview" as StudioKind, advanced: true, want: "overview" },
  ])("opens $asked as $want (advanced: $advanced)", ({ asked, advanced, want }) => {
    expect(studioKindShown(asked, advanced)).toBe(want);
  });

  it("shows Resources' tabs under Resources, and knows every kind", () => {
    for (const k of ["agents", "skills", "evals", "connectors", "secrets"] as const) expect(studioEntryOf(k)).toBe("resources");
    expect(studioEntryOf("workers")).toBe("workers");
    expect(studioEntryOf("workflows")).toBe("workflows");
    expect(isStudioKind("settings")).toBe(true);
    expect(isStudioKind("browser")).toBe(false); // browser profiles are Settings' alone
    expect(isStudioKind("library")).toBe(false);
  });
});
