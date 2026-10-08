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

// Tests run without a DOM: the window is a plain object, with no desktop
// app in it (no window.go), and fetch is the tray answering.
const stubWindow = () => vi.stubGlobal("window", { location: { origin: "http://127.0.0.1:5000" } });
stubWindow();

const { appVersion, canCall, detectHost, fileManager, licenseText, openFolder, resetHost, revealPath, serviceStatus } = await import("./desktop");

type Call = { url: string; method: string; body?: unknown };

/** A tray answering methods with results; calls records what the page asked. */
function tray(results: Record<string, unknown>, calls: Call[] = []) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined });
    if (url === "/host/info") return Response.json({ methods: Object.keys(results) });
    const m = url.replace("/host/", "");
    const r = results[m];
    if (r instanceof Error) return Response.json({ error: r.message }, { status: 500 });
    return Response.json(r ?? null);
  });
}

beforeEach(() => {
  stubWindow();
  resetHost();
});
afterEach(() => vi.unstubAllGlobals());

describe("the tray as host", () => {
  it("answers the methods it lists, with their arguments", async () => {
    const calls: Call[] = [];
    vi.stubGlobal("fetch", tray({ Version: "1.2.3", OpenFolder: null, FileManager: "File Explorer" }, calls));
    await detectHost();
    expect(canCall("OpenFolder")).toBe(true);
    expect(canCall("ChooseWorkspace")).toBe(false);
    expect(await appVersion()).toBe("1.2.3");
    expect(await fileManager()).toBe("File Explorer");
    await openFolder("C:\\Users\\me\\logs");
    expect(calls.at(-1)).toEqual({ url: "/host/OpenFolder", method: "POST", body: ["C:\\Users\\me\\logs"] });
  });

  it("reports a method's error", async () => {
    vi.stubGlobal("fetch", tray({ RevealPath: new Error("not showing a.txt: not an absolute path") }));
    await detectHost();
    await expect(revealPath("a.txt")).rejects.toThrow("not an absolute path");
  });

  it("falls back to the browser's stand-ins for what it doesn't do", async () => {
    vi.stubGlobal("fetch", tray({ Version: "1.2.3" }));
    await detectHost();
    expect(await licenseText("notice")).not.toBe("");
    await expect(openFolder("/x")).rejects.toThrow("not running inside");
  });

  it("gives the service's status as the tray sees it", async () => {
    const status = { running: true, installed: true, socket: "C:\\s", service: "C:\\blitzd.exe", tray: "C:\\blitz-tray.exe", tray_installed: true };
    vi.stubGlobal("fetch", tray({ ServiceStatus: status }));
    await detectHost();
    expect(await serviceStatus()).toEqual(status);
  });
});

describe("no host", () => {
  it.each([
    ["a page that isn't JSON (a development server)", vi.fn(async () => new Response("<html>"))],
    ["not found", vi.fn(async () => new Response("", { status: 404 }))],
    ["no answer", vi.fn(async () => Promise.reject(new TypeError("failed")))],
    ["no methods", vi.fn(async () => Response.json({}))],
  ])("%s: nothing can be called", async (_, fetch) => {
    vi.stubGlobal("fetch", fetch);
    await detectHost();
    expect(canCall("Version")).toBe(false);
    expect(await appVersion()).toBe("dev");
    expect(await fileManager()).toBeNull();
  });
});
