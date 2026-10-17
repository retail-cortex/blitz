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

import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import {
  appMenu,
  bounds,
  consoleLine,
  contentScript,
  englishMenu,
  gpuOff,
  isExternal,
  isLocalHost,
  isWindowMethod,
  keepLine,
  linkArgs,
  maxConsoleLines,
  maxContentHTML,
  maxLinkPrompt,
  menuLabels,
  originOf,
  pageEvents,
  parseDeepLink,
  parseLink,
  previewContent,
  previewId,
  previewStep,
  previewURL,
  printOptions,
  text,
  theme,
  unsaved,
  windowMethods,
} from "./logic";

describe("parseLink", () => {
  it.each([
    { name: "the host's link", line: '{"url":"http://127.0.0.1:5123/open?t=ab12"}', want: "http://127.0.0.1:5123/open?t=ab12" },
    { name: "not JSON", line: "starting", want: undefined },
    { name: "no url", line: '{"link":"x"}', want: undefined },
    { name: "another host", line: '{"url":"http://example.com:5123/open?t=ab12"}', want: undefined },
    { name: "https", line: '{"url":"https://127.0.0.1:5123/open?t=ab12"}', want: undefined },
    { name: "no port", line: '{"url":"http://127.0.0.1/open?t=ab12"}', want: undefined },
    { name: "another path", line: '{"url":"http://127.0.0.1:5123/?t=ab12"}', want: undefined },
    { name: "no ticket", line: '{"url":"http://127.0.0.1:5123/open"}', want: undefined },
    { name: "a ticket that isn't hex", line: '{"url":"http://127.0.0.1:5123/open?t=../x"}', want: undefined },
  ])("$name", ({ line, want }) => expect(parseLink(line)).toBe(want));
});

describe("originOf", () => {
  it("is the URL's origin, or empty", () => {
    expect(originOf("http://127.0.0.1:5123/open?t=ab")).toBe("http://127.0.0.1:5123");
    expect(originOf("not a url")).toBe("");
  });
});

describe("isExternal", () => {
  it.each([
    { url: "https://example.com/a", want: true },
    { url: " http://example.com ", want: true },
    { url: "mailto:someone@example.com", want: true },
    { url: "mailto:", want: false },
    { url: "file:///etc/passwd", want: false },
    { url: "javascript:alert(1)", want: false },
    { url: "blitz://open?dir=/tmp", want: false },
    { url: "nonsense", want: false },
  ])("$url: $want", ({ url, want }) => expect(isExternal(url)).toBe(want));
});

describe("the app menu", () => {
  it("takes the page's words, English where one is missing", () => {
    expect(menuLabels(undefined)).toEqual(englishMenu);
    expect(menuLabels({ about: "À propos de Blitz", settings: "  ", quit: 3 })).toEqual({ ...englishMenu, about: "À propos de Blitz" });
  });

  it("has About and Settings for the page, then Hide and Quit", () => {
    const items = appMenu(englishMenu);
    expect(items[0]).toEqual({ label: "About Blitz", event: "menu:about" });
    expect(items).toContainEqual({ label: "Settings…", accelerator: "CmdOrCtrl+,", event: "menu:settings" });
    expect(items.at(-1)).toEqual({ label: "Quit Blitz", accelerator: "CmdOrCtrl+Q", role: "quit" });
    expect(items.filter((i) => i === "separator")).toHaveLength(3);
  });
});

describe("unsaved", () => {
  it.each([
    { name: "none", v: undefined, want: { message: "", quit: "", cancel: "" } },
    { name: "an empty message", v: { message: "", quit: "Quit" }, want: { message: "", quit: "", cancel: "" } },
    { name: "changes", v: { message: "2 files aren't saved.", quit: "Quitter", cancel: "Annuler" }, want: { message: "2 files aren't saved.", quit: "Quitter", cancel: "Annuler" } },
    { name: "changes without the buttons' words", v: { message: "Unsaved." }, want: { message: "Unsaved.", quit: "Quit", cancel: "Cancel" } },
  ])("$name", ({ v, want }) => expect(unsaved(v)).toEqual(want));
});

