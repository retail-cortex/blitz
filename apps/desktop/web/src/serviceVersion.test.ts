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
import { staleReason, waitForService, withTimeout } from "./serviceVersion";

describe("staleReason", () => {
  const info = { version: "1.4.0", executable: "/usr/lib/blitz-desktop/blitzd", pid: 42 };
  it("accepts the service the app expects", () => {
    expect(staleReason("1.4.0", info, true)).toBe("");
    expect(staleReason("dev", { ...info, version: "dev" }, true)).toBe("");
  });
  it("flags a service too old to describe itself", () => {
    expect(staleReason("1.4.0", undefined, true)).toBe("old");
  });
  it("flags a service whose program is gone, before its version", () => {
    expect(staleReason("1.4.0", info, false)).toBe("missing");
    expect(staleReason("1.5.0", info, false)).toBe("missing");
  });
  it("flags a service whose program was replaced since it started, even at the same version", () => {
    expect(staleReason("dev", { ...info, version: "dev", replaced: true }, true)).toBe("missing");
    expect(staleReason("1.4.0", { ...info, replaced: true }, true)).toBe("missing");
  });
  it("flags another version", () => {
    expect(staleReason("1.5.0", info, true)).toBe("mismatch");
    expect(staleReason("dev", info, true)).toBe("mismatch");
  });
});

describe("waitForService", () => {
  const noSleep = () => Promise.resolve();
  it("waits for a service that starts late", async () => {
    let calls = 0;
    const status = async () => ({ running: ++calls >= 4 });
    const got = await waitForService(status, true, 15_000, 300, noSleep);
    expect(got).toEqual({ status: { running: true }, settled: true });
    expect(calls).toBe(4);
  });
  it("waits for a service to stop", async () => {
    let calls = 0;
    const got = await waitForService(async () => ({ running: ++calls < 3 }), false, 15_000, 300, noSleep);
    expect(got.settled).toBe(true);
    expect(calls).toBe(3);
  });
  it("gives up after the timeout, with the last status", async () => {
    let calls = 0;
    const got = await waitForService(async () => (calls++, { running: false }), true, 1_000, 300, noSleep);
    expect(got).toEqual({ status: { running: false }, settled: false });
    expect(calls).toBe(5); // at 0, 300, 600, 900 and 1200 ms
  });
});

describe("withTimeout", () => {
  it("passes an answer through", async () => {
    await expect(withTimeout(Promise.resolve(7), 1_000)).resolves.toBe(7);
  });
  it("rejects a call that doesn't answer", async () => {
    await expect(withTimeout(new Promise(() => {}), 10)).rejects.toThrow("no answer");
  });
});
