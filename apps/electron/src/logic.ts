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

// What the Electron main process decides that needs no Electron, kept
// apart to be tested plainly: the host's link, which links leave the
// window, the macOS menu, and the checks on what the page asks for.

/** The calls the page makes to the window (the rest go to the host, /host/). */
export const windowMethods = [
  "ChooseWorkspace",
  "Notify",
  "OpenURL",
  "PrintPDF",
  "SetAppearance",
  "SetMenu",
  "SetUnsaved",
  "PendingLinks",
  "ToggleFullscreen",
] as const;
export type WindowMethod = (typeof windowMethods)[number];

/** The events the window sends the page. */
export const pageEvents = ["menu:about", "menu:settings", "deeplink:open", "notification:open"] as const;
export type PageEvent = (typeof pageEvents)[number];

/**
 * The link to the page in a line blitz-tray --serve wrote: a one-time
 * link on the loopback address ({"url": "http://127.0.0.1:<port>/open?t=<hex>"}),
 * or undefined for anything else.
 */
export function parseLink(line: string): string | undefined {
  let url: unknown;
  try {
    url = (JSON.parse(line) as { url?: unknown }).url;
  } catch {
    return undefined;
  }
  if (typeof url !== "string") return undefined;
  try {
    const u = new URL(url);
    if (u.protocol !== "http:" || u.hostname !== "127.0.0.1" || !u.port || u.pathname !== "/open" || !/^[0-9a-f]+$/.test(u.searchParams.get("t") ?? "")) return undefined;
  } catch {
    return undefined;
  }
  return url;
}

/** The origin of a URL ("" when it isn't one). */
export function originOf(url: string): string {
  try {
    return new URL(url).origin;
  } catch {
    return "";
  }
}

/**
 * Whether a link may open outside the window, in the system's browser or
 * mail program: web and mail links only, never file: or other schemes
 * (as the desktop app's SafeURL).
 */
export function isExternal(raw: string): boolean {
  let u: URL;
  try {
    u = new URL(raw.trim());
  } catch {
    return false;
  }
  switch (u.protocol) {
    case "http:":
    case "https:":
      return u.hostname !== "";
    case "mailto:":
      return u.pathname !== "";
  }
  return false;
}

/** The macOS app menu's words (the page sends them in its language). */
export interface MenuLabels {
  about: string;
  settings: string;
  hide: string;
  hide_others: string;
  show_all: string;
  quit: string;
}

export const englishMenu: MenuLabels = {
  about: "About Blitz",
  settings: "Settings…",
  hide: "Hide Blitz",
  hide_others: "Hide Others",
  show_all: "Show All",
  quit: "Quit Blitz",
};

/** The labels the page sent, English where one is missing or isn't text. */
export function menuLabels(v: unknown): MenuLabels {
  const o = (v && typeof v === "object" ? v : {}) as Record<string, unknown>;
  const out = { ...englishMenu };
  for (const k of Object.keys(englishMenu) as (keyof MenuLabels)[]) {
    const s = o[k];
    if (typeof s === "string" && s.trim() !== "") out[k] = s;
  }
  return out;
}

/** An item of the app menu: a page event, an Electron role, or a separator. */
export type MenuItem = { label: string; accelerator?: string; event?: PageEvent; role?: "hide" | "hideOthers" | "unhide" | "quit" } | "separator";

/** The macOS app menu (as the desktop app's appmenu.go); Edit and Window follow it. */
export function appMenu(l: MenuLabels): MenuItem[] {
  return [
    { label: l.about, event: "menu:about" },
    "separator",
    { label: l.settings, accelerator: "CmdOrCtrl+,", event: "menu:settings" },
    "separator",
    { label: l.hide, accelerator: "CmdOrCtrl+H", role: "hide" },
    { label: l.hide_others, accelerator: "CmdOrCtrl+Alt+H", role: "hideOthers" },
    { label: l.show_all, role: "unhide" },
    "separator",
    { label: l.quit, accelerator: "CmdOrCtrl+Q", role: "quit" },
  ];
}

/** What the page says about its editor's unsaved changes: message "" when there are none. */
export interface Unsaved {
  message: string;
  quit: string;
  cancel: string;
}

/** The unsaved changes the page sent (none, for anything that isn't). */
export function unsaved(v: unknown): Unsaved {
  const o = (v && typeof v === "object" ? v : {}) as Record<string, unknown>;
  const s = (k: string) => (typeof o[k] === "string" ? (o[k] as string) : "");
  const message = s("message");
  return message ? { message, quit: s("quit") || "Quit", cancel: s("cancel") || "Cancel" } : { message: "", quit: "", cancel: "" };
}

/** The themes the window takes (nativeTheme.themeSource). */
export function theme(v: unknown): "system" | "light" | "dark" {
  return v === "light" || v === "dark" ? v : "system";
}

/** 15 mm, in inches: the PDF's margins, as the desktop app prints them. */
const margin = 15 / 25.4;

/** printToPDF's options for A4 or Letter paper (A4 for anything else). */
export function printOptions(pageSize: unknown) {
  return {
    pageSize: pageSize === "Letter" ? ("Letter" as const) : ("A4" as const),
    margins: { top: margin, bottom: margin, left: margin, right: margin },
    printBackground: true,
  };
}

/** The longest prompt a blitz:// link may carry (as deeplink.go). */
export const maxLinkPrompt = 20000;

/** A blitz://open link's folder and prompt. */
export interface DeepLink {
  dir: string;
  prompt: string;
}

/**
 * Reads blitz://open?dir=<folder>&prompt=<text>: the folder must be an
 * absolute path (whether it's a folder is the main process's to check),
 * the prompt at most maxLinkPrompt characters. Undefined otherwise.
 */
export function parseDeepLink(raw: string, home = ""): DeepLink | undefined {
  let u: URL;
  try {
    u = new URL(raw.trim());
  } catch {
    return undefined;
  }
  if (u.protocol !== "blitz:") return undefined;
  const action = (u.host || u.pathname.replace(/^\/+/, "")).replace(/\/$/, "");
  if (action !== "open") return undefined;
  let dir = u.searchParams.get("dir") ?? "";
  if (home && (dir === "~" || dir.startsWith("~/"))) dir = home + dir.slice(1);
  const absolute = dir.startsWith("/") || /^[A-Za-z]:[\\/]/.test(dir);
  if (!absolute) return undefined;
  const prompt = u.searchParams.get("prompt") ?? "";
  if ([...prompt].length > maxLinkPrompt) return undefined;
  return { dir, prompt };
}

/** The blitz:// links among a command line's arguments. */
export const linkArgs = (argv: readonly string[]) => argv.filter((a) => a.startsWith("blitz:"));

/** Whether the page may call method on the window. */
export const isWindowMethod = (method: unknown): method is WindowMethod => typeof method === "string" && (windowMethods as readonly string[]).includes(method);

/** The i-th argument, as text ("" when it isn't). */
export const text = (args: readonly unknown[], i: number) => (typeof args[i] === "string" ? (args[i] as string) : "");
