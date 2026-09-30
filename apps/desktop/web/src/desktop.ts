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

// The desktop app's own functions (Go methods bound by Wails), for what the
// service can't do: its own status and installation, native dialogs, the
// window's settings, and opening links outside the window. Wails injects
// window.go at run time. In a plain browser (development) they fall back
// to stand-ins: settings in localStorage, links in a new tab.
import { workspaces } from "./api";
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

type Bound = {
  Version(): Promise<string>;
  ProgramExists(path: string): Promise<boolean>;
  ServiceStatus(): Promise<ServiceStatus>;
  InstallService(): Promise<void>;
  SetTray(on: boolean): Promise<void>;
  StopService(pid: number): Promise<void>;
  RestartService(pid: number): Promise<void>;
  ChooseWorkspace(title: string): Promise<string>;
  GetPrefs(): Promise<Prefs>;
  SavePrefs(p: Prefs): Promise<Prefs>;
  OpenURL(url: string): Promise<void>;
  OpenFolder(dir: string): Promise<void>;
  OpenDocument(path: string): Promise<void>;
  Notify(title: string, body: string, dir: string): Promise<void>;
  SetUnsaved(u: { message: string; quit: string; cancel: string }): Promise<void>;
  License(which: string): Promise<string>;
  PendingLinks(): Promise<DeepLink[] | null>;
};

/** A blitz://open link: a folder to open, with a prompt to fill in. */
export interface DeepLink {
  dir: string;
  prompt: string;
}

function bound(): Bound | undefined {
  return (window as unknown as { go?: { main?: { App?: Bound } } }).go?.main?.App;
}

/** Whether the page runs inside the desktop app. */
export const inApp = () => !!bound();

function app(): Bound {
  const b = bound();
  if (!b) throw new Error("not running inside the Blitz desktop app");
  return b;
}

const devPrefsKey = "blitz.desktop.prefs";

/** The service's status (in a browser: whether the dev server's proxy reaches one). */
export async function serviceStatus(): Promise<ServiceStatus> {
  if (!inApp()) {
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
/** Stops the running service: its login item, else process pid (0: unknown). */
export const stopService = (pid: number) => app().StopService(pid);
/** Stops the running service and starts the blitzd that goes with this app. */
export const restartService = (pid: number) => app().RestartService(pid);

/** Whether the file a running service started from is still there (true in a browser). */
export const programExists = async (path: string) => (inApp() && path ? app().ProgramExists(path) : true);

/** The app's version ("dev" outside a release, and in a browser). */
export const appVersion = async () => (inApp() ? app().Version() : "dev");

/**
 * Asks for a folder to open as a workspace (a native dialog; in a browser,
 * a prompt); "" when cancelled.
 */
export async function chooseWorkspace(): Promise<string> {
  if (!inApp()) return window.prompt(t("desktop.choose.prompt")) ?? "";
  return app().ChooseWorkspace(t("desktop.choose.title"));
}

/** The window's saved preferences, normalized (in a browser, from localStorage). */
export async function getPrefs(): Promise<Prefs> {
  if (!inApp()) {
    try {
      return normalizePrefs(JSON.parse(localStorage.getItem(devPrefsKey) ?? "{}"));
    } catch {
      return defaultPrefs;
    }
  }
  return normalizePrefs(await app().GetPrefs());
}

/** Saves the preferences and returns them as saved (normalized). */
export async function savePrefs(p: Prefs): Promise<Prefs> {
  if (!inApp()) {
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
  if (!inApp()) {
    window.open(url, "_blank", "noopener,noreferrer");
    return;
  }
  return app().OpenURL(url);
}

/** Shows a folder in the file manager (only in the app). */
export const openFolder = (dir: string) => app().OpenFolder(dir);

/** Opens an image or PDF in the system's viewer (only in the app). */
export const openDocument = (path: string) => app().OpenDocument(path);

/** Shows a system notification; a click brings the window to dir. */
export async function notifyNative(title: string, body: string, dir: string): Promise<void> {
  if (inApp()) return app().Notify(title, body, dir);
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
  if (inApp()) app().SetUnsaved({ message, quit, cancel }).catch(() => {});
}

/** Which license text: the NOTICE, the Apache License, or the third-party notices. */
export type LicenseText = "notice" | "full" | "third-party";

/**
 * One of Blitz's license texts, embedded in the app (in a browser, a note
 * that only the app has them).
 */
export async function licenseText(which: LicenseText): Promise<string> {
  if (!inApp()) return t("desktop.license.dev");
  return app().License(which);
}

type WailsRuntime = {
  EventsOn(name: string, f: (...args: unknown[]) => void): () => void;
  WindowIsFullscreen(): Promise<boolean>;
  WindowFullscreen(): void;
  WindowUnfullscreen(): void;
};

/** Makes the window full screen, or puts it back (in a browser: the page). */
export async function toggleFullscreen(): Promise<void> {
  const rt = (window as unknown as { runtime?: WailsRuntime }).runtime;
  if (rt?.WindowIsFullscreen) {
    if (await rt.WindowIsFullscreen()) rt.WindowUnfullscreen();
    else rt.WindowFullscreen();
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
  const rt = (window as unknown as { runtime?: WailsRuntime }).runtime;
  if (!inApp() || !rt?.EventsOn) return () => {};
  const off = rt.EventsOn("deeplink:open", (link) => {
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
  const rt = (window as unknown as { runtime?: WailsRuntime }).runtime;
  if (!rt?.EventsOn) return () => {};
  return rt.EventsOn("notification:open", (dir) => {
    if (typeof dir === "string") f(dir);
  });
}
