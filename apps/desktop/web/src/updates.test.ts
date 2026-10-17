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
import type { UpdateStatus } from "./desktop";
import { shouldDownload, updateLine } from "./updates";

const base: UpdateStatus = { current: "0.6.6", latest: "v0.6.6", available: false, can: true, downloading: false, ready: false };

describe("updateLine", () => {
  it.each([
    { name: "checking", s: null, key: "desktop.update.checking" },
    { name: "the latest", s: base, key: "desktop.update.latest", version: "0.6.6" },
    { name: "one available", s: { ...base, latest: "v0.6.7", available: true }, key: "desktop.update.available", version: "v0.6.7" },
    { name: "downloading", s: { ...base, latest: "v0.6.7", available: true, downloading: true }, key: "desktop.update.downloading", version: "v0.6.7" },
    { name: "ready", s: { ...base, latest: "v0.6.7", available: true, ready: true }, key: "desktop.update.ready", version: "v0.6.7" },
    { name: "failed", s: { ...base, error: "offline" }, key: "desktop.update.failed" },
    { name: "a package manager's copy", s: { ...base, can: false, reason: "Installed by apt" }, key: "desktop.update.cannot" },
  ])("$name", ({ s, key, version }) => {
    const line = updateLine(s as UpdateStatus | null);
    expect(line.key).toBe(key);
    if (version) expect(line.vars?.version).toBe(version);
  });
});

describe("shouldDownload", () => {
  it.each([
    { name: "one available", s: { ...base, available: true }, want: true },
    { name: "none", s: base, want: false },
    { name: "already ready", s: { ...base, available: true, ready: true }, want: false },
    { name: "downloading", s: { ...base, available: true, downloading: true }, want: false },
    { name: "the app can't update itself", s: { ...base, available: true, can: false }, want: false },
    { name: "the last try failed", s: { ...base, available: true, error: "x" }, want: false },
  ])("$name", ({ s, want }) => expect(shouldDownload(s)).toBe(want));
});
