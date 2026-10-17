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

// The desktop app's own functions, for what the service can't do: its own
// status and installation, native dialogs, the window's settings, and
// opening links outside the window. The Electron app's window answers
// those only a window can (window.blitzElectron, its preload), and the
// program serving the page the rest, under /host/ (blitz-tray --serve,
// pkg/pagehost; the Windows tray's page in a browser too). In a plain
// browser (development), and for what neither does, they fall back to
// stand-ins: settings in localStorage, links in a new tab.
import { workspaces } from "./api";
import { editorConfig, toEditor } from "./host";
import { t } from "./i18n";
import { defaultPrefs, normalizePrefs, type Prefs } from "./prefs";

/**
 * Whether the service answers, whether its login item is installed, where
 * its socket is, and which blitzd this app would install.
 */
export interface ServiceStatus {
  running: boolean;
  installed: boolean;
  socket: string;
  service: string; // the blitzd this app installs and restarts ("" if not found)
  tray?: string; // the blitz-tray beside the app ("" if not found)
  tray_installed?: boolean; // it starts at login
}

/** Where the blitz command is: the one that comes with the app, and the one a terminal runs. */
export interface CLIStatus {
  cli: string; // the blitz beside the app ("" if not found)
  on_path: string; // the blitz on the user's PATH ("" if none)
  installed: boolean; // on_path is the app's
  method?: "link" | "path"; // a link on PATH (macOS, Linux), or its folder on the user's PATH (Windows)
}

/** What installing the CLI did: the link, and the startup file it added its directory to PATH in ("" if none). */
export interface CLIInstall {
  link: string;
  profile: string;
  removed?: string[]; // the dead blitz links a reinstall took off PATH
}

/** Whether the OS sandbox for the agent's commands works here (Linux; "unsupported" elsewhere). */
export interface SandboxStatus {
  state: "unsupported" | "ready" | "no_bwrap" | "restricted" | "broken";
  bwrap?: string;
  detail?: string;
  profile?: string;
}

/** What fixing the sandbox did: its state after, and if it failed, why and the commands to run by hand. */
export interface SandboxFix {
  status: SandboxStatus;
  error?: string;
  commands?: string;
}

/** The app's methods, by name: the window's and its host's. */
type AppMethods = {
  Version(): Promise<string>;
  ProgramExists(path: string): Promise<boolean>;
  ServiceStatus(): Promise<ServiceStatus>;
  InstallService(): Promise<void>;
  SetTray(on: boolean): Promise<void>;
  CLIStatus(): Promise<CLIStatus>;
  SandboxStatus(): Promise<SandboxStatus>;
  FixSandbox(): Promise<SandboxFix>;
  InstallCLI(): Promise<CLIInstall>;
  ReinstallCLI(): Promise<CLIInstall>;
  StopService(pid: number): Promise<void>;
  RestartService(pid: number): Promise<void>;
  ChooseWorkspace(title: string): Promise<string>;
  GetPrefs(): Promise<Prefs>;
  SavePrefs(p: Prefs): Promise<Prefs>;
  OpenURL(url: string): Promise<void>;
  OpenFolder(dir: string): Promise<void>;
  OpenDocument(path: string): Promise<void>;
  RevealPath(path: string): Promise<void>;
  FileManager(): Promise<string>;
  Notify(title: string, body: string, dir: string): Promise<void>;
  SetUnsaved(u: { message: string; quit: string; cancel: string }): Promise<void>;
  SetAppearance(theme: string): Promise<void>;
  SetMenu(l: AppMenuLabels): Promise<void>;
  License(which: string): Promise<string>;
  PendingLinks(): Promise<DeepLink[] | null>;
  PrintPDF(html: string, pageSize: string): Promise<string>;
  CheckUpdate(force?: boolean): Promise<UpdateStatus>;
  UpdateStatus(): Promise<UpdateStatus>;
  DownloadUpdate(): Promise<UpdateStatus>;
  ApplyUpdate(): Promise<void>;
  Quit(): Promise<void>;
  PreviewOpen(id: string, url: string): Promise<string>;
  PreviewBounds(id: string, b: PreviewBounds): Promise<void>;
  PreviewVisible(id: string, visible: boolean): Promise<void>;
  PreviewNavigate(id: string, step: string): Promise<void>;
  PreviewClose(id: string): Promise<void>;
  PreviewCapture(id: string): Promise<string>;
  PreviewContent(id: string): Promise<PreviewContent>;
  PreviewConsole(id: string): Promise<string[]>;
  PreviewPrintPDF(id: string, pageSize: string): Promise<string>;
};

