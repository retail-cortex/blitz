---
title: "038 · Electron trial"
weight: 38
---

*The desktop app in Electron: one engine on every platform, and a real app on Windows* (`spec_electron_038`)

| | |
|---|---|
| Status | **Trial** (approved 2026-10-16). Steps: the shared host and `blitz-tray --serve` (done, [spec_service_021](spec_service_021.md) SVC-53a); the Electron shell (EL-01–EL-31, done on macOS); the page's routing by capability (EL-40–EL-42, done); the Windows zip and CI (EL-50, to do); the findings (§6, to do). Nothing of it ships in releases. |
| Source | `apps/electron` (`src/main.ts`, `src/preload.ts`, `src/logic.ts`, `dev.sh`, `BUILD.bazel`), the Electron runtime in `MODULE.bazel`, `pkg/pagehost`, `apps/tray/internal/tray/serve.go` |
| Tests | `apps/electron/src/logic.test.ts` (vitest), `apps/desktop/web/src/desktop.test.ts` (the routing), `//apps/electron:typecheck`, `apps/tray/internal/tray/serve_test.go`, `pkg/pagehost/host_test.go` |

## 1. Purpose

The desktop app ([spec_desktop_024](spec_desktop_024.md)) is Wails: each system's own web view, WebKit on macOS and WebKitGTK on Linux. Windows has none. There the tray serves the page to Edge ([spec_windows_036](spec_windows_036.md)), without a folder picker, PDF export, the CLI's installation or menus. WebKitGTK has cost real bugs too (the NVIDIA crashes, #39 and #52).

The trial tries Electron instead: Chromium on macOS, Linux and Windows. It's a new surface beside the Wails app, not a replacement. Whether it replaces Wails is decided from measured numbers (§6).

## 2. How it fits together

- **EL-01** Electron's main process starts `blitz-tray --serve --page <app>/page` (SVC-53a) from the app's `bin/`, reads the link it prints, and opens the window on it. The page, the service's API (`/blitz.v1.*`, to the socket) and the host's calls (`/host/`, `pkg/pagehost`) share that one loopback origin, behind the one-time link and the cookie (WIN-11–13). So the page needs nothing new to reach the service or the host.
- **EL-02** The host stops when its standard input closes, which happens when Electron quits. If it stops on its own, the main process starts it again and points the window at the new link.
- **EL-03** The app (`//apps/electron:app`, `resources/app` once packaged) holds `package.json`, `dist/` (the main process and preload, TypeScript compiled by `tsc`), `page/` (the desktop page, `//apps/desktop/web:page`) and `bin/` (`blitz-tray`, with `blitzd` and `blitz` beside it as in the desktop app's packages, so the host finds them as it does there).
- **EL-04** The Electron runtime comes from Electron's release archives, one per platform (darwin-arm64, darwin-x64, linux-arm64, linux-x64, win32-x64), each pinned by its SHA-256 in `MODULE.bazel` (44.5.1). The `electron` npm package is there for its type definitions only: pnpm runs no install scripts.
- **EL-05** `bazel run //apps/electron:dev` runs it on this machine against its service. Electron runs from where its archive was unpacked, not from the runfiles' links, so macOS finds its framework. On macOS it runs a copy of Electron's bundle named Blitz (`CFBundleName`, `CFBundleDisplayName`, bundle ID `dev.blitz.electron`, re-signed ad hoc), made once per Electron in `~/Library/Caches/dev.blitz.electron`: macOS names the app in the menu bar, the Dock and the window switcher from the bundle, which says Electron. The Windows zip's program is `Blitz.exe`, though its file details still say Electron (EL-31).

## 3. The window

- **EL-10** One window: a second start (with a `blitz://` link, on Linux and Windows) goes to the first (`requestSingleInstanceLock`).
- **EL-11** Locked down: the page runs sandboxed, with context isolation and without Node. It sees only the preload's `window.blitzElectron`. The window never leaves the host's origin: other links open in the system's browser or mail program if they're web or mail links (`isExternal`, as the desktop app's `SafeURL`), and nowhere otherwise. New windows are refused. Permissions are granted for notifications, writing to the clipboard and full screen only.
- **EL-12** On macOS the page draws the whole window, title bar included (hidden inset), over the desktop showing through (`vibrancy`). Its title bar moves the window: the page's `.drag-region`s are Electron's drag regions too (`app-region: drag`, beside the Wails app's `--wails-draggable`), with what can be clicked in them excepted; and since Electron drags wherever a region is, even under what's drawn over it, menus, dialogs, the palette, snackbars and the drawer are excepted too (`app-region: no-drag`). The preload says so (`nativeGlass`), for the page's glass ([spec_desktop_024](spec_desktop_024.md) DSK-38). Elsewhere the window is solid.
- **EL-13** macOS's menu bar is the desktop app's (DSK-101): **About Blitz**, **Settings…** (⌘,), Hide, Hide Others, Show All, **Quit Blitz** (⌘Q), then Edit and Window, in the page's language (`SetMenu`). About and Settings tell the page (`menu:about`, `menu:settings`). Elsewhere there's no menu bar.
- **EL-14** Closing the window, or quitting, with unsaved changes in the editor asks first, in the page's words (`SetUnsaved`, as the desktop app's `beforeClose`).

## 4. What the page asks of the window

- **EL-20** `window.blitzElectron` has `methods` (the calls below), `nativeGlass`, `call(method, ...args)` and `on(event, f)` for `menu:about`, `menu:settings`, `deeplink:open` and `notification:open`. The main process answers only calls from the page's own origin, and only these:
  - `ChooseWorkspace(title)`: the system's folder picker (it can make a folder); `""` when cancelled.
  - `Notify(title, body, dir)`: a system notification; a click brings the window back and sends `notification:open` with `dir`.
  - `OpenURL(url)`: web and mail links only, in the system's browser; anything else is an error.
  - `PrintPDF(html, pageSize)`: the page (whole HTML, pictures inline) printed to PDF in a hidden window with its scripts off, on A4 or Letter paper with 15 mm margins, after it has loaded; the bytes as base64, as the desktop app returns them. A minute at most.
  - `SetAppearance(theme)`: the window's theme (`system`, `light` or `dark`).
  - `SetMenu(labels)`, `SetUnsaved(u)`: EL-13 and EL-14.
  - `PendingLinks()`: the `blitz://` links that came before the page was ready.
  - `ToggleFullscreen()`.
- **EL-21** Everything else the page asks of its host (the service's status and controls, preferences in `~/.blitz/desktop.json`, the CLI, the sandbox, files, licenses) goes to `/host/` (WIN-14), as on the Windows tray's page.
- **EL-22** `blitz://open?dir=<folder>&prompt=<text>` links ([spec_parity_027](spec_parity_027.md) PAR-INT-03) reach the page as `deeplink:open`, or wait for `PendingLinks`: the folder must be an absolute path that exists (`~` is the home folder), the prompt at most 20,000 characters. The packaged app registers the `blitz` scheme.

## 5. The page, and what's not done

- **EL-40** The page's `desktop.ts` routes each of the app's calls by capability (`canCall`): the Wails app, else the Electron window (`window.blitzElectron`), else the host (`/host/`), else the browser's stand-in. The window's events (`onAppMenu`, `onDeepLink`, `onNotificationOpen`) come from the Wails runtime or the Electron window alike, the window's glass (`nativeGlass`) from either, and Export as PDF is offered wherever `PrintPDF` is.
- **EL-41** Preferences go wherever `GetPrefs` is answered. Outside the Wails app, the first time the host keeps them and the browser's storage still has some, the browser's move to the host if the host has no workspaces yet, and the browser's copy is forgotten either way (`adoptBrowserPrefs`): the Windows tray's page, which kept them in the browser, doesn't lose its workspaces.
- **EL-42** *Known limit.* The host serves on a new loopback port each time it starts, so the window's origin changes with each launch, and what the page keeps in the browser's storage for itself (where the user guide was left, folded sections) starts afresh each time, as on the Windows tray's page.
- **EL-31** Not signed, no installers and no auto-update, and `Blitz.exe`'s file details (Task Manager's name) are still Electron's: the trial runs from Bazel (macOS, Linux) and from a zip (Windows, EL-50).
- **EL-50** *To do.* A `workflow_dispatch` CI job builds the Windows zip (Electron's win32 runtime, the app, and the tray, service and CLI cross-built in pure Go) and Linux and macOS archives, as artifacts. Electron's licenses go with them, and its npm dependencies into the third-party notices.

## 6. Findings

*To do (EL-60).* The Electron build beside the Wails app on macOS and Linux, and beside the Edge tray on Windows: download and installed size, memory with one workspace open after five minutes (all processes), cold start until the page is ready, macOS glass, the NVIDIA checks (#39, #52), and on Windows the folder picker, Export as PDF, notifications, the CLI and starting at login. Then a recommendation: replace Wails, keep both, or stop.

First readings (macOS, Apple silicon, 2026-10-16, development build): the page loads from the host and reaches the service; `PrintPDF` returns a PDF; `file:` links and unknown calls are refused; quitting stops the host; with the page routed (EL-40), the window has its glass, a workspace added through the host's preferences opens (saved in `desktop.json`), and Export as PDF writes the PDF beside the file; Electron and the host together hold about 530 MB resident at idle on the welcome screen (resident sizes added up, which counts shared memory more than once: a first number, not the comparison).
