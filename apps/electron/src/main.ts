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

// The Electron trial's main process (spec_electron_038): it starts
// blitz-tray --serve beside it (the page, the service's API and the
// host's /host/ calls, on one loopback origin), opens a window on the
// link it prints, and answers what only a window can do (the folder
// picker, printing to PDF, notifications, the menu, closing with unsaved
// changes) through the preload's window.blitzElectron.
import { spawn, type ChildProcess } from "node:child_process";
import { mkdtemp, rm, stat, writeFile } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import path from "node:path";
import { createInterface } from "node:readline";
import { pathToFileURL } from "node:url";
import { app, BrowserWindow, dialog, ipcMain, Menu, nativeTheme, Notification, session, shell, type MenuItemConstructorOptions } from "electron";
import {
  appMenu,
  englishMenu,
  isExternal,
  isWindowMethod,
  linkArgs,
  menuLabels,
  originOf,
  parseDeepLink,
  parseLink,
  printOptions,
  text,
  theme,
  unsaved,
  type DeepLink,
  type MenuLabels,
  type PageEvent,
  type Unsaved,
} from "./logic";

app.setName("Blitz");

/** The program that serves the page: bin/ in the app, beside blitzd and blitz. */
const host = path.join(app.getAppPath(), "bin", process.platform === "win32" ? "blitz-tray.exe" : "blitz-tray");
const pageDir = path.join(app.getAppPath(), "page");

let win: BrowserWindow | null = null;
let origin = "";
let sidecar: ChildProcess | null = null;
let quitting = false;
let editorUnsaved: Unsaved = unsaved(undefined);
const pending: DeepLink[] = [];

// One window: a second start (a link, on Linux and Windows) goes to the first.
if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", (_e, argv) => {
    linkArgs(argv).forEach((l) => void receiveLink(l));
    if (win) {
      if (win.isMinimized()) win.restore();
      win.show();
      win.focus();
    }
  });
  app.on("open-url", (e, url) => {
    e.preventDefault();
    void receiveLink(url);
  });
  if (app.isPackaged) app.setAsDefaultProtocolClient("blitz");
  linkArgs(process.argv).forEach((l) => void receiveLink(l));
  app.whenReady().then(start, (err) => fail(err));
  app.on("window-all-closed", () => app.quit());
  app.on("before-quit", () => {
    quitting = true;
  });
  app.on("will-quit", () => sidecar?.stdin?.end());
}

/** Starts the host and opens the window on the page. */
async function start() {
  // Only what the page needs: notifications, writing to the clipboard
  // (copy), and full screen.
  session.defaultSession.setPermissionRequestHandler((_wc, permission, done) => done(["notifications", "clipboard-sanitized-write", "fullscreen"].includes(permission)));
  setMenu(englishMenu);
  const link = await serve();
  origin = originOf(link);
  win = new BrowserWindow({
    width: 1280,
    height: 820,
    minWidth: 720,
    minHeight: 480,
    show: false,
    title: "Blitz",
    // macOS: the page draws the whole window, title bar included, over the
    // desktop showing through (as the Wails app: hidden inset, translucent).
    // Elsewhere the window has Blitz's icon (its taskbar button, its title
    // bar), as the Windows program does (winexe).
    ...(process.platform === "darwin"
      ? { titleBarStyle: "hiddenInset" as const, vibrancy: "under-window" as const, visualEffectState: "active" as const, backgroundColor: "#00000000" }
      : { backgroundColor: "#131314", icon: path.join(app.getAppPath(), "icon.png") }),
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      spellcheck: false,
    },
  });
  win.once("ready-to-show", () => win?.show());
  guard(win);
  await win.loadURL(link);
}

/**
 * Starts blitz-tray --serve and resolves with the first link it prints.
 * It stops when its standard input closes (when this app quits); if it
 * stops on its own, it's started again and the window reloaded.
 */
function serve(): Promise<string> {
  return new Promise((resolve, reject) => {
    const child = spawn(host, ["--serve", "--page", pageDir], { stdio: ["pipe", "pipe", "inherit"], windowsHide: true });
    sidecar = child;
    let first = true;
    createInterface({ input: child.stdout! }).on("line", (line) => {
      const link = parseLink(line);
      if (!link) return;
      if (first) {
        first = false;
        resolve(link);
      } else if (win && !win.isDestroyed()) {
        void win.loadURL(link);
      }
    });
    child.once("error", reject);
    child.once("exit", (code) => {
      if (first) return reject(new Error(`${host} exited (${code}) before serving the page`));
      if (quitting) return;
      // Serve again on a new port, and point the window there.
      serve().then(
        (link) => {
          origin = originOf(link);
          if (win && !win.isDestroyed()) void win.loadURL(link);
        },
        (err) => fail(err),
      );
    });
  });
}

