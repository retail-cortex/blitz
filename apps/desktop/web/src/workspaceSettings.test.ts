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

import { beforeEach, describe, expect, it, vi } from "vitest";

const setPermissionMode = vi.fn();
const setConfigValue = vi.fn();
vi.mock("./api", () => ({
  workspaces: { setPermissionMode: (r: unknown) => setPermissionMode(r) },
  config: { setConfigValue: (r: unknown) => setConfigValue(r) },
}));

const { keepPermissionMode, sliderCommit } = await import("./workspaceSettings");

describe("sliderCommit", () => {
  it.each([
    ["a Tab or click at Default saves nothing", false, 0, undefined, undefined],
    ["a blur with no move saves nothing", false, 0.7, 0.7, undefined],
    ["moved back to the set value saves nothing", true, 0.7, 0.7, undefined],
    ["moved from Default saves the value", true, 0, undefined, "0"],
    ["moved to another value saves it", true, 1.2, 0.7, "1.2"],
  ] as const)("%s", (_, moved, shown, value, want) => {
    expect(sliderCommit(moved, shown, value)).toBe(want);
  });
});

// A mode picked in the status bar is kept as the workspace's, as Settings
// keeps it: applied first, then saved, so new chats and a restart open in
// it; a mode the service refuses (bypass without the sandbox) isn't kept
// (spec_005 APR-14; N26 desktop-2#17).
describe("keepPermissionMode", () => {
  beforeEach(() => {
    setPermissionMode.mockReset();
    setConfigValue.mockReset().mockResolvedValue({});
  });
  it.each([
    { name: "a mode is applied and kept", pick: "accept-edits", canonical: "accept-edits" },
    { name: "Claude Code's spelling is kept as Blitz's", pick: "acceptEdits", canonical: "accept-edits" },
  ])("$name", async ({ pick, canonical }) => {
    setPermissionMode.mockResolvedValue({ mode: canonical });
    expect(await keepPermissionMode("/ws", pick)).toBe(canonical);
    expect(setPermissionMode).toHaveBeenCalledWith({ workspace: "/ws", mode: pick });
    expect(setConfigValue).toHaveBeenCalledWith({ workspace: "/ws", key: "blitz.permission_mode", value: canonical });
  });
  it("a mode the service refuses isn't kept", async () => {
    setPermissionMode.mockRejectedValue(new Error("bypass mode needs the OS sandbox"));
    await expect(keepPermissionMode("/ws", "bypass")).rejects.toThrow("sandbox");
    expect(setConfigValue).not.toHaveBeenCalled();
  });
});