describe("what the page sends", () => {
  it("takes a theme, else follows the system", () => {
    expect(theme("dark")).toBe("dark");
    expect(theme("light")).toBe("light");
    expect(theme("purple")).toBe("system");
  });

  it("prints on A4 or Letter with 15 mm margins", () => {
    expect(printOptions("Letter").pageSize).toBe("Letter");
    expect(printOptions("Tabloid").pageSize).toBe("A4");
    expect(printOptions("A4").margins.top).toBeCloseTo(0.5906, 3);
  });

  it("calls only the window's methods", () => {
    expect(isWindowMethod("PrintPDF")).toBe(true);
    expect(isWindowMethod("GetPrefs")).toBe(false);
    expect(isWindowMethod(7)).toBe(false);
  });

  it("reads text arguments", () => {
    expect(text(["a", 2], 0)).toBe("a");
    expect(text(["a", 2], 1)).toBe("");
    expect(text([], 3)).toBe("");
  });
});

describe("parseDeepLink", () => {
  it.each([
    { name: "a folder and a prompt", raw: "blitz://open?dir=/tmp/shop&prompt=Fix%20the%20tests", want: { dir: "/tmp/shop", prompt: "Fix the tests" } },
    { name: "the home folder", raw: "blitz://open?dir=~/shop", want: { dir: "/home/me/shop", prompt: "" } },
    { name: "a Windows folder", raw: "blitz://open?dir=C:%5CUsers%5Cme", want: { dir: "C:\\Users\\me", prompt: "" } },
    { name: "a relative folder", raw: "blitz://open?dir=shop", want: undefined },
    { name: "no folder", raw: "blitz://open?prompt=hi", want: undefined },
    { name: "another action", raw: "blitz://run?dir=/tmp", want: undefined },
    { name: "another scheme", raw: "https://open?dir=/tmp", want: undefined },
    { name: "a prompt too long", raw: `blitz://open?dir=/tmp&prompt=${"x".repeat(maxLinkPrompt + 1)}`, want: undefined },
  ])("$name", ({ raw, want }) => expect(parseDeepLink(raw, "/home/me")).toEqual(want));

  it("finds the links among a command line's arguments", () => {
    expect(linkArgs(["/app/blitz", "--flag", "blitz://open?dir=/tmp"])).toEqual(["blitz://open?dir=/tmp"]);
  });
});

describe("previewURL", () => {
  it.each([
    { raw: "http://localhost:3000/a?b#c", want: "http://localhost:3000/a?b#c" },
    { raw: "https://example.com", want: "https://example.com/" },
    { raw: "  localhost:5173 ", want: "http://localhost:5173/" },
    { raw: "localhost", want: "http://localhost/" },
    { raw: "127.0.0.1:8000/docs", want: "http://127.0.0.1:8000/docs" },
    { raw: "[::1]:8080", want: "http://[::1]:8080/" },
    { raw: "app.localhost:3000", want: "http://app.localhost:3000/" },
    { raw: "example.com", want: "https://example.com/" },
    { raw: "example.com:8443/x", want: "https://example.com:8443/x" },
    { raw: "file:///etc/passwd", want: undefined },
    { raw: "javascript:alert(1)", want: undefined },
    { raw: "data:text/html,hi", want: undefined },
    { raw: "blitz://open?dir=/", want: undefined },
    { raw: "mailto:a@b.c", want: undefined },
    { raw: "http://", want: undefined },
    { raw: "", want: undefined },
    { raw: 42, want: undefined },
  ])("$raw", ({ raw, want }) => expect(previewURL(raw)).toBe(want));
});

describe("isLocalHost", () => {
  it("is this machine's names only", () => {
    for (const h of ["localhost", "LOCALHOST", "web.localhost", "127.0.0.1", "[::1]", "::1"]) expect(isLocalHost(h)).toBe(true);
    for (const h of ["example.com", "localhost.example.com", "127.0.0.2", ""]) expect(isLocalHost(h)).toBe(false);
  });
});

describe("previewId", () => {
  it("takes plain IDs only", () => {
    expect(previewId("web-1_a")).toBe("web-1_a");
    expect(previewId("")).toBeUndefined();
    expect(previewId("a/b")).toBeUndefined();
    expect(previewId("x".repeat(65))).toBeUndefined();
    expect(previewId(7)).toBeUndefined();
  });
});