/** The window stays on the page: other links open outside it, if anywhere. */
function guard(w: BrowserWindow) {
  w.webContents.setWindowOpenHandler(({ url }) => {
    if (isExternal(url)) void shell.openExternal(url);
    return { action: "deny" };
  });
  w.webContents.on("will-navigate", (e, url) => {
    if (originOf(url) === origin) return;
    e.preventDefault();
    if (isExternal(url)) void shell.openExternal(url);
  });
  // Closing (or quitting) with unsaved changes in the editor asks first.
  w.on("close", (e) => {
    if (!editorUnsaved.message) return;
    const answer = dialog.showMessageBoxSync(w, {
      type: "question",
      title: "Blitz",
      message: editorUnsaved.message,
      buttons: [editorUnsaved.quit, editorUnsaved.cancel],
      defaultId: 1,
      cancelId: 1,
    });
    if (answer !== 0) {
      e.preventDefault();
      quitting = false;
    }
  });
}

/** Sends the page an event (a menu item, a link, a clicked notification). */
function send(event: PageEvent, arg?: unknown) {
  if (win && !win.isDestroyed()) win.webContents.send(event, arg);
}

/** The macOS app menu in the page's language, then Edit and Window; none elsewhere. */
function setMenu(l: MenuLabels) {
  if (process.platform !== "darwin") {
    Menu.setApplicationMenu(null);
    return;
  }
  const items: MenuItemConstructorOptions[] = appMenu(l).map((it) =>
    it === "separator" ? { type: "separator" } : it.event ? { label: it.label, accelerator: it.accelerator, click: () => send(it.event!) } : { label: it.label, accelerator: it.accelerator, role: it.role },
  );
  Menu.setApplicationMenu(Menu.buildFromTemplate([{ label: "Blitz", submenu: items }, { role: "editMenu" }, { role: "windowMenu" }]));
}

/** A blitz:// link: to the page at once when it's up, else when it asks (PendingLinks). */
async function receiveLink(raw: string) {
  const link = parseDeepLink(raw, homedir());
  if (!link) return;
  const info = await stat(link.dir).catch(() => undefined);
  if (!info?.isDirectory()) return;
  if (win && !win.isDestroyed() && !win.webContents.isLoading()) send("deeplink:open", link);
  else pending.push(link);
}

/** Prints a page (whole HTML, pictures inline) to PDF, offscreen, its scripts off. */
async function printPDF(html: string, pageSize: unknown): Promise<string> {
  const dir = await mkdtemp(path.join(tmpdir(), "blitz-print-"));
  const file = path.join(dir, "page.html");
  const printer = new BrowserWindow({ show: false, webPreferences: { javascript: false, sandbox: true } });
  try {
    await writeFile(file, html);
    const loaded = printer.loadURL(pathToFileURL(file).href);
    const timeout = new Promise<never>((_, reject) => setTimeout(() => reject(new Error("printing took longer than a minute")), 60_000));
    await Promise.race([loaded, timeout]);
    // The load event waits for pictures and style sheets; fonts may still be on their way.
    await new Promise((r) => setTimeout(r, 300));
    const pdf = await Promise.race([printer.webContents.printToPDF(printOptions(pageSize)), timeout]);
    return pdf.toString("base64"); // as Go's []byte reaches the page
  } finally {
    printer.destroy();
    await rm(dir, { recursive: true, force: true });
  }
}

// The page's calls to the window, from its own origin only.
ipcMain.handle("blitz:call", async (e, method: unknown, args: unknown) => {
  if (originOf(e.senderFrame?.url ?? "") !== origin || !win) throw new Error("not the page");
  if (!isWindowMethod(method)) throw new Error(`no such call: ${String(method)}`);
  const a = Array.isArray(args) ? args : [];
  switch (method) {
    case "ChooseWorkspace": {
      const r = await dialog.showOpenDialog(win, { title: text(a, 0), properties: ["openDirectory", "createDirectory"] });
      return r.canceled ? "" : (r.filePaths[0] ?? "");
    }
    case "Notify": {
      if (!Notification.isSupported()) return;
      const n = new Notification({ title: text(a, 0), body: text(a, 1) });
      const dir = text(a, 2);
      n.on("click", () => {
        if (!win) return;
        if (win.isMinimized()) win.restore();
        win.show();
        win.focus();
        send("notification:open", dir);
      });
      n.show();
      return;
    }
    case "OpenURL": {
      const url = text(a, 0);
      if (!isExternal(url)) throw new Error(`not opening ${JSON.stringify(url)}: only web and mail links open`);
      await shell.openExternal(url);
      return;
    }
    case "PrintPDF":
      return printPDF(text(a, 0), a[1]);
    case "SetAppearance":
      nativeTheme.themeSource = theme(a[0]);
      return;
    case "SetMenu":
      setMenu(menuLabels(a[0]));
      return;
    case "SetUnsaved":
      editorUnsaved = unsaved(a[0]);
      return;
    case "PendingLinks":
      return pending.splice(0);
    case "ToggleFullscreen":
      win.setFullScreen(!win.isFullScreen());
      return;
    case "Quit":
      // As the menu's Quit: closing asks first about unsaved changes. An
      // update waiting to be put in place (the host's ApplyUpdate) goes
      // ahead once the app has quit.
      app.quit();
      return;
  }
});

/** Starting failed: say why, and quit. */
function fail(err: unknown) {
  dialog.showErrorBox("Blitz couldn't start", String(err instanceof Error ? err.message : err));
  app.quit();
}
