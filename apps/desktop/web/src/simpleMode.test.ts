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
import { allSections, entryOf, menuIds, resourceTabs, sectionShown, settingsEntries, showsSignIn, shownEntries, shownTabs, studioResourceTabs, tabOf } from "./simpleMode";

describe("simple mode", () => {
  it("keeps what a newcomer needs in Settings, and everything with advanced", () => {
    expect(shownEntries(false)).toEqual(["providers", "personality", "appearance", "resources", "access", "system"]);
    expect(shownEntries(true)).toEqual(["providers", "personality", "appearance", "resources", "access", "system"]);
    expect(shownTabs("resources", false)).toEqual(["connectors"]);
    expect(shownTabs("resources", true)).toEqual(["agents", "skills", "evals", "workers", "connectors", "secrets", "browser"]);
    expect(shownTabs("access", false)).toEqual(["workspaces"]);
    expect(shownTabs("system", false)).toEqual(["about"]);
    expect(shownTabs("system", true)).toEqual(["service", "logs", "about"]);
    expect(shownTabs("providers", false)).toEqual(["providers", "organization"]);
  });

  it.each([
    { asked: "logs" as const, advanced: false, want: "providers" },
    { asked: "logs" as const, advanced: true, want: "logs" },
    { asked: "workspaces" as const, advanced: false, want: "workspaces" },
    { asked: "skills" as const, advanced: false, want: "providers" },
    { asked: "appearance" as const, advanced: false, want: "appearance" },
    { asked: "connectors" as const, advanced: false, want: "connectors" },
    { asked: "workers" as const, advanced: true, want: "workers" },
    { asked: "mcp" as const, advanced: true, want: "mcp" },
    { asked: "mcp" as const, advanced: false, want: "connectors" },
    { asked: "evals" as const, advanced: true, want: "evals" },
    { asked: "evals" as const, advanced: false, want: "providers" },
    { asked: "voice" as const, advanced: false, want: "voice" },
  ])("opens $asked as $want (advanced: $advanced)", ({ asked, advanced, want }) => {
    expect(sectionShown(asked, advanced)).toBe(want);
  });

  it("keeps Workspaces, Changes and Help in the bar, and Studio with advanced settings", () => {
    expect(menuIds(false)).toEqual(["workspaces", "changes", "help"]);
    expect(menuIds(true)).toEqual(["workspaces", "changes", "studio", "help"]);
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
  it("shows every section once, as a tab of one entry (MCP servers inside Connectors')", () => {
    const tabs = settingsEntries.flatMap((e) => [...e.tabs]);
    expect(new Set(tabs).size).toBe(tabs.length);
    expect([...tabs, "mcp"].sort()).toEqual([...allSections].sort());
    expect(tabOf("mcp")).toBe("connectors");
    expect(entryOf("mcp")).toBe("resources");
    expect(resourceTabs.map(entryOf)).toEqual(resourceTabs.map(() => "resources"));
    expect(entryOf("organization")).toBe("providers");
    expect(entryOf("voice")).toBe("personality");
    expect(entryOf("permissions")).toBe("access");
    expect(entryOf("logs")).toBe("system");
  });

  it("gives Studio's Resources Settings' tabs: all of them with advanced, the connectors without", () => {
    expect(studioResourceTabs(true)).toEqual([...resourceTabs]);
    expect(studioResourceTabs(false)).toEqual(["connectors"]);
  });
});
