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
// app in it (no window.blitzElectron), and fetch is the tray answering.
const stubWindow = () => vi.stubGlobal("window", { location: { origin: "http://127.0.0.1:5000" } });
stubWindow();

const {
  appVersion,
  canCall,
  canPreview,
  canUpdate,
  checkUpdate,
  chooseWorkspace,
  downloadUpdate,
  restartToUpdate,
  detectHost,
  fileManager,
  getPrefs,
  inElectron,
  isLinuxApp,
  licenseText,
  nativeGlass,
  onAppMenu,
  onNotificationOpen,
  onPreviewState,
  openFolder,
  openURL,
  previewBounds,
  previewCapture,
  previewClose,
  previewConsole,
  previewContent,
  previewNavigate,
  previewOpen,
  previewVisible,
  printPDF,
  resetHost,
  revealPath,
  serviceStatus,
  setAppMenu,
  toggleFullscreen,
  windowError,
} = await import("./desktop");

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

/**
 * The Electron app's window (its preload's window.blitzElectron),
 * answering methods with results; calls records what the page asked of
 * it, and emit sends one of its events.
 */
function electronWindow(results: Record<string, unknown>, calls: { method: string; args: unknown[] }[] = [], nativeGlass = false) {
  const listeners = new Map<string, ((arg: unknown) => void)[]>();
  const w = {
    methods: Object.keys(results),
    nativeGlass,
    call: vi.fn(async (method: string, ...args: unknown[]) => {
      calls.push({ method, args });
      return results[method];
    }),
    on: (event: string, f: (arg: unknown) => void) => {
      listeners.set(event, [...(listeners.get(event) ?? []), f]);
      return () => listeners.set(event, (listeners.get(event) ?? []).filter((g) => g !== f));
    },
  };
  vi.stubGlobal("window", { location: { origin: "http://127.0.0.1:5000" }, blitzElectron: w });
  return { emit: (event: string, arg?: unknown) => (listeners.get(event) ?? []).forEach((f) => f(arg)) };
}

/** A localStorage of its own for a test. */
function storage(initial: Record<string, string> = {}) {
  const m = new Map(Object.entries(initial));
  vi.stubGlobal("localStorage", {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
  });
  return m;
}

describe("the Electron app's window", () => {
  it("answers what only a window can do; its host the rest", async () => {
    const calls: { method: string; args: unknown[] }[] = [];
    const fetched: Call[] = [];
    electronWindow({ ChooseWorkspace: "/home/me/shop", OpenURL: undefined, PrintPDF: btoa("%PDF-1.4"), ToggleFullscreen: undefined }, calls, true);
    vi.stubGlobal("fetch", tray({ Version: "1.2.3", OpenFolder: null }, fetched));
    await detectHost();
    expect(inElectron()).toBe(true);
    expect(nativeGlass()).toBe(true);
    expect(canCall("ChooseWorkspace")).toBe(true);
    expect(canCall("OpenFolder")).toBe(true);
    expect(canCall("SandboxStatus")).toBe(false);

    expect(await chooseWorkspace()).toBe("/home/me/shop");
    await openURL("https://example.com");
    expect(new TextDecoder().decode(await printPDF("<p>hi</p>", "Letter"))).toBe("%PDF-1.4");
    await toggleFullscreen();
    expect(calls.map((c) => c.method)).toEqual(["ChooseWorkspace", "OpenURL", "PrintPDF", "ToggleFullscreen"]);
    expect(calls[2].args).toEqual(["<p>hi</p>", "Letter"]);

    await openFolder("/home/me/shop");
    expect(fetched.at(-1)).toEqual({ url: "/host/OpenFolder", method: "POST", body: ["/home/me/shop"] });
    expect(await appVersion()).toBe("1.2.3");
  });

  it("hears its menu and notifications", async () => {
    const { emit } = electronWindow({ SetMenu: undefined });
    const items: string[] = [];
    const dirs: string[] = [];
    const offMenu = onAppMenu((item) => items.push(item));
    const offOpen = onNotificationOpen((dir) => dirs.push(dir));
    emit("menu:settings");
    emit("menu:about");
    emit("notification:open", "/home/me/shop");
    emit("notification:open", 7); // not a folder
    offMenu();
    offOpen();
    emit("menu:settings");
    expect(items).toEqual(["settings", "about"]);
    expect(dirs).toEqual(["/home/me/shop"]);
    setAppMenu({ about: "À propos", settings: "Réglages…", hide: "", hide_others: "", show_all: "", quit: "" });
  });

  it("shows web previews (EL-100), and says how they change", async () => {
    const calls: { method: string; args: unknown[] }[] = [];
    const content = { url: "http://localhost:3000/", title: "Home", html: "<p>hi</p>", selection: "" };
    const { emit } = electronWindow(
      {
        PreviewOpen: "http://localhost:3000/",
        PreviewBounds: undefined,
        PreviewVisible: undefined,
        PreviewNavigate: undefined,
        PreviewClose: undefined,
        PreviewCapture: btoa("PNG"),
        PreviewContent: content,
        PreviewConsole: ["boom"],
      },
      calls,
    );
    expect(canPreview()).toBe(true);
    expect(await previewOpen("w1", "localhost:3000")).toBe("http://localhost:3000/");
    await previewBounds("w1", { x: 1, y: 2, width: 3, height: 4 });
    await previewVisible("w1", true);
    await previewNavigate("w1", "back");
    expect(new TextDecoder().decode(await previewCapture("w1"))).toBe("PNG");
    expect(await previewContent("w1")).toEqual(content);
    expect(await previewConsole("w1")).toEqual(["boom"]);
    await previewClose("w1");
    expect(calls.map((c) => c.method)).toEqual(["PreviewOpen", "PreviewBounds", "PreviewVisible", "PreviewNavigate", "PreviewCapture", "PreviewContent", "PreviewConsole", "PreviewClose"]);
    expect(calls[1].args).toEqual(["w1", { x: 1, y: 2, width: 3, height: 4 }]);

    const states: unknown[] = [];
    const off = onPreviewState((s) => states.push(s));
    emit("preview:state", { id: "w1", url: "http://localhost:3000/", title: "Home", loading: true, back: false });
    emit("preview:state", "not a state");
    off();
    emit("preview:state", { id: "w1" });
    expect(states).toEqual([{ id: "w1", url: "http://localhost:3000/", title: "Home", loading: true, back: false, forward: false }]);
  });

  it("says what went wrong without Electron's prefix", async () => {
    expect(windowError(new Error("Error invoking remote method 'blitz:call': Error: not opening \"file:///x\""))).toBe('not opening "file:///x"');
    expect(windowError("plain")).toBe("plain");
    electronWindow({ PreviewOpen: undefined });
    (window as unknown as { blitzElectron: { call: () => Promise<never> } }).blitzElectron.call = () => Promise.reject(new Error("Error invoking remote method 'blitz:call': Error: refused"));
    await expect(previewOpen("w1", "file:///x")).rejects.toThrow(/^refused$/);
  });

  it("has no web previews elsewhere", async () => {
    electronWindow({ OpenURL: undefined });
    expect(canPreview()).toBe(false);
    stubWindow();
    expect(canPreview()).toBe(false);
    expect(onPreviewState(() => {})).toBeTypeOf("function");
    // Placing, showing and closing a preview that isn't there is nothing.
    await previewBounds("w1", { x: 0, y: 0, width: 1, height: 1 });
    await previewVisible("w1", false);
    await previewClose("w1");
  });

  it("is the Linux app on Linux, for the graphics card setting", () => {
    electronWindow({});
    vi.stubGlobal("navigator", { userAgent: "Mozilla/5.0 (X11; Linux x86_64) Electron/44.5.1" });
    expect(isLinuxApp()).toBe(true);
    vi.stubGlobal("navigator", { userAgent: "Mozilla/5.0 (Macintosh) Electron/44.5.1" });
    expect(isLinuxApp()).toBe(false);
  });

  it("isn't macOS's translucent window unless it says so", () => {
    electronWindow({});
    expect(nativeGlass()).toBe(false);
  });
});

