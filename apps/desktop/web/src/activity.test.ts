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

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { USED_EVERY_MS, usedThrottle } from "./activity";
import { detectHost, markUsed, resetHost } from "./desktop";

describe("the window in use (spec_054_active_installs AI-01)", () => {
  it("is marked when it opens, then at most every ten minutes, and never while hidden", () => {
    let t = 0;
    let hidden = false;
    const mark = vi.fn();
    const used = usedThrottle(() => t, () => hidden, mark);
    used();
    expect(mark).toHaveBeenCalledTimes(1);
    t += USED_EVERY_MS - 1;
    used();
    expect(mark).toHaveBeenCalledTimes(1);
    hidden = true;
    t += USED_EVERY_MS;
    used();
    expect(mark).toHaveBeenCalledTimes(1);
    hidden = false;
    used();
    expect(mark).toHaveBeenCalledTimes(2);
  });
});

describe("markUsed", () => {
  beforeEach(() => {
    vi.stubGlobal("window", { location: { origin: "http://127.0.0.1:5000" } });
    resetHost();
  });
  afterEach(() => vi.unstubAllGlobals());

  const host = (methods: string[], urls: string[]) =>
    vi.fn(async (url: string) => {
      urls.push(url);
      if (url === "/host/info") return Response.json({ methods });
      return Response.json(null);
    });

  it("tells a host that counts", async () => {
    const urls: string[] = [];
    vi.stubGlobal("fetch", host(["Used"], urls));
    await detectHost();
    markUsed();
    await vi.waitFor(() => expect(urls).toContain("/host/Used"));
  });

  it("says nothing to a host that doesn't", async () => {
    const urls: string[] = [];
    vi.stubGlobal("fetch", host(["CheckUpdate"], urls));
    await detectHost();
    markUsed();
    await new Promise((r) => setTimeout(r, 10));
    expect(urls).not.toContain("/host/Used");
  });
});
