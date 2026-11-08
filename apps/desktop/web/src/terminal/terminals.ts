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

// The window's terminals (spec_062_terminal): a terminal's tab is a path
// of its own (term:<id>), like a web page's, and what it draws is worked
// out here, apart from xterm.js, so it can be tested.

/** The bundled font, with the system's monospace after it. */
export const bundledFont = "MesloLGS Nerd Font Mono";

/** A terminal's tab path. */
export const termPath = (id: string) => `term:${id}`;

/** Whether a tab is a terminal's. */
export const isTermPath = (path: string) => path.startsWith("term:");

/** The terminal a tab path names. */
export const termId = (path: string) => path.slice("term:".length);

/** A terminal tab's name: its folder's (the workspace's for ""). */
export function termName(dir: string, workspace: string): string {
  const parts = (dir || workspace).split("/").filter(Boolean);
  return parts[parts.length - 1] || "/";
}

/** The font family xterm draws with: the chosen one, then the bundled one, then any monospace. */
export function fontFamily(chosen: string): string {
  const quote = (f: string) => (/^[\w-]+$/.test(f) ? f : `"${f.replace(/"/g, "")}"`);
  const fams = [chosen.trim(), bundledFont].filter((f, i, all) => f && all.indexOf(f) === i);
  return [...fams.map(quote), "monospace"].join(", ");
}

/** xterm's colours: the window's for the surface, a palette for the 16 ANSI colours (TRM-23). */
export interface TermColors {
  background: string;
  foreground: string;
  cursor: string;
  cursorAccent: string;
  selectionBackground: string;
  black: string;
  red: string;
  green: string;
  yellow: string;
  blue: string;
  magenta: string;
  cyan: string;
  white: string;
  brightBlack: string;
  brightRed: string;
  brightGreen: string;
  brightYellow: string;
  brightBlue: string;
  brightMagenta: string;
  brightCyan: string;
  brightWhite: string;
}

// One Light's and One Dark's ANSI colours: Blitz's themes are theirs.
const ansiLight = {
  black: "#383a42", red: "#e45649", green: "#50a14f", yellow: "#c18401", blue: "#4078f2", magenta: "#a626a4", cyan: "#0184bc", white: "#a0a1a7",
  brightBlack: "#696c77", brightRed: "#ca1243", brightGreen: "#3e953a", brightYellow: "#986801", brightBlue: "#2f5af3", brightMagenta: "#9a1d98", brightCyan: "#0997b3", brightWhite: "#fafafa",
};
const ansiDark = {
  black: "#3f4451", red: "#e06c75", green: "#98c379", yellow: "#e5c07b", blue: "#61afef", magenta: "#c678dd", cyan: "#56b6c2", white: "#d7dae0",
  brightBlack: "#5c6370", brightRed: "#ef7e86", brightGreen: "#a5d189", brightYellow: "#ebcb8b", brightBlue: "#74baf3", brightMagenta: "#d38fe6", brightCyan: "#6fc4cf", brightWhite: "#ffffff",
};

/**
 * A token's value as a colour xterm reads: hex, or rgb()/rgba() with
 * commas (a theme's `rgb(255 255 255 / 0.9)` made so); else the fallback.
 */
export function plainColor(v: string | undefined, fallback: string): string {
  const s = (v ?? "").trim();
  if (/^#[0-9a-f]{3,8}$/i.test(s)) return s;
  const m = /^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)\s*(?:[,/]\s*([\d.]+%?))?\s*\)$/i.exec(s);
  if (!m) return fallback;
  const a = m[4] === undefined ? 1 : m[4].endsWith("%") ? Number(m[4].slice(0, -1)) / 100 : Number(m[4]);
  return a >= 1 ? `rgb(${m[1]}, ${m[2]}, ${m[3]})` : `rgba(${m[1]}, ${m[2]}, ${m[3]}, ${a})`;
}

/** A colour without its transparency: xterm blends dim text with its background, which must be solid. */
export const opaque = (c: string) => c.replace(/^rgba\(([^,]+),([^,]+),([^,]+),[^)]*\)$/, "rgb($1,$2,$3)");

/**
 * The terminal's colours from the window's tokens (read with token) in
 * light or dark: the tab's surface (made solid), its text and accent, and
 * a palette for the 16 ANSI colours.
 */
export function termColors(token: (name: string) => string | undefined, dark: boolean): TermColors {
  const bg = opaque(plainColor(token("--md-surface-container-lowest"), dark ? "#1d1f23" : "#ffffff"));
  const fg = plainColor(token("--md-on-surface"), dark ? "#abb2bf" : "#383a42");
  const cursor = plainColor(token("--md-primary"), dark ? "#61afef" : "#4078f2");
  return { background: bg, foreground: fg, cursor, cursorAccent: bg, selectionBackground: dark ? "#3e4451" : "#d7dce6", ...(dark ? ansiDark : ansiLight) };
}

/** A path the shell printed, as a place in a file: src/main.go:42:7. */
export interface PathLink {
  path: string;
  line?: number;
  column?: number;
  /** Where it is in the line (characters). */
  start: number;
  end: number;
}

// A relative path with a folder or an extension, and a line and column after it.
const pathIn = /(?:^|[\s'"(\[])((?:\.\/)?[\w@.-]+(?:\/[\w@.-]+)*\.[\w]+|(?:\.\/)?[\w@.-]+(?:\/[\w@.-]+)+)(?::(\d+))?(?::(\d+))?/g;

/** The paths in a line of output that may name a file in the workspace (TRM-21). */
export function pathLinks(text: string): PathLink[] {
  const out: PathLink[] = [];
  for (const m of text.matchAll(pathIn)) {
    const raw = m[1];
    if (/^\.+$/.test(raw) || /:\/\//.test(text.slice(m.index ?? 0, (m.index ?? 0) + m[0].length + 3))) continue; // dots, or a URL's
    const start = (m.index ?? 0) + m[0].indexOf(raw);
    const end = start + m[0].length - m[0].indexOf(raw);
    out.push({ path: raw.replace(/^\.\//, ""), line: m[2] ? Number(m[2]) : undefined, column: m[3] ? Number(m[3]) : undefined, start, end });
  }
  return out;
}

/** What a key does in a terminal beyond xterm's own: copy, paste, find (TRM-24). */
export function termKey(e: { key: string; metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; type: string }, mac: boolean): "copy" | "paste" | "find" | null {
  if (e.type !== "keydown") return null;
  const k = e.key.toLowerCase();
  const mod = mac ? e.metaKey && !e.ctrlKey : e.ctrlKey && e.shiftKey && !e.metaKey;
  if (!mod) return null;
  if (k === "c") return "copy";
  if (k === "v") return "paste";
  if (k === "f") return "find";
  return null;
}
