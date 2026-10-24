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
// changes, the web previews) through the preload's window.blitzElectron.
import { spawn, type ChildProcess } from "node:child_process";
import { X509Certificate } from "node:crypto";
import { readFileSync } from "node:fs";
import { mkdtemp, rm, stat, writeFile } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import path from "node:path";
import { createInterface } from "node:readline";
import { pathToFileURL } from "node:url";
import { app, BrowserWindow, dialog, ipcMain, Menu, nativeImage, nativeTheme, Notification, session, shell, WebContentsView, type MenuItemConstructorOptions, type NativeImage, type Session } from "electron";
import {
  absolutePaths,
  agentRequestAllowed,
  devToolsAllowed,
  devToolsCommand,
  stringList,
  type AgentReach,
  appMenu,
  bounds,
  consoleLine,
  contentScript,
  DroppedPaths,
  englishMenu,
  gpuOff,
  isExternal,
  isPageCertificate,
  isWindowMethod,
  keepLine,
  linkArgs,
  menuLabels,
  originOf,
  parseDeepLink,
  parseLink,
  shouldRenew,
  parseRunLink,
  parseTasksLink,
  taskWindowMethods,
  tasksURL,
  pictureExtension,
  maxPictureBytes,
  previewContent,
  previewId,
  profileName,
  profilePartition,
  previewStep,
  addressURL,
  previewURL,
  printCheckScript,
  printLoadScript,
  printOptions,
  text,
  theme,
  unsaved,
  waitForPrint,
  type DeepLink,
  type MenuLabels,
  type PageEvent,
  type RunLink,
  type TasksLink,
  type Unsaved,
} from "./logic";

app.setName("Blitz");

// Without the graphics card when the user said so (EL-07): before Electron starts.
const readPrefs = () => {
  try {
    return readFileSync(path.join(homedir(), ".blitz", "desktop.json"), "utf8");
  } catch {
    return undefined;
  }
};
if (gpuOff(process.env, readPrefs())) app.disableHardwareAcceleration();

/** The program that serves the page: bin/ in the app, beside blitzd and blitz. */
const host = path.join(app.getAppPath(), "bin", process.platform === "win32" ? "blitz-tray.exe" : "blitz-tray");
const pageDir = path.join(app.getAppPath(), "page");

let win: BrowserWindow | null = null;
// The tasks window (EL-110): a workflow's human tasks, without the app.
let taskWin: BrowserWindow | null = null;
// A blitz://tasks link that came before the app was ready: it starts in
// task mode, without its own window.
let taskStart: TasksLink | undefined;
// A failed run's notification asked for before the app was ready.
let runStart: RunLink | undefined;
let ready = false;
// The page's first one-time link, until a window takes it; after that the
// window's cookie lets the next one in at the origin.
let firstLink = "";
let origin = "";
// The page server's certificate (its SHA-256 fingerprint), the one the
// window trusts for it: the server makes a new one each time it starts.
let pinned = "";
let sidecar: ChildProcess | null = null;
let quitting = false;
let editorUnsaved: Unsaved = unsaved(undefined);
const pending: DeepLink[] = [];

