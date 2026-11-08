/*
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
import { fakeBanner, fakePrompt, fakeTerminals } from "../dev/fakeTerminal";
import { bundledFont, fontFamily, isTermPath, pathLinks, plainColor, termColors, termId, termKey, termName, termPath } from "./terminals";

describe("terminal tabs", () => {
  it("are paths of their own", () => {
    expect(termPath("term-1")).toBe("term:term-1");
    expect(isTermPath("term:term-1")).toBe(true);
    expect(isTermPath("src/term.go")).toBe(false);
    expect(termId("term:term-1")).toBe("term-1");
  });

  it.each([
    { dir: "", workspace: "/Users/me/shop", want: "shop" },
    { dir: "src/api", workspace: "/Users/me/shop", want: "api" },
    { dir: "", workspace: "/", want: "/" },
  ])("are named after their folder: $dir in $workspace", ({ dir, workspace, want }) => {
    expect(termName(dir, workspace)).toBe(want);
  });
});

describe("the terminal's font", () => {
  it.each([
    { chosen: "", want: `"${bundledFont}", monospace` },
    { chosen: "Menlo", want: `Menlo, "${bundledFont}", monospace` },
    { chosen: "JetBrainsMono Nerd Font", want: `"JetBrainsMono Nerd Font", "${bundledFont}", monospace` },
    { chosen: bundledFont, want: `"${bundledFont}", monospace` },
    { chosen: 'Evil", serif', want: `"Evil, serif", "${bundledFont}", monospace` },
  ])("falls back to the bundled one: $chosen", ({ chosen, want }) => {
    expect(fontFamily(chosen)).toBe(want);
  });
});

describe("the terminal's colours", () => {
  it("take the window's text and accent over a clear background, and a palette for each theme", () => {
    const tokens: Record<string, string> = { "--md-surface-container-lowest": "rgb(29 31 35 / 0.9)", "--md-on-surface": " #eeeeee", "--md-primary": "rgb(1 2 3 / 50%)" };
    const dark = termColors((n) => tokens[n], true);
    expect(dark.background, "glass made solid").toBe("rgb(29, 31, 35)");
    expect(dark.foreground).toBe("#eeeeee");
    expect(dark.cursor).toBe("rgba(1, 2, 3, 0.5)");
    expect(dark.cursorAccent).toBe(dark.background);
    expect(dark.red).toBe("#e06c75");
    const light = termColors(() => "color-mix(in srgb, red, blue)", false);
    expect(light.background).toBe("#ffffff");
    expect(light.foreground).toBe("#383a42");
    expect(light.red).toBe("#e45649");
  });

  it.each([
    { v: "#abc", want: "#abc" },
    { v: "rgb(255 255 255 / 0.9)", want: "rgba(255, 255, 255, 0.9)" },
    { v: "rgba(1, 2, 3, 1)", want: "rgb(1, 2, 3)" },
    { v: "rgb(10, 20, 30)", want: "rgb(10, 20, 30)" },
    { v: "var(--x)", want: "fallback" },
    { v: undefined, want: "fallback" },
  ])("reads $v as $want", ({ v, want }) => {
    expect(plainColor(v, "fallback")).toBe(want);
  });
});

describe("paths the shell printed", () => {
  it.each([
    { text: "src/main.go:42:7: undefined: x", want: [{ path: "src/main.go", line: 42, column: 7, start: 0, end: 16 }] },
    { text: "  modified:   apps/web/App.tsx", want: [{ path: "apps/web/App.tsx", line: undefined, column: undefined, start: 14, end: 30 }] },
    { text: "see ./README.md for more", want: [{ path: "README.md", line: undefined, column: undefined, start: 4, end: 15 }] },
    { text: "go to https://example.com/a/b.html now", want: [] },
    { text: "nothing here at all", want: [] },
    { text: "cd ..", want: [] },
  ])("$text", ({ text, want }) => {
    expect(pathLinks(text)).toEqual(want);
  });
});

describe("the terminal's keys", () => {
  const key = (k: string, mods: Partial<{ metaKey: boolean; ctrlKey: boolean; shiftKey: boolean }>, type = "keydown") => ({ key: k, metaKey: false, ctrlKey: false, shiftKey: false, type, ...mods });
  it.each([
    { e: key("c", { metaKey: true }), mac: true, want: "copy" },
    { e: key("v", { metaKey: true }), mac: true, want: "paste" },
    { e: key("f", { metaKey: true }), mac: true, want: "find" },
    { e: key("c", { ctrlKey: true }), mac: true, want: null }, // the shell's interrupt
    { e: key("C", { ctrlKey: true, shiftKey: true }), mac: false, want: "copy" },
    { e: key("V", { ctrlKey: true, shiftKey: true }), mac: false, want: "paste" },
    { e: key("F", { ctrlKey: true, shiftKey: true }), mac: false, want: "find" },
    { e: key("c", { ctrlKey: true }), mac: false, want: null },
    { e: key("c", { metaKey: true }, "keyup"), mac: true, want: null },
    { e: key("x", { metaKey: true }), mac: true, want: null },
  ])("$e.key on mac=$mac: $want", ({ e, mac, want }) => {
    expect(termKey(e, mac)).toBe(want);
  });
});

describe("the fake service's terminal", () => {
  const read = async (it: AsyncGenerator<{ data: Uint8Array }>) => new TextDecoder().decode((await it.next()).value!.data);

  it("draws a powerline prompt and echoes a line", async () => {
    const f = fakeTerminals();
    const { terminal } = f.openTerminal({ dir: "src" });
    expect(terminal.dir).toBe("src");
    expect(f.listTerminals().terminals.map((x) => x.id)).toEqual([terminal.id]);
    const abort = new AbortController();
    const stream = f.watchTerminal({ id: terminal.id }, { signal: abort.signal });
    const first = await read(stream);
    expect(first).toContain(fakeBanner());
    expect(first).toContain(""); // the powerline separator
    expect(first).toContain(fakePrompt("src"));

    f.writeTerminal({ id: terminal.id, data: new TextEncoder().encode("ls") });
    expect(await read(stream)).toBe("l");
    expect(await read(stream)).toBe("s");
    f.writeTerminal({ id: terminal.id, data: new TextEncoder().encode("\x7f\r") });
    expect(await read(stream)).toBe("\b \b");
    expect(await read(stream)).toContain("l: fake shell");

    expect(f.resizeTerminal({ id: terminal.id, cols: 80, rows: 24 })).toEqual({});
    expect(() => f.resizeTerminal({ id: terminal.id, cols: 0, rows: 24 })).toThrow();
    f.closeTerminal({ id: terminal.id });
    expect((await stream.next()).done).toBe(true);
    expect(f.listTerminals().terminals).toEqual([]);
    expect(() => f.writeTerminal({ id: terminal.id, data: new Uint8Array() })).toThrow();
    expect(() => f.openTerminal({ dir: "../elsewhere" })).toThrow();
    abort.abort();
  });
});
