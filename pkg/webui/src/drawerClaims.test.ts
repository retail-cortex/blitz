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
import { DrawerClaims, type Ask, type Claim } from "./drawerClaims";

// A host and its drawers, recording what's shown, closed and asked.
function setup() {
  const log = { shown: null as number | null, closed: [] as number[], asked: [] as Ask[] };
  const claims = new DrawerClaims(
    (id) => (log.shown = id),
    (a) => log.asked.push(a),
  );
  const drawer = (id: number, dirty = false): Claim => ({ id, close: () => log.closed.push(id), dirty: () => dirty, name: () => `drawer ${id}` });
  return { log, claims, drawer };
}

describe("DrawerClaims", () => {
  it("shows the drawer that opens, and none once it goes", () => {
    const { log, claims, drawer } = setup();
    const release = claims.claim(drawer(1));
    expect(log.shown).toBe(1);
    release();
    expect(log.shown).toBeNull();
  });

  it("closes the drawer another replaces", () => {
    const { log, claims, drawer } = setup();
    const first = claims.claim(drawer(1));
    claims.claim(drawer(2));
    expect(log.closed).toEqual([1]);
    expect(log.shown).toBe(2);
    first(); // the first going away doesn't hide the second
    expect(log.shown).toBe(2);
  });

  it("asks before replacing one with unsaved changes; discarding closes it", () => {
    const { log, claims, drawer } = setup();
    claims.claim(drawer(1, true));
    claims.claim(drawer(2));
    expect(log.closed).toEqual([]);
    expect(log.asked.map((a) => a.name)).toEqual(["drawer 1"]);
    log.asked[0].discard();
    expect(log.closed).toEqual([1]);
    expect(log.shown).toBe(2);
  });

  it("keeping the changes closes the new drawer and shows the old one again", () => {
    const { log, claims, drawer } = setup();
    claims.claim(drawer(1, true));
    const second = claims.claim(drawer(2));
    log.asked[0].keep();
    expect(log.closed).toEqual([2]);
    expect(log.shown).toBe(1);
    second(); // the new one unmounting leaves the old one shown
    expect(log.shown).toBe(1);
  });

  it.each([
    { name: "closes a drawer without changes", dirty: false, closed: [1], asked: 0 },
    { name: "asks before closing one with unsaved changes", dirty: true, closed: [], asked: 1 },
  ])("$name", ({ dirty, closed, asked }) => {
    const { log, claims, drawer } = setup();
    const d = drawer(1, dirty);
    claims.claim(d);
    claims.requestClose(d);
    expect(log.closed).toEqual(closed);
    expect(log.asked).toHaveLength(asked);
  });
});