// One window: a second start (a link, on Linux and Windows) goes to the first.
if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", (_e, argv) => {
    const links = linkArgs(argv);
    links.forEach((l) => void receiveLink(l));
    if (links.some((l) => parseTasksLink(l) || parseRunLink(l))) return;
    if (win) {
      if (win.isMinimized()) win.restore();
      win.show();
      win.focus();
    } else if (ready) {
      void openApp(); // started for a task, and now the app itself
    }
  });
  // macOS: opening the app while only the tasks window (or nothing) is open.
  app.on("activate", () => {
    if (ready && !win && !taskWin) void openApp();
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
  // The page is served over HTTP/2, so its streams share one connection
  // (HTTP/1.1 allows six per host, and the page's open streams took them
  // all). That needs TLS: the page server's own certificate is trusted, in
  // the app's windows only (the web tabs have a session of their own), and
  // every other certificate is left to Chromium (-3).
  session.defaultSession.setCertificateVerifyProc((req, done) => done(isPageCertificate(req.hostname, fingerprintOf(req.certificate.data), pinned) ? 0 : -3));
  // Should Chromium ever refuse that certificate anyway (a later Electron
  // checking its dates despite the pin, say), the host is started again:
  // it makes a new certificate, and the windows reload on its link (EL-02).
  let renewed = 0;
  session.defaultSession.webRequest.onErrorOccurred((d) => {
    if (!shouldRenew(d.error, d.url, origin, renewed, Date.now())) return;
    renewed = Date.now();
    console.warn(`blitz: the page server's certificate was refused (${d.error}); starting it again`);
    sidecar?.kill();
  });
  setMenu(englishMenu);
  firstLink = await serve();
  origin = originOf(firstLink);
  ready = true;
  if (taskStart) {
    showTasks(taskStart);
    return;
  }
  if (runStart) {
    showRun(runStart);
    return;
  }
  await openApp();
}

/** A one-time link to the page, or the page itself once a window has let the app in. */
function takeLink(): string {
  const l = firstLink;
  firstLink = "";
  return l || origin + "/";
}

/** Opens the app's window on the page, or shows it. */
async function openApp() {
  if (win && !win.isDestroyed()) {
    if (win.isMinimized()) win.restore();
    win.show();
    win.focus();
    return;
  }
  win = new BrowserWindow({
    width: 1280,
    height: 820,
    minWidth: 720,
    minHeight: 480,
    show: false,
    title: "Blitz",
    // macOS: the page draws the whole window, title bar included, over the
    // desktop showing through (hidden inset, translucent).
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
  win.on("closed", () => {
    win = null;
  });
  guard(win);
  await win.loadURL(takeLink());
}

/**
 * Shows a workflow's human tasks (EL-110): in the app's window when it's
 * open (its dialog; it notifies by itself), else a notification that opens the tasks window, or the
 * tasks window at once (asked, or no notifications here). Started only for
 * this, the app quits when the tasks window closes, or after ten minutes
 * of a notification nobody clicked.
 */
function showTasks(t: TasksLink) {
  if (!ready) {
    taskStart = t;
    return;
  }
  // The app's window announces new tasks itself; asked to show them (the
  // tray's menu, or task_alerts = open), it shows its tasks dialog.
  if (win && !win.isDestroyed()) {
    if (t.notify) return;
    void openApp();
    send("tasks:open");
    return;
  }
  if (!t.notify || !Notification.isSupported()) {
    openTaskWindow();
    return;
  }
  const n = new Notification({ title: t.title, body: t.body });
  n.on("click", () => openTaskWindow());
  n.show();
  setTimeout(() => {
    if (!win && !taskWin) app.quit();
  }, 10 * 60_000).unref();
}

/**
 * Says a workflow run failed (WF-19), from the tray: nothing when the
 * app's window is open (it announces failures itself), else a
 * notification that opens the app on the run (or the app on it at once,
 * with no notifications here). Started only for this, the app quits after
 * ten minutes of a notification nobody clicked.
 */
function showRun(r: RunLink) {
  if (!ready) {
    runStart = r;
    return;
  }
  if (win && !win.isDestroyed()) return;
  const open = () => {
    pending.push(r.open);
    void openApp();
  };
  if (!Notification.isSupported()) return open();
  const n = new Notification({ title: r.title, body: r.body });
  n.on("click", open);
  n.show();
  setTimeout(() => {
    if (!win && !taskWin) app.quit();
  }, 10 * 60_000).unref();
}

/** The small window with only the tasks: their forms, Submit and Later. */
function openTaskWindow() {
  if (taskWin && !taskWin.isDestroyed()) {
    taskWin.show();
    taskWin.focus();
    return;
  }
  taskWin = new BrowserWindow({
    width: 440,
    height: 520,
    minWidth: 360,
    minHeight: 360,
    show: false,
    title: "Blitz",
    alwaysOnTop: false,
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
  taskWin.once("ready-to-show", () => taskWin?.show());
  taskWin.on("closed", () => {
    taskWin = null;
  });
  guard(taskWin);
  void taskWin.loadURL(tasksURL(takeLink()));
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
      pinned = link.fingerprint;
      if (first) {
        first = false;
        resolve(link.url);
      } else {
        reload(link.url);
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
          reload(link);
        },
        (err) => fail(err),
      );
    });
  });
}

/** Points the open windows at the page served again (a new one-time link each). */
function reload(link: string) {
  if (win && !win.isDestroyed()) {
    void win.loadURL(link);
    if (taskWin && !taskWin.isDestroyed()) void taskWin.loadURL(tasksURL(origin + "/"));
  } else if (taskWin && !taskWin.isDestroyed()) {
    void taskWin.loadURL(tasksURL(link));
  } else {
    firstLink = link;
  }
}

