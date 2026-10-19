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
  "Quit",
  // The tasks window's: opens the app's own window (EL-110).
  "OpenApp",
  // The web preview (EL-100).
  "PreviewOpen",
  "PreviewBounds",
  "PreviewVisible",
  "PreviewNavigate",
  "PreviewClose",
  "PreviewCapture",
  "PreviewContent",
  "PreviewConsole",
  "PreviewPrintPDF",
  "PreviewPicture",
] as const;
export type WindowMethod = (typeof windowMethods)[number];

/** What the tasks window may call (EL-110): it has no editor, menu or previews. */
export const taskWindowMethods: readonly WindowMethod[] = ["OpenURL", "SetAppearance", "OpenApp"];

/** The events the window sends the page. */
export const pageEvents = ["menu:about", "menu:settings", "deeplink:open", "notification:open", "preview:state", "tasks:open"] as const;
export type PageEvent = (typeof pageEvents)[number];

/** A link to the page, and the fingerprint of the certificate its server has. */
export interface PageLink {
  url: string;
  fingerprint: string;
}

/**
 * The link to the page in a line blitz-tray --serve wrote: a one-time
 * link on the loopback address, over HTTPS, with its server's certificate
 * ({"url": "https://127.0.0.1:<port>/open?t=<hex>", "fingerprint": "AB:…"},
 * SHA-256 as colon-separated hex), or undefined for anything else.
 */
export function parseLink(line: string): PageLink | undefined {
  let url: unknown, fingerprint: unknown;
  try {
    ({ url, fingerprint } = JSON.parse(line) as { url?: unknown; fingerprint?: unknown });
  } catch {
    return undefined;
  }
  if (typeof url !== "string" || typeof fingerprint !== "string" || !/^([0-9A-F]{2}:){31}[0-9A-F]{2}$/.test(fingerprint)) return undefined;
  try {
    const u = new URL(url);
    if (u.protocol !== "https:" || u.hostname !== "127.0.0.1" || !u.port || u.pathname !== "/open" || !/^[0-9a-f]+$/.test(u.searchParams.get("t") ?? "")) return undefined;
  } catch {
    return undefined;
  }
  return { url, fingerprint };
}

/**
 * Whether a certificate is the page server's: on the loopback address,
 * with the fingerprint blitz-tray --serve named (pinned). The window
 * trusts that one certificate, made by the server as it started, and
 * leaves every other to Chromium's own checks.
 */
