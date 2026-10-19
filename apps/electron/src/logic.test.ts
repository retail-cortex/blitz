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
  isPageCertificate,
  originOf,
  pageEvents,
  parseDeepLink,
  parseRunLink,
  parseTasksLink,
  tasksURL,
  parseLink,
  renewEvery,
  shouldRenew,
  pictureExtension,
  previewContent,
  previewId,
  previewStep,
  addressURL,
  previewURL,
  printCheckScript,
  printLoadScript,
  printOptions,
  printWait,
  text,
  theme,
  unsaved,
  waitForPrint,
  windowMethods,
} from "./logic";

describe("parseLink", () => {
  const fp = Array(32).fill("AB").join(":");
  const line = (url: string, fingerprint: string = fp) => JSON.stringify({ url, fingerprint });
  it.each([
    { name: "the host's link", line: line("https://127.0.0.1:5123/open?t=ab12"), want: { url: "https://127.0.0.1:5123/open?t=ab12", fingerprint: fp } },
    { name: "not JSON", line: "starting", want: undefined },
    { name: "no url", line: '{"link":"x"}', want: undefined },
    { name: "no fingerprint", line: '{"url":"https://127.0.0.1:5123/open?t=ab12"}', want: undefined },
    { name: "a fingerprint too short", line: line("https://127.0.0.1:5123/open?t=ab12", "AB:CD"), want: undefined },
    { name: "a fingerprint in lower case", line: line("https://127.0.0.1:5123/open?t=ab12", fp.toLowerCase()), want: undefined },
    { name: "another host", line: line("https://example.com:5123/open?t=ab12"), want: undefined },
    { name: "plain http", line: line("http://127.0.0.1:5123/open?t=ab12"), want: undefined },
    { name: "no port", line: line("https://127.0.0.1/open?t=ab12"), want: undefined },
    { name: "another path", line: line("https://127.0.0.1:5123/?t=ab12"), want: undefined },
    { name: "no ticket", line: line("https://127.0.0.1:5123/open"), want: undefined },
    { name: "a ticket that isn't hex", line: line("https://127.0.0.1:5123/open?t=../x"), want: undefined },
  ])("$name", ({ line, want }) => expect(parseLink(line)).toEqual(want));
});

describe("shouldRenew", () => {
  const origin = "https://127.0.0.1:5123";
  it.each([
    { name: "a certificate error on the page", error: "net::ERR_CERT_DATE_INVALID", url: origin + "/blitz.v1.X/Y", last: 0, now: renewEvery, want: true },
    { name: "the page itself", error: "net::ERR_CERT_AUTHORITY_INVALID", url: origin + "/", last: 0, now: renewEvery, want: true },
    { name: "renewed a moment ago", error: "net::ERR_CERT_DATE_INVALID", url: origin + "/", last: 1000, now: 1000 + renewEvery - 1, want: false },
    { name: "another error", error: "net::ERR_CONNECTION_REFUSED", url: origin + "/", last: 0, now: renewEvery, want: false },
    { name: "another site's certificate", error: "net::ERR_CERT_DATE_INVALID", url: "https://example.com/", last: 0, now: renewEvery, want: false },
    { name: "another port", error: "net::ERR_CERT_DATE_INVALID", url: "https://127.0.0.1:5124/", last: 0, now: renewEvery, want: false },
  ])("$name", ({ error, url, last, now, want }) => expect(shouldRenew(error, url, origin, last, now)).toBe(want));
  it("not before the page is served", () => expect(shouldRenew("net::ERR_CERT_DATE_INVALID", "https://127.0.0.1:5123/", "", 0, renewEvery)).toBe(false));
});

describe("isPageCertificate", () => {
  const fp = Array(32).fill("AB").join(":");
  it.each([
    { name: "the page server's", host: "127.0.0.1", fingerprint: fp, pinned: fp, want: true },
    { name: "another certificate on the loopback address", host: "127.0.0.1", fingerprint: fp.replace("AB", "CD"), pinned: fp, want: false },
    { name: "the same certificate for another host", host: "example.com", fingerprint: fp, pinned: fp, want: false },
    { name: "nothing pinned yet", host: "127.0.0.1", fingerprint: "", pinned: "", want: false },
  ])("$name", ({ host, fingerprint, pinned, want }) => expect(isPageCertificate(host, fingerprint, pinned)).toBe(want));
});