/** A certificate's SHA-256 fingerprint (colon-separated hex), from its PEM; "" if it can't be read. */
function fingerprintOf(pem: string): string {
  try {
    return new X509Certificate(pem).fingerprint256;
  } catch {
    return "";
  }
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
  const tasks = parseTasksLink(raw);
  if (tasks) return showTasks(tasks);
  const failed = parseRunLink(raw);
  if (failed) return showRun(failed);
  const link = parseDeepLink(raw, homedir());
  if (!link) return;
  const info = await stat(link.dir).catch(() => undefined);
  if (!info?.isDirectory()) return;
  if (win && !win.isDestroyed() && !win.webContents.isLoading()) send("deeplink:open", link);
  else pending.push(link);
  // From the tray (Show in Blitz): the window comes forward, opened if it was closed.
  if (ready) void openApp();
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
    // Fonts and pictures may not be ready to draw yet: wait for them.
    const wc = printer.webContents;
    await wc.executeJavaScriptInIsolatedWorld(1001, [{ code: printLoadScript }]).catch(() => {});
    await waitForPrint(() => wc.executeJavaScriptInIsolatedWorld(1001, [{ code: printCheckScript }]));
    const pdf = await Promise.race([printer.webContents.printToPDF(printOptions(pageSize)), timeout]);
    return pdf.toString("base64"); // as Go's []byte reaches the page
  } finally {
    printer.destroy();
    await rm(dir, { recursive: true, force: true });
  }
}

// Files dropped on the page, as its preload read them (TrashDropped).
const dropped = new DroppedPaths();
ipcMain.handle("blitz:dropped", (e, paths: unknown) => {
  if (originOf(e.senderFrame?.url ?? "") !== origin || !win || BrowserWindow.fromWebContents(e.sender) !== win) throw new Error("not the page");
  dropped.add(absolutePaths(paths, path.isAbsolute));
});

// The picture under the pointer while files are dragged out: the app's icon.
let dragImage: NativeImage | undefined;
function dragIcon() {
  dragImage ??= nativeImage.createFromPath(path.join(app.getAppPath(), "icon.png")).resize({ width: 32, height: 32 });
  return dragImage;
}

