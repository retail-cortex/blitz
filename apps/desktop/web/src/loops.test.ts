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

import { Window } from "happy-dom";
import { act, createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { formatInterval, listLoops, parseInterval, startLoop, stopAllLoops, useLoopsEndWith } from "./loops";

describe("intervals", () => {
  it.each([
    ["10", 600_000],
    ["10m", 600_000],
    ["1h30m", 5_400_000],
    ["90s", 90_000],
    ["30s", undefined], // under a minute
    ["0", undefined],
    ["soon", undefined],
    ["", undefined],
  ])("%s is %s ms", (s, ms) => {
    expect(parseInterval(s)).toBe(ms);
  });
  it.each([
    [600_000, "10m"],
    [5_400_000, "1h30m"],
    [90_000, "1m30s"],
    [0, "0s"],
  ])("%i ms is written %s", (ms, s) => {
    expect(formatInterval(ms)).toBe(s);
  });
});

describe("a workspace's loops", () => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  afterEach(() => {
    stopAllLoops();
    vi.useRealTimers();
  });

  it.each([
    { name: "its conversation closing", remount: false },
    { name: "its conversation remounting (a reconnect)", remount: true },
  ])("stop with $name, and fire no more", async ({ remount }) => {
    // A DOM for React, set before react-dom loads (it looks once).
    const g = globalThis as { window?: unknown; document?: unknown };
    const win = new Window();
    g.window ??= win;
    g.document ??= win.document;
    const { createRoot } = await import("react-dom/client");
    vi.useFakeTimers();
    const fire = vi.fn();
    const Chat = ({ dir }: { dir: string }) => {
      useLoopsEndWith(dir);
      return null;
    };
    const root = createRoot((g.document as Document).createElement("div"));
    act(() => root.render(createElement(Chat, { key: "1", dir: "/w/shop" })));
    startLoop("/w/shop", "check the build", 60_000, fire, () => false);
    startLoop("/w/notes", "tidy", 60_000, () => {}, () => false);
    vi.advanceTimersByTime(60_000);
    expect(fire).toHaveBeenCalledTimes(1);

    act(() => (remount ? root.render(createElement(Chat, { key: "2", dir: "/w/shop" })) : root.unmount()));
    vi.advanceTimersByTime(120_000);
    expect(fire).toHaveBeenCalledTimes(1);
    expect(listLoops("/w/shop")).toEqual([]);
    expect(listLoops("/w/notes")).toHaveLength(1); // another workspace's goes on
    if (remount) act(() => root.unmount());
  });
});