export function isPageCertificate(hostname: string, fingerprint: string, pinned: string): boolean {
  return pinned !== "" && hostname === "127.0.0.1" && fingerprint === pinned;
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

/** A blitz:// link's folder, and its prompt (open) or view (workers, or a workflow's run). */
export interface DeepLink {
  dir: string;
  prompt: string;
  view?: "workers" | "workflows";
  /** For workflows: the workflow, and the run to show on its canvas. */
  workflow?: string;
  run?: string;
}

/**
 * Reads blitz://open?dir=<folder>&prompt=<text> and, from the tray,
 * blitz://workers?dir=<folder> (the folder's workers): the folder must be
 * an absolute path (whether it's a folder is the main process's to
 * check), the prompt at most maxLinkPrompt characters. Undefined
 * otherwise.
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
  if (action !== "open" && action !== "workers") return undefined;
  let dir = u.searchParams.get("dir") ?? "";
  if (home && (dir === "~" || dir.startsWith("~/"))) dir = home + dir.slice(1);
  const absolute = dir.startsWith("/") || /^[A-Za-z]:[\\/]/.test(dir);
  if (!absolute) return undefined;
  if (action === "workers") return { dir, prompt: "", view: "workers" };
  const prompt = u.searchParams.get("prompt") ?? "";
  if ([...prompt].length > maxLinkPrompt) return undefined;
  return { dir, prompt };
}

/**
 * What blitz://tasks asks (EL-110), from the tray when a workflow's human
 * task waits: to notify first (notify=1, with the notification's title
 * and body in the user's language) or open the tasks window at once.
 * Undefined for any other link.
 */
export interface TasksLink {
  notify: boolean;
  title: string;
  body: string;
}

export function parseTasksLink(raw: string): TasksLink | undefined {
  let u: URL;
  try {
    u = new URL(raw.trim());
  } catch {
    return undefined;
  }
  const action = (u.host || u.pathname.replace(/^\/+/, "")).replace(/\/$/, "");
  if (u.protocol !== "blitz:" || action !== "tasks") return undefined;
  const cut = (s: string | null) => [...(s ?? "")].slice(0, 200).join("");
  return { notify: u.searchParams.get("notify") === "1", title: cut(u.searchParams.get("title")) || "Blitz", body: cut(u.searchParams.get("body")) };
}

/**
 * What blitz://run says (spec_workflows_043 WF-19), from the tray when a
 * workflow run fails with the app's window closed: the notification's
 * title and body (in the user's language), and the run to open when it's
 * clicked (its workspace, an absolute folder; its workflow; its ID).
 * Undefined for any other link, or one without a folder.
 */
export interface RunLink {
  title: string;
  body: string;
  open: DeepLink;
}

export function parseRunLink(raw: string): RunLink | undefined {
  let u: URL;
  try {
    u = new URL(raw.trim());
  } catch {
    return undefined;
  }
  const action = (u.host || u.pathname.replace(/^\/+/, "")).replace(/\/$/, "");
  if (u.protocol !== "blitz:" || action !== "run") return undefined;
  const dir = u.searchParams.get("dir") ?? "";
  if (!(dir.startsWith("/") || /^[A-Za-z]:[\\/]/.test(dir))) return undefined;
  const cut = (s: string | null) => [...(s ?? "")].slice(0, 200).join("");
  return {
    title: cut(u.searchParams.get("title")) || "Blitz",
    body: cut(u.searchParams.get("body")),
    open: { dir, prompt: "", view: "workflows", workflow: cut(u.searchParams.get("workflow")), run: cut(u.searchParams.get("run")) },
  };
}

/** The page's one-time link, opening on the tasks window (the page server keeps tasks). */
export const tasksURL = (link: string) => link + (link.includes("?") ? "&" : "?") + "tasks";

/** The blitz:// links among a command line's arguments. */
export const linkArgs = (argv: readonly string[]) => argv.filter((a) => a.startsWith("blitz:"));

/** Whether the page may call method on the window. */
export const isWindowMethod = (method: unknown): method is WindowMethod => typeof method === "string" && (windowMethods as readonly string[]).includes(method);

/** The i-th argument, as text ("" when it isn't). */
export const text = (args: readonly unknown[], i: number) => (typeof args[i] === "string" ? (args[i] as string) : "");

/**
 * The address a web preview opens (EL-101): http and https only, never
 * file: or other schemes. One without a scheme gets http:// for this
 * machine (localhost:3000) and https:// elsewhere; undefined for anything
 * that isn't a web address.
 */
export function previewURL(raw: unknown): string | undefined {
  if (typeof raw !== "string") return undefined;
  let s = raw.trim();
  if (!s) return undefined;
  // A scheme (http:, file:, javascript:), not a host and its port (localhost:3000).
  if (!/^[a-z][a-z0-9+.-]*:(?!\d)/i.test(s)) {
    const host = s.startsWith("[") ? s.slice(0, s.indexOf("]") + 1) : (s.split(/[/:?#]/)[0] ?? "");
    s = (isLocalHost(host) ? "http://" : "https://") + s;
  }
  let u: URL;
  try {
    u = new URL(s);
  } catch {
    return undefined;
  }
  if ((u.protocol !== "http:" && u.protocol !== "https:") || !u.hostname) return undefined;
  return u.href;
}

/** Where a typed search goes (EL-101a): Google, with the words as its query. */
export const searchURL = (words: string) => `https://www.google.com/search?q=${encodeURIComponent(words)}`;

/**
 * What the address bar opens (EL-101a): a web address as previewURL takes
 * it, else a Google search for the words. Words are anything with a space,
 * or a bare word that isn't a host (no dot, no port, not this machine's,
 * not an IPv6 address).
 * Other schemes (file:, javascript:) are still refused, not searched.
 */
export function addressURL(raw: unknown): string | undefined {
  if (typeof raw !== "string") return undefined;
  const s = raw.trim();
  if (!s) return undefined;
  if (/\s/.test(s)) return searchURL(s);
  if (/^[a-z][a-z0-9+.-]*:(?!\d)/i.test(s)) return previewURL(s);
  const host = s.split(/[/?#]/)[0] ?? "";
  if (!s.startsWith("[") && !host.includes(".") && !/:\d+$/.test(host) && !isLocalHost(host)) return searchURL(s);
  return previewURL(s) ?? searchURL(s);
}

/** Whether a host name is this machine's (localhost, 127.0.0.1, [::1]). */
export function isLocalHost(host: string): boolean {
  const h = host.toLowerCase();
  return h === "localhost" || h.endsWith(".localhost") || h === "127.0.0.1" || h === "[::1]" || h === "::1";
}

/** A preview's ID from the page: letters, digits, - and _, up to 64. */
export const previewId = (v: unknown): string | undefined => (typeof v === "string" && /^[\w-]{1,64}$/.test(v) ? v : undefined);

/** Where a preview goes in the window, in the page's pixels. */
export interface Bounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** The bounds the page sent, whole and not negative; undefined when they aren't bounds. */
export function bounds(v: unknown): Bounds | undefined {
  if (typeof v !== "object" || v === null) return undefined;
  const o = v as Record<string, unknown>;
  const out: Record<string, number> = {};
  for (const k of ["x", "y", "width", "height"]) {
    const n = o[k];
    if (typeof n !== "number" || !Number.isFinite(n)) return undefined;
    out[k] = Math.min(Math.max(0, Math.round(n)), 100_000);
  }
  return out as unknown as Bounds;
}

/** What the preview's toolbar asks of it (an address opens with PreviewOpen). */
export type PreviewStep = "back" | "forward" | "reload" | "stop";

/** A step the page asked for; undefined for one it can't ask for. */
export const previewStep = (v: unknown): PreviewStep | undefined => (v === "back" || v === "forward" || v === "reload" || v === "stop" ? v : undefined);

/** The console errors a preview keeps, the latest. */
export const maxConsoleLines = 200;

/**
 * A preview's console message as kept for Add to chat (EL-104): errors
 * only, with where they came from; undefined for the rest.
 */
export function consoleLine(level: string, message: string, source: string, line: number): string | undefined {
  if (level !== "error") return undefined;
  const at = source ? ` (${source}${line ? `:${line}` : ""})` : "";
  return message.slice(0, 4000) + at;
}

/** Adds a line to the kept ones, dropping the oldest past maxConsoleLines. */
export function keepLine(lines: string[], line: string): void {
  lines.push(line);
  if (lines.length > maxConsoleLines) lines.splice(0, lines.length - maxConsoleLines);
}

/** The largest page or selection Add to chat takes, as HTML (the service's limit too). */
export const maxContentHTML = 2 << 20;

/**
 * What Add to chat reads from a preview (EL-104), run in a world of its own
 * so the page's scripts can't change what it sees: the address, the title,
 * the page's HTML and the selection's, each cut to maxContentHTML.
 */
export const contentScript = `(() => {
  const max = ${maxContentHTML};
  const cut = (s) => (s.length > max ? s.slice(0, max) : s);
  const sel = window.getSelection();
  let selection = "";
  if (sel && sel.rangeCount && !sel.isCollapsed) {
    const box = document.createElement("div");
    for (let i = 0; i < sel.rangeCount; i++) box.appendChild(sel.getRangeAt(i).cloneContents());
    selection = box.innerHTML;
  }
  const root = document.documentElement;
  return { url: location.href, title: document.title, html: cut(root ? root.outerHTML : ""), selection: cut(selection) };
})()`;

/** What PreviewContent answers: the page's address, title, HTML and selection. */
export interface PreviewContent {
  url: string;
  title: string;
  html: string;
  selection: string;
}

/** The content script's answer, as strings; empty where it gave none. */
export function previewContent(v: unknown): PreviewContent {
  const o = (typeof v === "object" && v !== null ? v : {}) as Record<string, unknown>;
  const str = (k: string) => (typeof o[k] === "string" ? (o[k] as string).slice(0, maxContentHTML) : "");
  return { url: str("url"), title: str("title"), html: str("html"), selection: str("selection") };
}

/**
 * Whether the window draws without the graphics card (EL-07): BLITZ_GPU=off,
 * or the gpu preference off in desktop.json (Settings › Appearance › Use
 * the graphics card, on Linux), read before Electron starts.
 */
export function gpuOff(env: Record<string, string | undefined>, prefs: string | undefined): boolean {
  if (env.BLITZ_GPU === "off") return true;
  try {
    return (JSON.parse(prefs ?? "{}") as { gpu?: unknown }).gpu === "off";
  } catch {
    return false;
  }
}

/**
 * Run in the printed page once it has loaded, in a world of its own (the
 * page's scripts are off, and so are timers, so the main process does the
 * waiting): makes every font the page declares load (layout may not have
 * asked for them yet) and every picture decode. Printing straight away
 * printed pictures as blank space while a font was still loading (KaTeX's,
 * inlined with font-display: block).
 */
export const printLoadScript = `(() => {
  for (const f of Array.from(document.fonts)) f.load().catch(() => {});
  for (const i of Array.from(document.images)) i.decode().catch(() => {});
  return true;
})()`;

/** Whether the printed page's fonts and pictures are ready to draw (laying it out, so they're drawn next). */
export const printCheckScript = `(() => {
  const ready = document.fonts.status === "loaded" && Array.from(document.images).every((i) => i.complete);
  if (ready && document.body) document.body.getBoundingClientRect();
  return ready;
})()`;

/** How long printing waits for fonts and pictures at most, and how often it looks. */
export const printWait = { deadlineMs: 10_000, everyMs: 50, settleMs: 150 };

/**
 * Waits until check says the page is ready, or the deadline: check is
 * asked every printWait.everyMs (a failed check counts as ready: printing
 * goes ahead), then the page has printWait.settleMs to paint.
 */
export async function waitForPrint(check: () => Promise<unknown>, sleep = (ms: number) => new Promise((r) => setTimeout(r, ms)), now = Date.now): Promise<void> {
  const deadline = now() + printWait.deadlineMs;
  while (now() < deadline) {
    const ready = await check().catch(() => true);
    if (ready === true) break;
    await sleep(printWait.everyMs);
  }
  await sleep(printWait.settleMs);
}

/** The largest picture Save to workspace downloads (EL-106). */
export const maxPictureBytes = 10 << 20;

/** A picture's file extension by its type ("" for what isn't a picture Save to workspace keeps). */
export function pictureExtension(contentType: string): string {
  const type = contentType.split(";")[0].trim().toLowerCase();
  return { "image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp", "image/svg+xml": "svg", "image/avif": "avif", "image/x-icon": "ico", "image/vnd.microsoft.icon": "ico", "image/bmp": "bmp" }[type] ?? "";
}