// The page's calls to the window, from its own origin only.
ipcMain.handle("blitz:call", async (e, method: unknown, args: unknown) => {
  if (originOf(e.senderFrame?.url ?? "") !== origin) throw new Error("not the page");
  if (!isWindowMethod(method)) throw new Error(`no such call: ${String(method)}`);
  const from = BrowserWindow.fromWebContents(e.sender);
  const a0 = Array.isArray(args) ? args : [];
  if (from && from === taskWin) {
    if (!taskWindowMethods.includes(method)) throw new Error(`not from the tasks window: ${method}`);
    if (method === "OpenApp") return openApp();
    if (method === "SetAppearance") nativeTheme.themeSource = theme(a0[0]);
    if (method === "OpenURL") {
      const url = text(a0, 0);
      if (!isExternal(url)) throw new Error(`not opening ${JSON.stringify(url)}: only web and mail links open`);
      await shell.openExternal(url);
    }
    return;
  }
  if (!win || from !== win) throw new Error("not the page");
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
    case "PreviewOpen": {
      const id = previewId(a[0]);
      const url = addressURL(a[1]);
      if (!id || !url) throw new Error(`not opening ${JSON.stringify(a[1])}: only web addresses open in a preview`);
      openPreview(id, url);
      return url;
    }
    case "PreviewBounds": {
      const b = bounds(a[1]);
      if (b) preview(a).view.setBounds(b);
      return;
    }
    case "PreviewVisible":
      preview(a).view.setVisible(a[1] === true);
      return;
    case "PreviewNavigate": {
      const wc = preview(a).view.webContents;
      const step = previewStep(a[1]);
      if (step === "back") wc.navigationHistory.goBack();
      else if (step === "forward") wc.navigationHistory.goForward();
      else if (step === "reload") wc.reload();
      else if (step === "stop") wc.stop();
      else throw new Error(`no such step: ${JSON.stringify(a[1])}`);
      return;
    }
    case "PreviewClose":
      closePreview(previewId(a[0]) ?? "");
      return;
    case "PreviewCapture": {
      const image = await preview(a).view.webContents.capturePage();
      return image.toPNG().toString("base64");
    }
    case "PreviewContent":
      return previewContent(await preview(a).view.webContents.executeJavaScriptInIsolatedWorld(1000, [{ code: contentScript }]));
    case "PreviewConsole":
      return [...preview(a).errors];
    case "PreviewPicture": {
      // A picture of the page, for Save to workspace (EL-106): through the
      // preview's session (its sign-ins), web addresses and pictures only.
      preview(a);
      const url = previewURL(a[1]);
      if (!url) throw new Error(`not fetching ${JSON.stringify(a[1])}: only web addresses`);
      const res = await previewSessionOf().fetch(url, { signal: AbortSignal.timeout(30_000) });
      if (!res.ok) throw new Error(`${url}: ${res.status} ${res.statusText}`);
      const ext = pictureExtension(res.headers.get("content-type") ?? "");
      if (!ext) throw new Error(`${url} isn't a picture`);
      if (Number(res.headers.get("content-length") ?? 0) > maxPictureBytes) throw new Error(`${url} is larger than ${maxPictureBytes >> 20} MB`);
      const data = Buffer.from(await res.arrayBuffer());
      if (data.length > maxPictureBytes) throw new Error(`${url} is larger than ${maxPictureBytes >> 20} MB`);
      return { ext, data: data.toString("base64") };
    }
    case "PreviewPrintPDF": {
      // The page as it shows, for Save to workspace (EL-106).
      const pdf = await preview(a).view.webContents.printToPDF(printOptions(a[1]));
      return pdf.toString("base64");
    }
    case "DragOut": {
      // Files dragged out of the Files view: the computer gets copies
      // (EL-15). Only files there, so not a remote service's.
      const files: string[] = [];
      for (const p of absolutePaths(a[0], path.isAbsolute)) {
        if (await stat(p).then(() => true, () => false)) files.push(p);
      }
      if (files.length === 0) return false;
      e.sender.startDrag({ file: files[0], files, icon: dragIcon() });
      return true;
    }
    case "TrashDropped": {
      // A move from the computer into the workspace, once copied: only
      // files the preload read from a drop (EL-15).
      const failed: string[] = [];
      for (const p of dropped.take(absolutePaths(a[0], path.isAbsolute))) {
        await shell.trashItem(p).catch(() => failed.push(p));
      }
      return failed;
    }
    case "SignInWindow": {
      // Signing in to a site in a browser profile (EL-16), for the
      // agent's browsing in the app: a window of its own on the profile's
      // partition, popups (OAuth's) in the same session.
      const name = profileName(a[0]);
      const url = addressURL(a[1]) ?? "about:blank";
      if (!name) throw new Error(`not a browser profile: ${JSON.stringify(a[0])}`);
      openSignIn(name, url);
      return;
    }
    case "AgentBrowserOpen": {
      // The agent's browser in the app (EL-17): a view of its own,
      // watched (a preview the page shows as a tab) or off screen.
      const id = previewId(a[0]);
      if (!id || agents.has(id) || previews.has(id)) throw new Error("not a new view");
      const profile = a[2] === "" || a[2] === undefined ? undefined : profileName(a[2]);
      if (a[2] && !profile) throw new Error(`not a browser profile: ${JSON.stringify(a[2])}`);
      await openAgentView(id, a[1] === "watch", profile, { allowLocal: a[3] === true, deny: stringList(a[4]) });
      return;
    }
    case "AgentBrowserSend": {
      const v = agents.get(previewId(a[0]) ?? "");
      const cmd = devToolsCommand(a[1]);
      if (!v || !cmd) throw new Error("no such view, or not a DevTools command");
      const reply = (m: object) => send("agentbrowser:message", { id: v.id, message: JSON.stringify(m) });
      if (!devToolsAllowed(cmd.method)) {
        reply({ id: cmd.id, error: { code: -32601, message: `${cmd.method} isn't the agent's to send` } });
        return;
      }
      v.view.webContents.debugger.sendCommand(cmd.method, cmd.params ?? {}, cmd.sessionId).then(
        (result: unknown) => reply({ id: cmd.id, result: result ?? {} }),
        (err: unknown) => reply({ id: cmd.id, error: { code: -32000, message: err instanceof Error ? err.message : String(err) } }),
      );
      return;
    }
    case "AgentBrowserTakeOver": {
      // The user takes the watched view: the agent lets go, the tab stays.
      const v = agents.get(previewId(a[0]) ?? "");
      if (!v) return;
      agents.delete(v.id);
      if (v.view.webContents.debugger.isAttached()) v.view.webContents.debugger.detach();
      send("agentbrowser:closed", { id: v.id, reason: "the user took over the page" });
      return;
    }
    case "AgentBrowserClose": {
      const id = previewId(a[0]) ?? "";
      const v = agents.get(id);
      if (!v) return;
      agents.delete(id);
      agentReach.delete(v.view.webContents.id);
      if (previews.has(id)) closePreview(id);
      else v.view.webContents.close();
      return;
    }
    case "OpenApp":
      return openApp();
    case "Quit":
      // As the menu's Quit: closing asks first about unsaved changes. An
      // update waiting to be put in place (the host's ApplyUpdate) goes
      // ahead once the app has quit.
      app.quit();
      return;
  }
});