/** Where a web preview goes in the window, in the page's pixels. */
export interface PreviewBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** A web preview's page, as the window says when it changes (preview:state). */
export interface PreviewState {
  id: string;
  url: string;
  title: string;
  loading: boolean;
  back: boolean;
  forward: boolean;
}

/** What Add to chat reads from a web preview: its address, title, HTML and selection (HTML). */
export interface PreviewContent {
  url: string;
  title: string;
  html: string;
  selection: string;
}

/** The desktop app's update, as its host says (spec_electron_038 EL-90). */
export interface UpdateStatus {
  current: string;
  latest: string; // the latest release's tag
  available: boolean;
  can: boolean; // the app updates itself here (else reason says why not)
  reason?: string;
  downloading: boolean;
  ready: boolean; // downloaded, checked, unpacked: restart to update
  error?: string;
  notes?: string; // the release's page
}

/** A blitz://open link: a folder to open, with a prompt to fill in. */
export interface DeepLink {
  dir: string;
  prompt: string;
}

/** The methods the program serving the page answers (the tray's), once asked. */
let hostMethods: ReadonlySet<string> = new Set();

/**
 * Asks the program serving the page which of the app's methods it answers
 * (GET /host/info). Call once before rendering; outside the tray's page
 * nothing answers, and none are.
 */
export async function detectHost(): Promise<void> {
  if (editorConfig()) return;
  try {
    const res = await fetch("/host/info", { headers: { Accept: "application/json" } });
    const info: unknown = res.ok ? await res.json() : undefined;
    const methods = (info as { methods?: unknown } | undefined)?.methods;
    if (Array.isArray(methods)) hostMethods = new Set(methods.filter((m): m is string => typeof m === "string"));
  } catch {
    // no host: a development server's page, or a file
  }
}

/** Forgets the host (tests). */
export const resetHost = () => (hostMethods = new Set());

