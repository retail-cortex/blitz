// The desktop app's own functions (Go methods bound by Wails), for what the
// service can't do: its own status and installation, native dialogs, the
// window's settings, and opening links outside the window. Wails injects
// window.go at run time. In a plain browser (development) they fall back
// to stand-ins: settings in localStorage, links in a new tab.
import { workspaces } from "./api";
import { defaultPrefs, normalizePrefs, type Prefs } from "./prefs";

export interface ServiceStatus {
  running: boolean;
  installed: boolean;
  socket: string;
  cli: string; // the blitz binary used to install the service ("" if not found)
}

type Bound = {
  ServiceStatus(): Promise<ServiceStatus>;
  InstallService(): Promise<string>;
  ChooseWorkspace(): Promise<string>;
  GetPrefs(): Promise<Prefs>;
  SavePrefs(p: Prefs): Promise<Prefs>;
  OpenURL(url: string): Promise<void>;
  Notify(title: string, body: string, dir: string): Promise<void>;
};

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

export async function serviceStatus(): Promise<ServiceStatus> {
  if (!inApp()) {
    // In a browser the dev server proxies to the service: ask it something.
    const running = await workspaces.listWorkspaces({}).then(
      () => true,
      () => false,
    );
    return { running, installed: true, socket: "(development server)", cli: "" };
  }
  return app().ServiceStatus();
}
export const installService = () => app().InstallService();

export async function chooseWorkspace(): Promise<string> {
  if (!inApp()) return window.prompt("Workspace directory (absolute path)") ?? "";
  return app().ChooseWorkspace();
}

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

/** Shows a system notification; a click brings the window to dir. */
export async function notifyNative(title: string, body: string, dir: string): Promise<void> {
  if (inApp()) return app().Notify(title, body, dir);
  // A browser (development): the web's own notifications.
  if (typeof Notification === "undefined") return;
  if (Notification.permission === "default") await Notification.requestPermission();
  if (Notification.permission === "granted") new Notification(title, { body }).onclick = () => window.focus();
}

type WailsRuntime = { EventsOn(name: string, f: (...args: unknown[]) => void): () => void };

/** Calls f with the workspace a clicked notification is about. */
export function onNotificationOpen(f: (dir: string) => void): () => void {
  const rt = (window as unknown as { runtime?: WailsRuntime }).runtime;
  if (!rt?.EventsOn) return () => {};
  return rt.EventsOn("notification:open", (dir) => {
    if (typeof dir === "string") f(dir);
  });
}