/**
 * The web previews (EL-100): one view each, over the page's placeholder,
 * in a session of their own (persist:preview, so sign-ins last) with no
 * preload and no Node, sandboxed. Only web addresses load; new windows
 * open in the same view; downloads and permissions (but writing to the
 * clipboard) are refused.
 */
const previews = new Map<string, { view: WebContentsView; errors: string[] }>();
let previewSession: Session | undefined;

function previewSessionOf(): Session {
  if (previewSession) return previewSession;
  const s = session.fromPartition("persist:preview");
  s.setPermissionRequestHandler((_wc, permission, done) => done(permission === "clipboard-sanitized-write"));
  s.setPermissionCheckHandler((_wc, permission) => permission === "clipboard-sanitized-write");
  s.on("will-download", (e) => e.preventDefault());
  previewSession = s;
  return s;
}

// The browser profiles' sessions (EL-16): each its own persistent
// partition, refusing permissions and downloads as the previews do.
const profileSessions = new Map<string, Session>();
function profileSessionOf(name: string): Session {
  const had = profileSessions.get(name);
  if (had) return had;
  const s = session.fromPartition(profilePartition(name));
  s.setPermissionRequestHandler((_wc, permission, done) => done(permission === "clipboard-sanitized-write"));
  s.setPermissionCheckHandler((_wc, permission) => permission === "clipboard-sanitized-write");
  s.on("will-download", (e) => e.preventDefault());
  profileSessions.set(name, s);
  return s;
}

/** A sign-in window on profile name's session, at url. */
function openSignIn(name: string, url: string) {
  const ses = profileSessionOf(name);
  const prefs = { session: ses, sandbox: true, contextIsolation: true, nodeIntegration: false };
  const w = new BrowserWindow({ width: 980, height: 760, title: `Sign in · ${name}`, parent: win ?? undefined, webPreferences: prefs });
  w.webContents.setWindowOpenHandler(() => ({ action: "allow", overrideBrowserWindowOptions: { webPreferences: prefs } }));
  w.webContents.on("page-title-updated", (e, title) => {
    e.preventDefault();
    w.setTitle(`${title} · ${name}`);
  });
  void w.loadURL(url);
}

// The agent's views (EL-17), by ID, and what each may reach, by its web
// contents: the request filter of the sessions they're in reads it.
const agents = new Map<string, { id: string; view: WebContentsView; watched: boolean }>();
const agentReach = new Map<number, AgentReach>();
const filtered = new WeakSet<Session>();

/** Filters the agent's requests in ses (once a session); others' go through. */
function filterAgentRequests(ses: Session) {
  if (filtered.has(ses)) return;
  filtered.add(ses);
  ses.webRequest.onBeforeRequest((details, done) => {
    const reach = details.webContentsId === undefined ? undefined : agentReach.get(details.webContentsId);
    done({ cancel: reach !== undefined && !agentRequestAllowed(details.url, reach) });
  });
}

/** A session of its own for a view without a profile: in memory, gone with it. */
function throwawaySession(id: string): Session {
  const s = session.fromPartition(`agent-${id}`);
  s.setPermissionRequestHandler((_wc, permission, done) => done(permission === "clipboard-sanitized-write"));
  s.setPermissionCheckHandler((_wc, permission) => permission === "clipboard-sanitized-write");
  s.on("will-download", (e) => e.preventDefault());
  return s;
}