describe("preferences kept by the host", () => {
  const key = "blitz.desktop.prefs";
  const shop = { workspaces: [{ dir: "/home/me/shop", name: "Shop", open: true }], active: "/home/me/shop" };

  it("moves the browser's to the host the first time, then forgets them", async () => {
    const m = storage({ [key]: JSON.stringify(shop) });
    const fetched: Call[] = [];
    vi.stubGlobal("fetch", tray({ GetPrefs: { theme: "system", workspaces: [] }, SavePrefs: { ...shop, theme: "system" } }, fetched));
    await detectHost();
    const p = await getPrefs();
    expect(p.workspaces.map((w) => w.dir)).toEqual(["/home/me/shop"]);
    const saved = fetched.find((c) => c.url === "/host/SavePrefs");
    expect((saved?.body as { workspaces: { dir: string }[] }[])[0].workspaces[0].dir).toBe("/home/me/shop");
    expect(m.has(key)).toBe(false);
  });

  it("keeps the host's when it has workspaces, and forgets the browser's", async () => {
    const m = storage({ [key]: JSON.stringify(shop) });
    const fetched: Call[] = [];
    vi.stubGlobal("fetch", tray({ GetPrefs: { workspaces: [{ dir: "/home/me/other", open: true }] }, SavePrefs: null }, fetched));
    await detectHost();
    const p = await getPrefs();
    expect(p.workspaces.map((w) => w.dir)).toEqual(["/home/me/other"]);
    expect(fetched.some((c) => c.url === "/host/SavePrefs")).toBe(false);
    expect(m.has(key)).toBe(false);
  });

  it("leaves the host's alone when the browser has none", async () => {
    storage();
    vi.stubGlobal("fetch", tray({ GetPrefs: { theme: "dark", workspaces: [] }, SavePrefs: null }));
    await detectHost();
    expect((await getPrefs()).theme).toBe("dark");
  });
});

describe("updates", () => {
  it("are the host's, and restarting quits the window after the swap is started", async () => {
    const calls: { method: string; args: unknown[] }[] = [];
    const fetched: Call[] = [];
    electronWindow({ Quit: undefined }, calls);
    const ready = { current: "0.6.6", latest: "v0.6.7", available: true, can: true, downloading: false, ready: true };
    vi.stubGlobal("fetch", tray({ CheckUpdate: ready, DownloadUpdate: ready, ApplyUpdate: null }, fetched));
    await detectHost();
    expect(canUpdate()).toBe(true);
    expect(await checkUpdate()).toEqual(ready);
    expect(fetched.at(-1)).toEqual({ url: "/host/CheckUpdate", method: "POST", body: [false] });
    await checkUpdate(true);
    expect(fetched.at(-1)?.body).toEqual([true]);
    expect((await downloadUpdate()).ready).toBe(true);
    await restartToUpdate();
    expect(fetched.at(-1)?.url).toBe("/host/ApplyUpdate");
    expect(calls.map((c) => c.method)).toEqual(["Quit"]);
  });

  it("aren't offered where nothing answers", async () => {
    vi.stubGlobal("fetch", tray({ Version: "1" }));
    await detectHost();
    expect(canUpdate()).toBe(false);
  });
});
