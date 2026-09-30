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
import { normalizePrefs, baseName, closeWorkspace, defaultPrefs, displayName, editWorkspace, forgetWorkspace, moveWorkspace, openWorkspace, type Prefs } from "./prefs";
import { resolveTheme } from "./theme";
import { workspaceColor } from "./palette";

const dirs = (p: Prefs) => p.workspaces.map((w) => `${w.dir}${w.open ? "" : "(closed)"}`).join(" ");

describe("workspaces", () => {
  it("opens, closes and reopens, keeping what the user set", () => {
    let p = openWorkspace(defaultPrefs, "/a");
    p = openWorkspace(p, "/b");
    p = openWorkspace(p, "/c");
    expect(dirs(p)).toBe("/a /b /c");
    expect(p.active).toBe("/c");
    p = editWorkspace(p, "/b", { name: "  Shop  ", color: "teal" });
    p = { ...p, active: "/b" };
    p = closeWorkspace(p, "/b");
    expect(dirs(p)).toBe("/a /c /b(closed)");
    expect(p.active).toBe("/c"); // the tab after it
    p = closeWorkspace(p, "/c");
    expect(p.active).toBe("/a"); // none after: the one before
    expect(dirs(p)).toBe("/a /c(closed) /b(closed)");
    p = openWorkspace(p, "/b");
    expect(dirs(p)).toBe("/a /b /c(closed)");
    expect(p.workspaces[1]).toMatchObject({ name: "Shop", color: "teal", open: true });
    p = closeWorkspace(closeWorkspace(p, "/a"), "/b");
    expect(p.active).toBeUndefined();
  });

  it("opening an open workspace just shows it", () => {
    const p = openWorkspace(openWorkspace(openWorkspace(defaultPrefs, "/a"), "/b"), "/a");
    expect(dirs(p)).toBe("/a /b");
    expect(p.active).toBe("/a");
  });

  it("forgets only closed workspaces, and reorders open ones", () => {
    let p = openWorkspace(openWorkspace(openWorkspace(defaultPrefs, "/a"), "/b"), "/c");
    p = closeWorkspace(p, "/c");
    expect(dirs(forgetWorkspace(p, "/c"))).toBe("/a /b");
    expect(dirs(forgetWorkspace(p, "/a"))).toBe(dirs(p));
    expect(dirs(moveWorkspace(p, "/b", 0))).toBe("/b /a /c(closed)");
    expect(dirs(moveWorkspace(p, "/a", 9))).toBe("/b /a /c(closed)");
  });

  it("names a workspace after its directory unless named", () => {
    expect(baseName("/Users/me/code/shop/")).toBe("shop");
    expect(baseName("C:\\code\\shop")).toBe("shop");
    expect(displayName({ dir: "/x/shop", name: " " })).toBe("shop");
    expect(displayName({ dir: "/x/shop", name: "Storefront" })).toBe("Storefront");
  });
});

describe("theme", () => {
  it("follows the system unless chosen", () => {
    expect(resolveTheme("system", true)).toBe("dark");
    expect(resolveTheme("system", false)).toBe("light");
    expect(resolveTheme("light", true)).toBe("light");
    expect(resolveTheme("dark", false)).toBe("dark");
  });
  it("gives each workspace colour a shade per theme", () => {
    expect(workspaceColor("teal", "light")).not.toBe(workspaceColor("teal", "dark"));
    expect(workspaceColor("nonsense", "light")).toBe(workspaceColor("blue", "light"));
  });
});

describe("normalizePrefs", () => {
  it("makes whatever was loaded usable", () => {
    // What Go sends with no workspaces yet, and a damaged or older shape.
    expect(normalizePrefs({ theme: "dark", workspaces: null }).workspaces).toEqual([]);
    expect(normalizePrefs(null)).toEqual({ ...defaultPrefs, active: undefined });
    expect(normalizePrefs({ notifications: "sometimes" }).notifications).toBe("on");
    const p = normalizePrefs({
      theme: "sepia",
      density: 3,
      run_settings: "yes",
      workspaces: [{ dir: "/a", open: true, name: 7 }, { dir: "" }, "nonsense", { dir: "/b", color: "teal" }],
      active: "/b",
    });
    expect(p.theme).toBe("system");
    expect(p.density).toBe("comfortable");
    expect(p.run_settings).toBe(false);
    expect(p.workspaces).toEqual([
      { dir: "/a", open: true, name: undefined, description: undefined, color: undefined },
      { dir: "/b", open: false, name: undefined, description: undefined, color: "teal" },
    ]);
    expect(p.active).toBe("/a"); // /b is closed
  });
  it("keeps when a workspace's workers were seen, when it's a time", () => {
    const ws = (seen: unknown) => normalizePrefs({ workspaces: [{ dir: "/a", open: true, workers_seen: seen }] }).workspaces[0].workers_seen;
    expect(ws(1700000000000)).toBe(1700000000000);
    expect(ws("yesterday")).toBeUndefined();
    expect(ws(-1)).toBeUndefined();
  });
  it("keeps the files settings when they're right", () => {
    expect(normalizePrefs({ files: true, show_hidden: true, chat_width: 612.4 })).toMatchObject({ files: true, show_hidden: true, chat_width: 612 });
    expect(normalizePrefs({ files: "yes", show_hidden: 1, chat_width: -5 })).toMatchObject({ files: false, show_hidden: false, chat_width: 0 });
    expect(normalizePrefs({}).width).toBe("full");
    expect(normalizePrefs({ width: "readable" }).width).toBe("readable");
    expect(normalizePrefs({ width: "huge" }).width).toBe("full");
  });
});