/** Calls one of the app's methods on the program serving the page. */
async function hostCall(method: string, args: unknown[]): Promise<unknown> {
  const res = await fetch(`/host/${method}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(args),
  });
  const body: unknown = await res.json().catch(() => undefined);
  if (!res.ok) throw new Error((body as { error?: string } | undefined)?.error ?? res.statusText);
  return body;
}

/**
 * What the Electron app's window answers (window.blitzElectron, from its
 * preload, spec_electron_038): the calls only a window can make, and its
 * events. The rest of the app's methods are its host's (/host/).
 */
type ElectronWindow = {
  methods: readonly string[];
  nativeGlass: boolean;
  call(method: string, ...args: unknown[]): Promise<unknown>;
  on(event: string, f: (arg: unknown) => void): () => void;
};

function electronWindow(): ElectronWindow | undefined {
  return (window as unknown as { blitzElectron?: ElectronWindow }).blitzElectron;
}

/** Whether the page runs in the Electron app's window. */
export const inElectron = () => !!electronWindow();

/** Whether the Electron app's window answers method. */
const windowHas = (method: string) => !!electronWindow()?.methods.includes(method);

/**
 * Whether the app's method can be called: by the Electron app's window,
 * else by the program serving the page (the Electron app's host, or the
 * tray).
 */
export const canCall = (method: keyof AppMethods) => windowHas(method) || hostMethods.has(method);

/**
 * What the Electron window said went wrong, without what Electron adds
 * ("Error invoking remote method 'blitz:call': Error: ").
 */
export function windowError(e: unknown): string {
  const m = e instanceof Error ? e.message : String(e);
  return m.replace(/^Error invoking remote method '[^']*': (?:Error: )?/, "");
}

/** The app's methods: the Electron window's, then the host's. */
const served = new Proxy({} as AppMethods, {
  get: (_, method: string) => {
    const w = electronWindow();
    if (w && windowHas(method)) return (...args: unknown[]) => w.call(method, ...args).catch((e: unknown) => Promise.reject(new Error(windowError(e))));
    if (hostMethods.has(method)) return (...args: unknown[]) => hostCall(method, args);
    return () => Promise.reject(new Error("not running inside the Blitz desktop app"));
  },
});

function app(): AppMethods {
  if (inElectron() || hostMethods.size > 0) return served;
  throw new Error("not running inside the Blitz desktop app");
}

/**
 * Listens to the window's events (its menu, links, notifications, web
 * previews): the Electron window's; undefined in a browser, which has none.
 */
function windowEvents(): ((name: string, f: (arg: unknown) => void) => () => void) | undefined {
  const w = electronWindow();
  if (w) return (name, f) => w.on(name, f);
  return undefined;
}

/**
 * Whether the window is macOS's translucent one, the page's glass drawn
 * over the desktop showing through (Electron's).
 */
export function nativeGlass(): boolean {
  if (typeof window === "undefined" || typeof navigator === "undefined") return false;
  return !!electronWindow()?.nativeGlass;
}

const devPrefsKey = "blitz.desktop.prefs";

/** The service's status (in a browser: whether the dev server's proxy reaches one). */
export async function serviceStatus(): Promise<ServiceStatus> {
  if (!canCall("ServiceStatus")) {
    // In a browser the dev server proxies to the service: ask it something.
    const running = await workspaces.listWorkspaces({}).then(
      () => true,
      () => false,
    );
    return { running, installed: true, socket: "(development server)", service: "" };
  }
  return app().ServiceStatus();
}
/** Installs this app's blitzd as the login item and starts it. */
export const installService = () => app().InstallService();

/** Shows the service in the system tray at login and now, or stops that. */
export const setTray = (on: boolean) => app().SetTray(on);
/** The OS sandbox's state (undefined in a browser). */
export const sandboxStatus = async () => (canCall("SandboxStatus") ? app().SandboxStatus() : undefined);
/** Lets bubblewrap past AppArmor's restriction, with the system's password dialog. */
export const fixSandbox = () => app().FixSandbox();
/** Where the blitz command is (undefined in a browser, which can't install it). */
export const cliStatus = async () => (canCall("CLIStatus") ? app().CLIStatus() : undefined);
/** Puts the app's blitz on the user's PATH (a no-op when one is there already). */
export const installCLI = () => app().InstallCLI();
/** Links this app's blitz again: dead links off PATH, the one found pointed at this app's. */
export const reinstallCLI = () => app().ReinstallCLI();
/** Stops the running service: its login item, else process pid (0: unknown). */
export const stopService = (pid: number) => app().StopService(pid);
/** Stops the running service and starts the blitzd that goes with this app. */
export const restartService = (pid: number) => app().RestartService(pid);

/** Whether the file a running service started from is still there (true in a browser). */
export const programExists = async (path: string) => (canCall("ProgramExists") && path ? app().ProgramExists(path) : true);

/** Whether this is the Linux app (the Electron app's window), which can draw without the graphics card. */
export const isLinuxApp = () => inElectron() && typeof navigator !== "undefined" && /Linux/.test(navigator.userAgent) && !/Android/.test(navigator.userAgent);

/** The app's version ("dev" outside a release, and in a browser). */
export const appVersion = async () => (canCall("Version") ? app().Version() : "dev");

/**
 * Asks for a folder to open as a workspace (a native dialog; in a browser,
 * a prompt); "" when cancelled.
 */
export async function chooseWorkspace(): Promise<string> {
  if (!canCall("ChooseWorkspace")) return window.prompt(t("desktop.choose.prompt")) ?? "";
  return app().ChooseWorkspace(t("desktop.choose.title"));
}

/** The window's saved preferences, normalized (in a browser, from localStorage). */
export async function getPrefs(): Promise<Prefs> {
  if (!canCall("GetPrefs")) {
    try {
      return normalizePrefs(JSON.parse(localStorage.getItem(devPrefsKey) ?? "{}"));
    } catch {
      return defaultPrefs;
    }
  }
  const saved = normalizePrefs(await app().GetPrefs());
  return adoptBrowserPrefs(saved);
}

/**
 * The Windows tray's page kept its preferences in the browser's storage
 * before its host kept them (in desktop.json, as the app does): the first
 * time, when the host has no workspaces and the browser does, the
 * browser's move to the host, and the browser's copy goes.
 */
async function adoptBrowserPrefs(saved: Prefs): Promise<Prefs> {
  let old: Prefs | undefined;
  try {
    const raw = localStorage.getItem(devPrefsKey);
    if (raw !== null) old = normalizePrefs(JSON.parse(raw));
  } catch {
    return saved; // no storage, or nothing readable in it
  }
  if (!old) return saved;
  if (saved.workspaces.length > 0 || old.workspaces.length === 0) {
    try {
      localStorage.removeItem(devPrefsKey);
    } catch {
      // nothing to forget
    }
    return saved;
  }
  const moved = normalizePrefs(await app().SavePrefs(old));
  try {
    localStorage.removeItem(devPrefsKey);
  } catch {
    // kept: the host has them now, and is asked first
  }
  return moved;
}

/** Saves the preferences and returns them as saved (normalized). */
export async function savePrefs(p: Prefs): Promise<Prefs> {
  if (!canCall("SavePrefs")) {
    try {
      localStorage.setItem(devPrefsKey, JSON.stringify(p));
    } catch {
      // private browsing: settings last for the page
    }
    return p;
  }
  return normalizePrefs(await app().SavePrefs(p));
}

/** Opens a link from model output in the system browser (never the window). */
export async function openURL(url: string): Promise<void> {
  if (!canCall("OpenURL")) {
    window.open(url, "_blank", "noopener,noreferrer");
    return;
  }
  return app().OpenURL(url);
}

/** Shows a folder in the file manager (in the app, or from the tray). */
export const openFolder = (dir: string) => app().OpenFolder(dir);

/** Opens an image or PDF in the system's viewer (in the app, or from the tray). */
export const openDocument = (path: string) => app().OpenDocument(path);

/**
 * Prints a page (whole HTML, pictures inline) to PDF with the window's web
 * engine, on A4 or Letter paper (in the Electron app: canCall("PrintPDF")).
 */
export async function printPDF(html: string, pageSize: "A4" | "Letter"): Promise<Uint8Array> {
  const b64 = await app().PrintPDF(html, pageSize); // Go's []byte comes as base64
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
}

/** Shows a file or folder selected in the system's file manager (in the app, or from the tray). */
export const revealPath = (path: string) => app().RevealPath(path);

let fileManagerName: Promise<string> | undefined;
/** The system file manager's name (Finder, Dolphin, …; "" when unknown), or null where nothing can show files. */
export function fileManager(): Promise<string | null> {
  if (!canCall("FileManager")) return Promise.resolve(null);
  fileManagerName ??= app().FileManager().catch(() => "");
  return fileManagerName;
}

/** Shows a system notification; a click brings the window to dir. */
export async function notifyNative(title: string, body: string, dir: string): Promise<void> {
  if (canCall("Notify")) return app().Notify(title, body, dir);
  // Inside an editor: the editor's own.
  if (editorConfig()) return toEditor({ type: "notify", title, body });
  // A browser (development): the web's own notifications.
  if (typeof Notification === "undefined") return;
  if (Notification.permission === "default") await Notification.requestPermission();
  if (Notification.permission === "granted") new Notification(title, { body }).onclick = () => window.focus();
}

/**
 * Tells the app whether the editor has unsaved changes, so closing the
 * window asks first (message "" when there are none).
 */
export function setUnsaved(message: string, quit: string, cancel: string) {
  if (canCall("SetUnsaved")) app().SetUnsaved({ message, quit, cancel }).catch(() => {});
}

/**
 * Gives the window the theme the user chose, so macOS's translucent window
 * blurs in it rather than in the system's; "system" follows the system
 * again (nothing in a browser).
 */
export function setAppearance(theme: "system" | "light" | "dark") {
  if (canCall("SetAppearance")) app().SetAppearance(theme).catch(() => {});
}

/** The macOS app menu's words, in the window's language. */
export type AppMenuLabels = { about: string; settings: string; hide: string; hide_others: string; show_all: string; quit: string };

/** Relabels the macOS app menu (DSK-101) in the window's language; nothing elsewhere. */
export function setAppMenu(l: AppMenuLabels) {
  if (canCall("SetMenu")) app().SetMenu(l).catch(() => {});
}

/** Whether the app updates itself (the Electron app's host answers). */
export const canUpdate = () => canCall("CheckUpdate");
/**
 * Where the update is: the host asks for the latest release only five days
 * after it last did, unless force (Check now).
 */
export const checkUpdate = (force = false) => app().CheckUpdate(force);
/** Where the update is, without asking for the latest release again (a download, followed). */
export const updateStatus = () => app().UpdateStatus();
/** Downloads, checks and unpacks the latest release; resolves when it's done. */
export const downloadUpdate = () => app().DownloadUpdate();
/**
 * Restarts into the downloaded update: the host starts the swap, which
 * waits for the app to quit (asking first about unsaved changes, as
 * quitting does), then quits it.
 */
export async function restartToUpdate(): Promise<void> {
  await app().ApplyUpdate();
  await app().Quit();
}

/** Calls f when the app menu's About Blitz or Settings… is chosen (macOS). */
export function onAppMenu(f: (item: "about" | "settings") => void): () => void {
  const on = windowEvents();
  if (!on) return () => {};
  const offs = [on("menu:about", () => f("about")), on("menu:settings", () => f("settings"))];
  return () => offs.forEach((off) => off());
}

/** Which license text: the NOTICE, the Apache License, or the third-party notices. */
export type LicenseText = "notice" | "full" | "third-party";

/**
 * One of Blitz's license texts, embedded in the app (in a browser, a note
 * that only the app has them).
 */
export async function licenseText(which: LicenseText): Promise<string> {
  if (!canCall("License")) return t("desktop.license.dev");
  return app().License(which);
}

/** Makes the window full screen, or puts it back (in a browser: the page). */
export async function toggleFullscreen(): Promise<void> {
  const w = electronWindow();
  if (w && windowHas("ToggleFullscreen")) {
    await w.call("ToggleFullscreen");
    return;
  }
  if (document.fullscreenElement) await document.exitFullscreen();
  else await document.documentElement.requestFullscreen();
}

/**
 * Calls f with each blitz://open link: those the app was opened with, then
 * each as it comes.
 */
export function onDeepLink(f: (link: DeepLink) => void): () => void {
  const on = windowEvents();
  if (!on || !canCall("PendingLinks")) return () => {};
  const off = on("deeplink:open", (link) => {
    if (link && typeof link === "object" && typeof (link as DeepLink).dir === "string") f(link as DeepLink);
  });
  app()
    .PendingLinks()
    .then((links) => (links ?? []).forEach(f))
    .catch(() => {});
  return off;
}

/** Calls f with the workspace a clicked notification is about. */
export function onNotificationOpen(f: (dir: string) => void): () => void {
  const on = windowEvents();
  if (!on) return () => {};
  return on("notification:open", (dir) => {
    if (typeof dir === "string") f(dir);
  });
}

/**
 * Whether web pages can open in the editor (spec_electron_038 EL-100):
 * only the Electron app's window can show them.
 */
export const canPreview = () => windowHas("PreviewOpen");
/**
 * Opens web preview id at an address (a bare host gets http:// or
 * https://), or points it there when it's open; resolves with the address.
 */
export const previewOpen = (id: string, url: string) => app().PreviewOpen(id, url);
/** Places web preview id over the page's placeholder. */
export const previewBounds = (id: string, b: PreviewBounds) => (canPreview() ? app().PreviewBounds(id, b).catch(() => {}) : Promise.resolve());
/** Shows or hides web preview id (hidden under a menu, a dialog, a drawer, or another tab). */
export const previewVisible = (id: string, visible: boolean) => (canPreview() ? app().PreviewVisible(id, visible).catch(() => {}) : Promise.resolve());
/** Moves web preview id: back, forward, reload or stop (an address opens with previewOpen). */
export const previewNavigate = (id: string, step: "back" | "forward" | "reload" | "stop") => app().PreviewNavigate(id, step);
/** Closes web preview id. */
export const previewClose = (id: string) => (canPreview() ? app().PreviewClose(id).catch(() => {}) : Promise.resolve());
/** Web preview id's page as a PNG. */
export async function previewCapture(id: string): Promise<Uint8Array> {
  const b64 = await app().PreviewCapture(id);
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
}
/** Web preview id's address, title, HTML and selection. */
export const previewContent = (id: string) => app().PreviewContent(id);
/** The errors web preview id's console has shown since it opened, the latest last. */
export const previewConsole = (id: string) => app().PreviewConsole(id);
/** Web preview id's page printed to PDF, on A4 or Letter paper. */
export async function previewPrintPDF(id: string, pageSize: "A4" | "Letter"): Promise<Uint8Array> {
  const b64 = await app().PreviewPrintPDF(id, pageSize);
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
}

/** Calls f when a web preview's page changes (its address, title, loading). */
export function onPreviewState(f: (s: PreviewState) => void): () => void {
  const on = windowEvents();
  if (!on || !inElectron()) return () => {};
  return on("preview:state", (arg) => {
    const s = arg as Partial<PreviewState> | undefined;
    if (s && typeof s.id === "string") f({ id: s.id, url: s.url ?? "", title: s.title ?? "", loading: !!s.loading, back: !!s.back, forward: !!s.forward });
  });
}