/** Opens the agent's view id, its DevTools relayed to the page (and on to the service). */
async function openAgentView(id: string, watched: boolean, profile: string | undefined, reach: AgentReach) {
  if (!win) throw new Error("no window");
  const ses = profile ? profileSessionOf(profile) : throwawaySession(id);
  filterAgentRequests(ses);
  const view = new WebContentsView({ webPreferences: { session: ses, sandbox: true, contextIsolation: true, nodeIntegration: false, spellcheck: false, offscreen: !watched, backgroundThrottling: false } });
  const wc = view.webContents;
  agentReach.set(wc.id, reach);
  agents.set(id, { id, view, watched });
  if (watched) {
    view.setVisible(false);
    view.setBackgroundColor("#ffffff");
    win.contentView.addChildView(view);
    const p = { view, errors: [] };
    previews.set(id, p);
    watchPreview(id, p);
  } else {
    wc.setWindowOpenHandler(({ url }) => {
      void wc.loadURL(url).catch(() => {});
      return { action: "deny" };
    });
  }
  wc.debugger.on("message", (_e, method, params, sessionId) => send("agentbrowser:message", { id, message: JSON.stringify({ method, params, sessionId: sessionId || undefined }) }));
  wc.debugger.on("detach", (_e, reason) => {
    if (agents.delete(id)) send("agentbrowser:closed", { id, reason: `the view closed (${reason})` });
  });
  wc.on("destroyed", () => {
    agentReach.delete(wc.id);
    if (agents.delete(id)) send("agentbrowser:closed", { id, reason: "the view closed" });
  });
  await wc.loadURL("about:blank");
  wc.debugger.attach("1.3");
}

/** A preview by the page's ID; it throws for one that isn't open. */
function preview(a: unknown[]) {
  const p = previews.get(previewId(a[0]) ?? "");
  if (!p) throw new Error("no such preview");
  return p;
}

/** Opens (or points) preview id at url, hidden until the page places and shows it. */
function openPreview(id: string, url: string) {
  if (!win) return;
  let p = previews.get(id);
  if (!p) {
    const view = new WebContentsView({ webPreferences: { session: previewSessionOf(), sandbox: true, contextIsolation: true, nodeIntegration: false, spellcheck: false } });
    view.setVisible(false);
    view.setBackgroundColor("#ffffff");
    win.contentView.addChildView(view);
    p = { view, errors: [] };
    previews.set(id, p);
    watchPreview(id, p);
  }
  // A page that won't load shows Chromium's error page, and its error.
  p.view.webContents.loadURL(url).catch(() => {});
}

/** Keeps a preview to web addresses, and tells the page where it is. */
function watchPreview(id: string, p: { view: WebContentsView; errors: string[] }) {
  const wc = p.view.webContents;
  wc.setWindowOpenHandler(({ url }) => {
    const u = previewURL(url);
    if (u) void wc.loadURL(u).catch(() => {});
    return { action: "deny" };
  });
  const stay = (e: { preventDefault(): void }, url: string) => {
    if (!previewURL(url)) e.preventDefault();
  };
  wc.on("will-navigate", stay);
  wc.on("will-redirect", stay);
  const state = () => {
    if (wc.isDestroyed()) return;
    send("preview:state", { id, url: wc.getURL(), title: wc.getTitle(), loading: wc.isLoading(), back: wc.navigationHistory.canGoBack(), forward: wc.navigationHistory.canGoForward() });
  };
  wc.on("did-start-loading", state);
  wc.on("did-stop-loading", state);
  wc.on("did-navigate", state);
  wc.on("did-navigate-in-page", state);
  wc.on("page-title-updated", state);
  wc.on("console-message", (e) => {
    const line = consoleLine(e.level, e.message, e.sourceId, e.lineNumber);
    if (line) keepLine(p.errors, line);
  });
  wc.on("did-fail-load", (_e, code, description, url, mainFrame) => {
    // -3: a load stopped (by another), not failed.
    if (mainFrame && code !== -3) keepLine(p.errors, `${description} (${url})`);
  });
}

/** Closes a preview: its view goes, and its page with it. */
function closePreview(id: string) {
  const p = previews.get(id);
  if (!p) return;
  previews.delete(id);
  win?.contentView.removeChildView(p.view);
  p.view.webContents.close();
}

/** Starting failed: say why, and quit. */
function fail(err: unknown) {
  dialog.showErrorBox("Blitz couldn't start", String(err instanceof Error ? err.message : err));
  app.quit();
}
