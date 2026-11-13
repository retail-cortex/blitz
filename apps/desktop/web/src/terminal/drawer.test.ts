/*
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

import { createRouterTransport } from "@connectrpc/connect";
import { beforeEach, describe, expect, it } from "vitest";
import { setTransport } from "../api";
import { TerminalService } from "../gen/blitz/v1/terminal_pb";
import {
  added,
  closeTerminal,
  drawerHeight,
  hidden,
  hideTerminals,
  isToggleTerminalKey,
  newTerminal,
  removed,
  replaced,
  restartTerminal,
  resetTermDrawers,
  restored,
  restoreTerminals,
  showTerminals,
  shown,
  termChoices,
  termDetail,
  termDrawer,
  terminalExited,
  toggleTerminals,
  type TermDrawer,
  type TermEntry,
} from "./drawer";

const term = (id: string, dir = "", more: Partial<TermEntry> = {}): TermEntry => ({ id, dir, shell: "/bin/zsh", ...more });
const state = (ids: string[], active = "", open = false): TermDrawer => ({ list: ids.map((id) => term(id)), active, open });
const ids = (s: TermDrawer) => s.list.map((x) => x.id);

describe("the terminal drawer's state", () => {
  it("adds a terminal shown with the drawer down, or behind the one shown", () => {
    expect(added(state([]), term("a"))).toMatchObject({ active: "a", open: true });
    const back = added(state(["a"], "a"), term("b"), false);
    expect([ids(back), back.active, back.open]).toEqual([["a", "b"], "a", false]);
    const again = state(["a"], "a", true);
    expect(added(again, term("a")).list).toBe(again.list);
  });

  it.each([
    { name: "the shown one: its right-hand neighbour", s: state(["a", "b", "c"], "b", true), id: "b", active: "c", open: true },
    { name: "the last: its left-hand one", s: state(["a", "b"], "b", true), id: "b", active: "a", open: true },
    { name: "another: the shown one stays", s: state(["a", "b"], "a", true), id: "b", active: "a", open: true },
    { name: "the only one: the drawer goes up", s: state(["a"], "a", true), id: "a", active: "", open: false },
  ])("removes $name", ({ s, id, active, open }) => {
    expect(removed(s, id)).toMatchObject({ active, open });
    expect(ids(removed(s, id))).not.toContain(id);
  });

  it("puts a restarted terminal in its tab's place, shown if it was", () => {
    const s = replaced(state(["a", "b", "c"], "b", true), "b", term("n"));
    expect([ids(s), s.active, s.open]).toEqual([["a", "n", "c"], "n", true]);
    expect(replaced(state(["a", "b"], "a"), "b", term("n")).active, "behind the shown one").toBe("a");
    expect(ids(replaced(state(["a"], "a"), "x", term("n"))), "one it doesn't have: added").toEqual(["a", "n"]);
  });

  it("leaves the state alone removing one it doesn't have", () => {
    const s = state(["a"], "a");
    expect(removed(s, "x")).toBe(s);
  });

  it("drops down on the one asked for, the one shown last, or the first; and goes up", () => {
    expect(shown(state(["a", "b"], "a"), "b")).toMatchObject({ active: "b", open: true });
    expect(shown(state(["a", "b"], "b"), "gone")).toMatchObject({ active: "b", open: true });
    expect(shown(state(["a", "b"]))).toMatchObject({ active: "a", open: true });
    expect(shown(state([]))).toMatchObject({ active: "", open: false });
    const down = state(["a"], "a", true);
    expect(hidden(down)).toEqual({ ...down, open: false });
    const up = state(["a"], "a");
    expect(hidden(up)).toBe(up);
  });

  it("restores a reloaded window's terminals with the drawer up, skipping those it has", () => {
    const s = restored(state(["a"]), [term("a"), term("b", "src")]);
    expect([ids(s), s.active, s.open]).toEqual([["a", "b"], "a", false]);
    expect(restored(state([]), [term("b")]).active).toBe("b");
  });

  it.each([
    { h: 0, room: 800, want: 0 },
    { h: 100, room: 800, want: 0 },
    { h: 420, room: 800, want: 420 },
    { h: 900, room: 600, want: 600 },
    { h: 300, room: 50, want: 160 },
  ])("keeps a height of $h in $room of room as $want", ({ h, room, want }) => {
    expect(drawerHeight(h, room)).toBe(want);
  });
});

describe("the status bar's list of terminals (TRM-30a)", () => {
  it("names each by its folder, with its folder in the workspace and its shell, the shown one marked", () => {
    const s: TermDrawer = { list: [term("a"), term("b", "src/api"), term("c", "web", { shell: undefined })], active: "b", open: true };
    expect(termChoices(s, "/Users/me/shop")).toEqual([
      { id: "a", label: "shop", detail: ". · zsh", current: false },
      { id: "b", label: "api", detail: "src/api · zsh", current: true },
      { id: "c", label: "web", detail: "web", current: false },
    ]);
    expect(termChoices({ ...s, open: false }, "/w").some((c) => c.current), "none is shown with the drawer up").toBe(false);
  });

  it("writes the workspace's own folder as .", () => {
    expect(termDetail(term("a", "", { shell: "" }))).toBe(".");
  });
});

describe("⌃` (TRM-30b)", () => {
  const key = (k: Partial<KeyboardEvent>) => ({ key: "`", code: "Backquote", ctrlKey: true, metaKey: false, altKey: false, ...k });
  it.each([
    { name: "⌃`", e: key({}), want: true },
    { name: "a layout's dead key, by its place", e: key({ key: "Dead" }), want: true },
    { name: "a backtick alone", e: key({ ctrlKey: false }), want: false },
    { name: "⌘`, macOS's next window", e: key({ ctrlKey: false, metaKey: true }), want: false },
    { name: "⌃⌥`", e: key({ altKey: true }), want: false },
    { name: "⌃1", e: key({ key: "1", code: "Digit1" }), want: false },
  ])("$name: $want", ({ e, want }) => {
    expect(isToggleTerminalKey(e)).toBe(want);
  });
});

describe("a workspace's terminals in the drawer", () => {
  const ws = "/w/shop";
  let calls: string[];
  let running: { id: string; dir: string; shell: string; exited: boolean; exitCode: number }[];
  let n: number;

  beforeEach(() => {
    resetTermDrawers();
    calls = [];
    running = [];
    n = 0;
    setTransport(
      createRouterTransport(({ service }) => {
        service(TerminalService, {
          listTerminals: (r) => (calls.push(`list ${r.workspace}`), { terminals: running }),
          openTerminal: (r) => (calls.push(`open ${r.dir}`), { terminal: { id: `t${++n}`, dir: r.dir, shell: "/bin/zsh" } }),
          closeTerminal: (r) => {
            calls.push(`close ${r.id}`);
            if (r.id === "gone") throw new Error("unknown terminal");
            return {};
          },
        });
      }),
    );
  });

  it("opens one with the drawer down, and keeps them while it's up", async () => {
    await newTerminal(ws, "src");
    await newTerminal(ws, "");
    expect(termDrawer(ws)).toMatchObject({ active: "t2", open: true });
    hideTerminals(ws);
    expect([ids(termDrawer(ws)), termDrawer(ws).open]).toEqual([["t1", "t2"], false]);
    showTerminals(ws, "t1");
    expect(termDrawer(ws)).toMatchObject({ active: "t1", open: true });
    expect(calls).toEqual(["open src", "open "]);
    expect(termDrawer("/w/other").list, "each workspace its own").toEqual([]);
  });

  it("toggles: a new one when there's none, then up, then down on the same one", async () => {
    await toggleTerminals(ws);
    expect(termDrawer(ws)).toMatchObject({ active: "t1", open: true });
    await toggleTerminals(ws);
    expect(termDrawer(ws).open).toBe(false);
    await toggleTerminals(ws);
    expect(termDrawer(ws)).toMatchObject({ active: "t1", open: true });
    expect(calls).toEqual(["open "]);
  });

  it("closes a tab then its shell, even one already gone", async () => {
    await newTerminal(ws, "");
    await closeTerminal(ws, "t1");
    expect(termDrawer(ws)).toMatchObject({ list: [], open: false });
    await newTerminal(ws, "");
    await expect(closeTerminal(ws, "gone")).resolves.toBeUndefined();
    expect(calls).toEqual(["open ", "close t1", "open ", "close gone"]);
  });

  // TRM-27: Restart is a new shell in the same folder and tab, then the old one ended.
  it("restarts a terminal in its folder and tab", async () => {
    await newTerminal(ws, "src");
    await newTerminal(ws, "");
    showTerminals(ws, "t1");
    const t = await restartTerminal(ws, "t1");
    expect(t).toMatchObject({ id: "t3", dir: "src" });
    expect(termDrawer(ws)).toMatchObject({ active: "t3", open: true });
    expect(ids(termDrawer(ws))).toEqual(["t3", "t2"]);
    expect(calls).toEqual(["open src", "open ", "open src", "close t1"]);
    await expect(restartTerminal(ws, "gone")).resolves.toMatchObject({ id: "t4" });
  });

  // TRM-26: exit closes the tab.
  it("closes the tab of a shell that exited, and the drawer with the last", async () => {
    await newTerminal(ws, "src");
    await newTerminal(ws, "");
    await terminalExited(ws, "t2");
    expect(termDrawer(ws)).toMatchObject({ active: "t1", open: true });
    expect(ids(termDrawer(ws))).toEqual(["t1"]);
    await terminalExited(ws, "t1");
    expect(termDrawer(ws)).toMatchObject({ list: [], open: false });
    expect(calls).toEqual(["open src", "open ", "close t2", "close t1"]);
  });

  it("restores a reloaded window's terminals once, with the drawer up", async () => {
    running = [
      { id: "old", dir: "src", shell: "/bin/zsh", exited: false, exitCode: 0 },
      { id: "done", dir: "", shell: "/bin/zsh", exited: true, exitCode: 1 },
    ];
    await restoreTerminals(ws);
    await restoreTerminals(ws);
    expect(calls, "the one that exited meanwhile is closed, not shown").toEqual([`list ${ws}`, "close done"]);
    expect(termDrawer(ws)).toMatchObject({ active: "old", open: false });
    expect(termDrawer(ws).list).toEqual([{ id: "old", dir: "src", shell: "/bin/zsh" }]);
  });

  it("restores none from a service without terminals", async () => {
    setTransport(createRouterTransport(() => {}));
    await restoreTerminals(ws);
    expect(termDrawer(ws).list).toEqual([]);
  });
});