describe("originOf", () => {
  it("is the URL's origin, or empty", () => {
    expect(originOf("https://127.0.0.1:5123/open?t=ab")).toBe("https://127.0.0.1:5123");
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

describe("parseTasksLink", () => {
  it.each([
    ["blitz://tasks", { notify: false, title: "Blitz", body: "" }],
    ["blitz://tasks?notify=1&title=Refund%20triage&body=Approve%20it", { notify: true, title: "Refund triage", body: "Approve it" }],
    ["blitz:tasks/", { notify: false, title: "Blitz", body: "" }],
  ])("reads %s", (raw, want) => expect(parseTasksLink(raw)).toEqual(want));
  it.each(["blitz://open?dir=/x", "https://tasks", "nonsense"])("ignores %s", (raw) => expect(parseTasksLink(raw)).toBeUndefined());
  it("cuts long text", () => expect(parseTasksLink("blitz://tasks?title=" + "x".repeat(500))!.title).toHaveLength(200));
  it("opens the page on the tasks window", () => {
    expect(tasksURL("http://127.0.0.1:5/open?t=ab")).toBe("http://127.0.0.1:5/open?t=ab&tasks");
    expect(tasksURL("http://127.0.0.1:5/")).toBe("http://127.0.0.1:5/?tasks");
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
    { name: "a folder's workers", raw: "blitz://workers?dir=/tmp/shop&prompt=ignored", want: { dir: "/tmp/shop", prompt: "", view: "workers" } },
    { name: "workers without a folder", raw: "blitz://workers", want: undefined },
    { name: "workers of a relative folder", raw: "blitz://workers?dir=shop", want: undefined },
    { name: "another action", raw: "blitz://settings?dir=/tmp", want: undefined },
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

describe("addressURL", () => {
  const google = (q: string) => `https://www.google.com/search?q=${q}`;
  it.each([
    { raw: "localhost:3000", want: "http://localhost:3000/" },
    { raw: "localhost", want: "http://localhost/" },
    { raw: "example.com", want: "https://example.com/" },
    { raw: "https://example.com/a?b", want: "https://example.com/a?b" },
    { raw: "intranet:8080/x", want: "https://intranet:8080/x" },
    { raw: "[::1]:8080", want: "http://[::1]:8080/" },
    { raw: "golang", want: google("golang") },
    { raw: "react hooks", want: google("react%20hooks") },
    { raw: "  what is 2+2? ", want: google("what%20is%202%2B2%3F") },
    { raw: "c++/templates", want: google("c%2B%2B%2Ftemplates") },
    { raw: "react: hooks", want: google("react%3A%20hooks") },
    { raw: "file:///etc/passwd", want: undefined },
    { raw: "javascript:alert(1)", want: undefined },
    { raw: "", want: undefined },
    { raw: 42, want: undefined },
  ])("$raw", ({ raw, want }) => expect(addressURL(raw)).toBe(want));
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

describe("printing waits for fonts and pictures", () => {
  it("starts them loading, and says when they're ready", () => {
    const loaded: string[] = [];
    const font = { load: async () => void loaded.push("font") };
    const img = { complete: false, decode: async () => void loaded.push("img") };
    const doc = { fonts: Object.assign([font], { status: "loading" }), images: [img], body: { getBoundingClientRect: () => ({}) } };
    const run = (code: string) => new Function("document", `return ${code};`)(doc);
    expect(run(printLoadScript)).toBe(true);
    expect(loaded).toEqual(["font", "img"]);
    expect(run(printCheckScript)).toBe(false);
    doc.fonts.status = "loaded";
    img.complete = true;
    expect(run(printCheckScript)).toBe(true);
  });
  it("waits until ready, or the deadline, then lets the page paint", async () => {
    let t = 0;
    const slept: number[] = [];
    const sleep = async (ms: number) => void (slept.push(ms), (t += ms));
    let asks = 0;
    await waitForPrint(async () => ++asks === 3, sleep, () => t);
    expect(asks).toBe(3);
    expect(slept).toEqual([printWait.everyMs, printWait.everyMs, printWait.settleMs]);
    t = 0;
    slept.length = 0;
    await waitForPrint(async () => false, sleep, () => t);
    expect(t).toBe(printWait.deadlineMs + printWait.settleMs);
    t = 0;
    slept.length = 0;
    await waitForPrint(() => Promise.reject(new Error("gone")), sleep, () => t);
    expect(slept).toEqual([printWait.settleMs]);
  });
});

describe("pictureExtension", () => {
  it.each([
    ["image/png", "png"],
    ["image/jpeg; charset=binary", "jpg"],
    ["IMAGE/SVG+XML", "svg"],
    ["image/webp", "webp"],
    ["text/html", ""],
    ["", ""],
  ])("%s", (type, want) => expect(pictureExtension(type)).toBe(want));
});

describe("parseRunLink", () => {
  it("reads a failed run's notification and the run it opens", () =>
    expect(parseRunLink("blitz://run?notify=1&title=brief+failed&body=stopped+at+Save&dir=%2FUsers%2Fme%2Fshop&workflow=brief&run=flow-1")).toEqual({
      title: "brief failed",
      body: "stopped at Save",
      open: { dir: "/Users/me/shop", prompt: "", view: "workflows", workflow: "brief", run: "flow-1" },
    }));
  it("takes a Windows folder, and Blitz for a missing title", () => expect(parseRunLink("blitz://run?dir=C%3A%5Cshop")).toMatchObject({ title: "Blitz", open: { dir: "C:\\shop" } }));
  it.each(["blitz://run?dir=shop", "blitz://run", "blitz://tasks?dir=/x", "https://run?dir=/x", "nonsense"])("ignores %s", (raw) => expect(parseRunLink(raw)).toBeUndefined());
});
