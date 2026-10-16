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
import { appMenu, englishMenu, isExternal, isWindowMethod, linkArgs, maxLinkPrompt, menuLabels, originOf, parseDeepLink, parseLink, printOptions, text, theme, unsaved } from "./logic";

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
