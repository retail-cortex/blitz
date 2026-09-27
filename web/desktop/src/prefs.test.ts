import { describe, expect, it } from "vitest";
import { baseName, closeWorkspace, defaultPrefs, displayName, editWorkspace, forgetWorkspace, moveWorkspace, openWorkspace, type Prefs } from "./prefs";
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