describe("bounds", () => {
  it("rounds, and keeps them on the window", () => {
    expect(bounds({ x: 10.4, y: -3, width: 200.6, height: 1e9 })).toEqual({ x: 10, y: 0, width: 201, height: 100_000 });
  });
  it.each([null, "x", {}, { x: 1, y: 1, width: 1 }, { x: 1, y: 1, width: 1, height: NaN }, { x: "1", y: 1, width: 1, height: 1 }])("refuses %j", (v) => expect(bounds(v)).toBeUndefined());
});

describe("previewStep", () => {
  it("is a step, never an address", () => {
    for (const s of ["back", "forward", "reload", "stop"]) expect(previewStep(s)).toBe(s);
    expect(previewStep("localhost:3000")).toBeUndefined();
    expect(previewStep({})).toBeUndefined();
  });
});

describe("console", () => {
  it("keeps errors, with where they came from", () => {
    expect(consoleLine("error", "boom", "http://x/app.js", 12)).toBe("boom (http://x/app.js:12)");
    expect(consoleLine("error", "boom", "http://x/app.js", 0)).toBe("boom (http://x/app.js)");
    expect(consoleLine("error", "boom", "", 0)).toBe("boom");
    expect(consoleLine("error", "x".repeat(5000), "", 0)).toHaveLength(4000);
    expect(consoleLine("warning", "meh", "", 0)).toBeUndefined();
    expect(consoleLine("info", "hi", "", 0)).toBeUndefined();
  });
  it("keeps the latest lines", () => {
    const lines: string[] = [];
    for (let i = 0; i < maxConsoleLines + 5; i++) keepLine(lines, String(i));
    expect(lines).toHaveLength(maxConsoleLines);
    expect(lines[0]).toBe("5");
    expect(lines.at(-1)).toBe(String(maxConsoleLines + 4));
  });
});

describe("previewContent", () => {
  it("is strings, cut to the limit", () => {
    expect(previewContent({ url: "http://x/", title: "T", html: "<p>a</p>", selection: 3 })).toEqual({ url: "http://x/", title: "T", html: "<p>a</p>", selection: "" });
    expect(previewContent(undefined)).toEqual({ url: "", title: "", html: "", selection: "" });
    expect(previewContent({ html: "x".repeat(maxContentHTML + 1) }).html).toHaveLength(maxContentHTML);
  });
  it("reads the page and the selection", () => {
    // The script as a function of a page like the preview's.
    const range = { cloneContents: () => "<b>sel</b>" };
    const page = {
      window: { getSelection: () => ({ rangeCount: 1, isCollapsed: false, getRangeAt: () => range }) },
      document: {
        title: "Home",
        documentElement: { outerHTML: "<html>hi</html>" },
        createElement: () => {
          const box = { innerHTML: "", appendChild: (s: string) => void (box.innerHTML += s) };
          return box;
        },
      },
      location: { href: "http://localhost:3000/" },
    };
    const run = new Function("window", "document", "location", `return ${contentScript};`);
    expect(run(page.window, page.document, page.location)).toEqual({ url: "http://localhost:3000/", title: "Home", html: "<html>hi</html>", selection: "<b>sel</b>" });
    page.window.getSelection = () => ({ rangeCount: 0, isCollapsed: true, getRangeAt: () => range });
    expect(run(page.window, page.document, page.location).selection).toBe("");
  });
});

// A sandboxed preload can't import logic.ts: its own lists must say the same.
describe("preload", () => {
  const src = readFileSync(path.join(__dirname, "preload.ts"), "utf8");
  const list = (re: RegExp) => [...(src.match(re)?.[1] ?? "").matchAll(/"([^"]+)"/g)].map((m) => m[1]);
  it("offers the window's calls", () => expect(list(/const methods = \[([^\]]*)\]/)).toEqual([...windowMethods]));
  it("passes on the window's events", () => expect(list(/const events = new Set\(\[([^\]]*)\]\)/)).toEqual([...pageEvents]));
});

describe("gpuOff", () => {
  it.each([
    { env: {}, prefs: undefined, want: false },
    { env: {}, prefs: '{"gpu":"on"}', want: false },
    { env: {}, prefs: '{"gpu":"off"}', want: true },
    { env: { BLITZ_GPU: "off" }, prefs: '{"gpu":"on"}', want: true },
    { env: {}, prefs: "not json", want: false },
  ])("$env $prefs", ({ env, prefs, want }) => expect(gpuOff(env, prefs)).toBe(want));
});
